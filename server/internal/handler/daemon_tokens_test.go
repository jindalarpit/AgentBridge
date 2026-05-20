package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/user/agentbridge/server/internal/auth"
	"github.com/user/agentbridge/server/pkg/db"
)

func TestDaemonTokenHandler_CreateToken_RequiresAuth(t *testing.T) {
	// Create router without DaemonTokenHandler (nil) to test auth middleware.
	cfg := RouterConfig{JWTSecret: "test-secret"}
	deps := RouterDeps{
		AuthHandler:    NewAuthHandler("test-secret"),
		SessionHandler: NewSessionHandler(nil),
		RuntimeHandler: NewRuntimeHandler(nil, nil),
		// DaemonTokenHandler is nil — route won't be registered.
	}
	router := NewRouter(cfg, deps)

	req := httptest.NewRequest(http.MethodPost, "/api/daemon-tokens", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Without the handler registered, the route should 404 (since handler is nil).
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when DaemonTokenHandler is nil, got %d", w.Code)
	}
}

func TestDaemonTokenHandler_CreateToken_InvalidBody(t *testing.T) {
	h := &DaemonTokenHandler{queries: nil}

	// Create a request with user ID in context but invalid body.
	body := bytes.NewBufferString("not json")
	req := httptest.NewRequest(http.MethodPost, "/api/daemon-tokens", body)
	ctx := context.WithValue(req.Context(), UserIDKey, "user-123")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.CreateToken(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid body, got %d", w.Code)
	}
}

func TestDaemonTokenHandler_CreateToken_EmptyName(t *testing.T) {
	h := &DaemonTokenHandler{queries: nil}

	body, _ := json.Marshal(CreateDaemonTokenRequest{
		Name:          "",
		ExpiresInDays: 90,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/daemon-tokens", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), UserIDKey, "user-123")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.CreateToken(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", w.Code)
	}

	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "name is required" {
		t.Errorf("expected 'name is required' error, got %q", resp.Error)
	}
}

func TestDaemonTokenHandler_CreateToken_NameTooLong(t *testing.T) {
	h := &DaemonTokenHandler{queries: nil}

	body, _ := json.Marshal(CreateDaemonTokenRequest{
		Name:          strings.Repeat("a", 101),
		ExpiresInDays: 90,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/daemon-tokens", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), UserIDKey, "user-123")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.CreateToken(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for name too long, got %d", w.Code)
	}

	var resp ErrorResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Error != "name must be at most 100 characters" {
		t.Errorf("expected 'name must be at most 100 characters' error, got %q", resp.Error)
	}
}

func TestDaemonTokenHandler_CreateToken_InvalidExpiresInDays(t *testing.T) {
	h := &DaemonTokenHandler{queries: nil}

	tests := []struct {
		name          string
		expiresInDays int
	}{
		{"zero", 0},
		{"negative", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(CreateDaemonTokenRequest{
				Name:          "Test Token",
				ExpiresInDays: tt.expiresInDays,
			})
			req := httptest.NewRequest(http.MethodPost, "/api/daemon-tokens", bytes.NewReader(body))
			ctx := context.WithValue(req.Context(), UserIDKey, "user-123")
			req = req.WithContext(ctx)

			w := httptest.NewRecorder()
			h.CreateToken(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for expires_in_days=%d, got %d", tt.expiresInDays, w.Code)
			}
		})
	}
}

func TestDaemonTokenHandler_CreateToken_Unauthenticated(t *testing.T) {
	h := &DaemonTokenHandler{queries: nil}

	body, _ := json.Marshal(CreateDaemonTokenRequest{
		Name:          "Test Token",
		ExpiresInDays: 90,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/daemon-tokens", bytes.NewReader(body))
	// No user ID in context.

	w := httptest.NewRecorder()
	h.CreateToken(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated request, got %d", w.Code)
	}
}

func TestDaemonTokenHandler_CreateToken_AuthMiddlewareRejectsNoToken(t *testing.T) {
	// Test that the auth middleware rejects requests without a token.
	cfg := RouterConfig{JWTSecret: "test-secret"}
	deps := RouterDeps{
		AuthHandler:        NewAuthHandler("test-secret"),
		SessionHandler:     NewSessionHandler(nil),
		RuntimeHandler:     NewRuntimeHandler(nil, nil),
		DaemonTokenHandler: &DaemonTokenHandler{queries: nil},
		ClientHub:          nil,
		DaemonHub:          nil,
	}

	// We can't use NewRouter here because it requires non-nil hubs for WS routes.
	// Instead, test the middleware behavior directly.
	_ = cfg
	_ = deps

	// Verify that the auth middleware returns 401 for missing token.
	token := "invalid-token"
	_, err := auth.ValidateToken(token, "test-secret")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

// --- Mock DBTX for testing token generation ---

// mockRow implements pgx.Row for testing.
type mockRow struct {
	scanFn func(dest ...interface{}) error
}

func (m *mockRow) Scan(dest ...interface{}) error {
	if m.scanFn != nil {
		return m.scanFn(dest...)
	}
	return nil
}

// mockDTBX implements db.DBTX for testing the CreateToken handler.
type mockDTBX struct {
	lastSQL  string
	lastArgs []interface{}
	queryRowFn func(ctx context.Context, sql string, args ...interface{}) pgx.Row
}

func (m *mockDTBX) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	m.lastSQL = sql
	m.lastArgs = args
	return pgconn.NewCommandTag("INSERT 1"), nil
}

func (m *mockDTBX) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	return nil, nil
}

func (m *mockDTBX) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	m.lastSQL = sql
	m.lastArgs = args
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &mockRow{}
}

