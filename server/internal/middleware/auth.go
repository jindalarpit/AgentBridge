package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/user/agentbridge/server/internal/auth"
)

// contextKey is an unexported type used for context keys to avoid collisions.
type contextKey string

// UserIDKey is the context key for the authenticated user's ID.
const UserIDKey contextKey = "user_id"

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
