package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/user/agentbridge/server/internal/auth"
	"github.com/user/agentbridge/server/pkg/db"
)

// contextKey is an unexported type used for context keys to avoid collisions.
type contextKey string

// UserIDKey is the context key for the authenticated user's ID.
const UserIDKey contextKey = "user_id"

// daemonTokenPrefix is the prefix for daemon tokens that use hash-based auth.
const daemonTokenPrefix = "ab_"

// DaemonTokenQuerier defines the database methods needed for daemon token authentication.
// This interface allows for easy testing without requiring a full database connection.
type DaemonTokenQuerier interface {
	GetDaemonTokenByHash(ctx context.Context, tokenHash string) (db.DaemonToken, error)
	UpdateDaemonTokenLastUsed(ctx context.Context, id pgtype.UUID) error
}

// AuthMiddleware returns a Chi-compatible middleware that validates JWT tokens
// from the Authorization header. On success it injects the user ID into the
// request context. On failure it returns a 401 JSON error response.
func AuthMiddleware(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeUnauthorized(w, "missing authorization header")
				return
			}

			// Expect "Bearer <token>" format
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeUnauthorized(w, "invalid authorization header format")
				return
			}

			tokenString := parts[1]
			if tokenString == "" {
				writeUnauthorized(w, "missing token")
				return
			}

			claims, err := auth.ValidateToken(tokenString, jwtSecret)
			if err != nil {
				writeUnauthorized(w, "invalid or expired token")
				return
			}

			// Inject user ID into request context
			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuthMiddlewareWithDaemonTokens returns a Chi-compatible middleware that supports
// both daemon tokens (ab_ prefix) and JWT tokens. For tokens starting with "ab_",
// it computes a SHA-256 hash and looks up the token in the daemon_tokens table.
// For other tokens, it falls back to standard JWT validation.
func AuthMiddlewareWithDaemonTokens(jwtSecret string, queries DaemonTokenQuerier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				writeUnauthorized(w, "missing authorization header")
				return
			}

			// Expect "Bearer <token>" format.
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				writeUnauthorized(w, "invalid authorization header format")
				return
			}

			tokenString := parts[1]
			if tokenString == "" {
				writeUnauthorized(w, "missing token")
				return
			}

			// Determine authentication method based on token prefix.
			if strings.HasPrefix(tokenString, daemonTokenPrefix) {
				// Daemon token: hash-based lookup.
				userID, tokenID, err := authenticateDaemonToken(r.Context(), tokenString, queries)
				if err != nil {
					writeUnauthorized(w, "invalid or expired token")
					return
				}

				// Update last_used_at asynchronously (fire-and-forget).
				go func() {
					if err := queries.UpdateDaemonTokenLastUsed(context.Background(), tokenID); err != nil {
						log.Printf("middleware: failed to update last_used_at for daemon token: %v", err)
					}
				}()

				// Inject user ID into request context.
				ctx := context.WithValue(r.Context(), UserIDKey, userID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Non-ab_ token: fall back to JWT validation.
			claims, err := auth.ValidateToken(tokenString, jwtSecret)
			if err != nil {
				writeUnauthorized(w, "invalid or expired token")
				return
			}

			// Inject user ID into request context.
			ctx := context.WithValue(r.Context(), UserIDKey, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// authenticateDaemonToken validates a daemon token by computing its SHA-256 hash
// and looking it up in the daemon_tokens table. Returns the user_id and token ID
// on success, or an error if the token is not found or expired.
func authenticateDaemonToken(ctx context.Context, token string, queries DaemonTokenQuerier) (string, pgtype.UUID, error) {
	// Compute SHA-256 hash of the full token.
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Look up the hash in daemon_tokens where expires_at > now().
	record, err := queries.GetDaemonTokenByHash(ctx, tokenHash)
	if err != nil {
		return "", pgtype.UUID{}, err
	}

	// Format user_id UUID as standard string (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx).
	b := record.UserID.Bytes
	userID := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])

	return userID, record.ID, nil
}

// GetUserID extracts the authenticated user ID from the request context.
// Returns an empty string if no user ID is present.
func GetUserID(ctx context.Context) string {
	userID, _ := ctx.Value(UserIDKey).(string)
	return userID
}

// writeUnauthorized writes a 401 JSON error response.
func writeUnauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{
		"error": message,
	})
}