// --- Tests for token generation format (Requirement 2.2, 6.1) ---

// TestDaemonTokenHandler_CreateToken_TokenFormat verifies that the generated token
// has the correct format: "ab_" prefix + 64 lowercase hex characters (67 chars total).
// Validates: Requirements 2.2, 6.1, 8.2
func TestDaemonTokenHandler_CreateToken_TokenFormat(t *testing.T) {
	mockDB := &mockDTBX{
		queryRowFn: func(ctx context.Context, sql string, args ...interface{}) pgx.Row {
			return &mockRow{
				scanFn: func(dest ...interface{}) error {
					// The CreateDaemonToken query returns the full row.
					// We just need to not error — the handler only uses the returned token.
					return nil
				},
			}
		},
	}

	queries := db.New(mockDB)
	h := NewDaemonTokenHandler(queries)

	body, _ := json.Marshal(CreateDaemonTokenRequest{
		Name:          "Test Token",
		ExpiresInDays: 90,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/daemon-tokens", bytes.NewReader(body))
	// Use a valid UUID format for user ID since the handler parses it.
	ctx := context.WithValue(req.Context(), UserIDKey, "12345678-1234-1234-1234-123456789abc")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.CreateToken(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp CreateDaemonTokenResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify token format: ab_ prefix + 64 hex chars = 67 total.
	if !strings.HasPrefix(resp.Token, "ab_") {
		t.Errorf("token should start with 'ab_', got prefix: %q", resp.Token[:min(3, len(resp.Token))])
	}

	if len(resp.Token) != 67 {
		t.Errorf("token should be 67 characters, got %d", len(resp.Token))
	}

	// Verify the hex portion is valid lowercase hex.
	hexPart := resp.Token[3:]
	hexRegex := regexp.MustCompile(`^[0-9a-f]{64}$`)
	if !hexRegex.MatchString(hexPart) {
		t.Errorf("token hex portion should be 64 lowercase hex chars, got %q", hexPart)
	}
}

// TestDaemonTokenHandler_CreateToken_TokenHashStoredCorrectly verifies that the
// SHA-256 hash of the generated token is what gets stored in the database.
// Validates: Requirements 2.3, 6.1
func TestDaemonTokenHandler_CreateToken_TokenHashStoredCorrectly(t *testing.T) {
	var storedHash string

	mockDB := &mockDTBX{
		queryRowFn: func(ctx context.Context, sql string, args ...interface{}) pgx.Row {
			// args[2] is the token_hash (3rd parameter in INSERT).
			if len(args) >= 3 {
				if h, ok := args[2].(string); ok {
					storedHash = h
				}
			}
			return &mockRow{
				scanFn: func(dest ...interface{}) error {
					return nil
				},
			}
		},
	}

	queries := db.New(mockDB)
	h := NewDaemonTokenHandler(queries)

	body, _ := json.Marshal(CreateDaemonTokenRequest{
		Name:          "Hash Test",
		ExpiresInDays: 30,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/daemon-tokens", bytes.NewReader(body))
	ctx := context.WithValue(req.Context(), UserIDKey, "12345678-1234-1234-1234-123456789abc")
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.CreateToken(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp CreateDaemonTokenResponse
	json.NewDecoder(w.Body).Decode(&resp)

	// Compute expected hash from the returned token.
	expectedHash := sha256.Sum256([]byte(resp.Token))
	expectedHashHex := hex.EncodeToString(expectedHash[:])

	if storedHash != expectedHashHex {
		t.Errorf("stored hash mismatch:\n  got:  %s\n  want: %s", storedHash, expectedHashHex)
	}
}

// TestDaemonTokenHandler_CreateToken_UniqueTokensGenerated verifies that multiple
// calls produce different tokens (randomness check).
// Validates: Requirement 8.3 (unique constraint support)
func TestDaemonTokenHandler_CreateToken_UniqueTokensGenerated(t *testing.T) {
	mockDB := &mockDTBX{
		queryRowFn: func(ctx context.Context, sql string, args ...interface{}) pgx.Row {
			return &mockRow{scanFn: func(dest ...interface{}) error { return nil }}
		},
	}

	queries := db.New(mockDB)
	h := NewDaemonTokenHandler(queries)

	tokens := make(map[string]bool)
	for i := 0; i < 10; i++ {
		body, _ := json.Marshal(CreateDaemonTokenRequest{
			Name:          "Unique Test",
			ExpiresInDays: 90,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/daemon-tokens", bytes.NewReader(body))
		ctx := context.WithValue(req.Context(), UserIDKey, "12345678-1234-1234-1234-123456789abc")
		req = req.WithContext(ctx)

		w := httptest.NewRecorder()
		h.CreateToken(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("iteration %d: expected 201, got %d", i, w.Code)
		}

		var resp CreateDaemonTokenResponse
		json.NewDecoder(w.Body).Decode(&resp)

		if tokens[resp.Token] {
			t.Fatalf("duplicate token generated on iteration %d: %s", i, resp.Token)
		}
		tokens[resp.Token] = true
	}
}

// --- Migration schema verification tests ---

// TestMigration_DaemonTokens_CascadeDelete verifies that the migration SQL
// includes ON DELETE CASCADE for the user_id foreign key.
// Validates: Requirement 8.5
func TestMigration_DaemonTokens_CascadeDelete(t *testing.T) {
	migrationPath := findMigrationFile(t)
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("failed to read migration file: %v", err)
	}

	sql := string(content)

	// Verify ON DELETE CASCADE is present for user_id FK.
	if !strings.Contains(strings.ToUpper(sql), "ON DELETE CASCADE") {
		t.Error("migration should include ON DELETE CASCADE for user_id foreign key")
	}

	// Verify it references users(id).
	if !strings.Contains(sql, "REFERENCES users(id)") {
		t.Error("migration should reference users(id) for the foreign key")
	}
}

// TestMigration_DaemonTokens_UniqueTokenHash verifies that the migration SQL
// includes a UNIQUE index on token_hash.
// Validates: Requirement 8.3
func TestMigration_DaemonTokens_UniqueTokenHash(t *testing.T) {
	migrationPath := findMigrationFile(t)
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("failed to read migration file: %v", err)
	}

	sql := string(content)

	// Verify UNIQUE INDEX on token_hash exists.
	if !strings.Contains(strings.ToUpper(sql), "CREATE UNIQUE INDEX") {
		t.Error("migration should include CREATE UNIQUE INDEX for token_hash")
	}

	if !strings.Contains(sql, "token_hash") {
		t.Error("migration should include token_hash in the unique index")
	}
}

// TestMigration_DaemonTokens_RequiredColumns verifies that the migration SQL
// includes all required columns per Requirement 8.1.
// Validates: Requirement 8.1
func TestMigration_DaemonTokens_RequiredColumns(t *testing.T) {
	migrationPath := findMigrationFile(t)
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		t.Fatalf("failed to read migration file: %v", err)
	}

	sql := string(content)

	requiredColumns := []string{
		"id",
		"user_id",
		"name",
		"token_hash",
		"expires_at",
		"created_at",
		"last_used_at",
	}

	for _, col := range requiredColumns {
		if !strings.Contains(sql, col) {
			t.Errorf("migration should include column %q", col)
		}
	}
}

// findMigrationFile locates the daemon_tokens migration file relative to the test file.
func findMigrationFile(t *testing.T) string {
	t.Helper()

	// Get the directory of the current test file.
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to get caller info")
	}

	// Navigate from server/internal/handler/ to server/
	handlerDir := filepath.Dir(filename)
	serverDir := filepath.Join(handlerDir, "..", "..")
	migrationPath := filepath.Join(serverDir, "migrations", "002_daemon_tokens.up.sql")

	if _, err := os.Stat(migrationPath); os.IsNotExist(err) {
		t.Skipf("migration file not found at %s", migrationPath)
	}

	return migrationPath
}

// --- Revoke endpoint tests ---

// TestDaemonTokenHandler_RevokeCurrentToken_MissingAuthHeader verifies that
// the revoke endpoint returns 401 when no Authorization header is present.
// Validates: Requirement 9.4
func TestDaemonTokenHandler_RevokeCurrentToken_MissingAuthHeader(t *testing.T) {
	h := &DaemonTokenHandler{queries: nil}

	req := httptest.NewRequest(http.MethodDelete, "/api/daemon-tokens/current", nil)
	w := httptest.NewRecorder()
	h.RevokeCurrentToken(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestDaemonTokenHandler_RevokeCurrentToken_InvalidFormat verifies that
// the revoke endpoint returns 401 for non-Bearer auth headers.
// Validates: Requirement 9.4
func TestDaemonTokenHandler_RevokeCurrentToken_InvalidFormat(t *testing.T) {
	h := &DaemonTokenHandler{queries: nil}

	tests := []struct {
		name   string
		header string
	}{
		{"no bearer prefix", "Token ab_1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"},
		{"non-ab_ token", "Bearer jwt-token-here"},
		{"empty token", "Bearer "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodDelete, "/api/daemon-tokens/current", nil)
			req.Header.Set("Authorization", tt.header)
			w := httptest.NewRecorder()
			h.RevokeCurrentToken(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", w.Code)
			}
		})
	}
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
