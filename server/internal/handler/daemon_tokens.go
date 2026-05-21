package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/user/agentbridge/server/pkg/db"
)

// DaemonTokenHandler handles HTTP requests for daemon token management.
type DaemonTokenHandler struct {
	queries *db.Queries
}

// NewDaemonTokenHandler creates a new DaemonTokenHandler with the given database queries.
func NewDaemonTokenHandler(queries *db.Queries) *DaemonTokenHandler {
	return &DaemonTokenHandler{
		queries: queries,
	}
}

// CreateDaemonTokenRequest is the JSON body for creating a daemon token.
type CreateDaemonTokenRequest struct {
	Name          string `json:"name"`
	ExpiresInDays int    `json:"expires_in_days"`
}

// CreateDaemonTokenResponse is the JSON response for a newly created daemon token.
type CreateDaemonTokenResponse struct {
	Token string `json:"token"`
}

// CreateToken handles POST /api/daemon-tokens.
// Generates a new daemon token for the authenticated user.
func (h *DaemonTokenHandler) CreateToken(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	var req CreateDaemonTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate name: must be non-empty and max 100 characters.
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Name) > 100 {
		writeError(w, http.StatusBadRequest, "name must be at most 100 characters")
		return
	}

	// Validate expires_in_days: must be positive.
	if req.ExpiresInDays <= 0 {
		writeError(w, http.StatusBadRequest, "expires_in_days must be a positive integer")
		return
	}

	// Generate daemon token: ab_ prefix + 32 random bytes as lowercase hex (67 chars total).
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	token := "ab_" + hex.EncodeToString(tokenBytes)

	// Compute SHA-256 hash of the full token.
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Calculate expiry time.
	expiresAt := time.Now().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)

	// Insert record into daemon_tokens table.
	_, err := h.queries.CreateDaemonToken(r.Context(), db.CreateDaemonTokenParams{
		UserID:    userID,
		Name:      req.Name,
		TokenHash: tokenHash,
		ExpiresAt: pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create daemon token")
		return
	}

	// Return HTTP 201 with the plaintext token (only returned once).
	writeJSON(w, http.StatusCreated, CreateDaemonTokenResponse{
		Token: token,
	})
}

// RevokeCurrentToken handles DELETE /api/daemon-tokens/current.
// Authenticates using the daemon token itself (ab_ prefix Bearer token),
// deletes the matching token record from the database, and returns 204 No Content.
func (h *DaemonTokenHandler) RevokeCurrentToken(w http.ResponseWriter, r *http.Request) {
	// Extract Bearer token from Authorization header.
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		writeError(w, http.StatusUnauthorized, "missing authorization header")
		return
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		writeError(w, http.StatusUnauthorized, "invalid authorization header format")
		return
	}

	token := strings.TrimSpace(parts[1])
	if token == "" || !strings.HasPrefix(token, "ab_") {
		writeError(w, http.StatusUnauthorized, "invalid or missing daemon token")
		return
	}

	// Compute SHA-256 hash of the token.
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	// Delete the token record from the database by hash.
	err := h.queries.DeleteDaemonTokenByHash(r.Context(), tokenHash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}

	// Return 204 No Content on success.
	w.WriteHeader(http.StatusNoContent)
}
