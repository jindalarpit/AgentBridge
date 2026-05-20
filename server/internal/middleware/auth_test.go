package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/user/agentbridge/server/internal/auth"
	"github.com/user/agentbridge/server/pkg/db"
)

const testSecret = "test-jwt-secret-key"

// mockDaemonTokenQuerier is a mock implementation of DaemonTokenQuerier for testing.
type mockDaemonTokenQuerier struct {
	getByHashFn    func(ctx context.Context, tokenHash string) (db.DaemonToken, error)
	updateLastUsed func(ctx context.Context, id pgtype.UUID) error
}

func (m *mockDaemonTokenQuerier) GetDaemonTokenByHash(ctx context.Context, tokenHash string) (db.DaemonToken, error) {
	if m.getByHashFn != nil {
		return m.getByHashFn(ctx, tokenHash)
	}
	return db.DaemonToken{}, errors.New("not found")
}

func (m *mockDaemonTokenQuerier) UpdateDaemonTokenLastUsed(ctx context.Context, id pgtype.UUID) error {
	if m.updateLastUsed != nil {
		return m.updateLastUsed(ctx, id)
	}
	return nil
}

// newAuthenticatedRequest creates a request with a valid Bearer token.
func newAuthenticatedRequest(t *testing.T, userID, email string) *http.Request {
	t.Helper()
	token, err := auth.GenerateToken(userID, email, testSecret)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

// dummyHandler is a handler that records whether it was called and the user ID from context.
func dummyHandler(called *bool, gotUserID *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		*gotUserID = GetUserID(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	var called bool
	var gotUserID string

	handler := AuthMiddleware(testSecret)(dummyHandler(&called, &gotUserID))

	req := newAuthenticatedRequest(t, "user-123", "user@example.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	if !called {
		t.Error("expected next handler to be called")
	}
	if gotUserID != "user-123" {
		t.Errorf("expected user ID 'user-123', got '%s'", gotUserID)
	}
}

func TestAuthMiddleware_MissingAuthorizationHeader(t *testing.T) {
	var called bool
	var gotUserID string

	handler := AuthMiddleware(testSecret)(dummyHandler(&called, &gotUserID))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
	if called {
		t.Error("expected next handler NOT to be called")
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected error message in response body")
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	var called bool
	var gotUserID string

	handler := AuthMiddleware(testSecret)(dummyHandler(&called, &gotUserID))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-string")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
	if called {
		t.Error("expected next handler NOT to be called")
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected error message in response body")
	}
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	var called bool
	var gotUserID string

	handler := AuthMiddleware(testSecret)(dummyHandler(&called, &gotUserID))

	// Create an expired token by signing with a past expiry.
	// We use the jwt library directly to craft an expired token.
	token := craftExpiredToken(t, "user-456", "expired@example.com", testSecret)

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
	if called {
		t.Error("expected next handler NOT to be called")
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected error message in response body")
	}
}

func TestAuthMiddleware_InvalidHeaderFormat(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"no bearer prefix", "token-without-bearer-prefix"},
		{"basic auth", "Basic dXNlcjpwYXNz"},
		{"bearer with empty token", "Bearer "},
		{"bearer lowercase", "bearer some-token"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var called bool
			var gotUserID string

			handler := AuthMiddleware(testSecret)(dummyHandler(&called, &gotUserID))

			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			req.Header.Set("Authorization", tc.header)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			// "bearer lowercase" should still work since we use EqualFold,
			// but the token "some-token" is invalid so it should still 401.
			if rr.Code != http.StatusUnauthorized {
				t.Errorf("expected status 401, got %d", rr.Code)
			}
			if called {
				t.Error("expected next handler NOT to be called")
			}
		})
	}
}

func TestAuthMiddleware_WrongSecret(t *testing.T) {
	var called bool
	var gotUserID string

	handler := AuthMiddleware("different-secret")(dummyHandler(&called, &gotUserID))

	// Generate token with testSecret but validate with different-secret
	token, err := auth.GenerateToken("user-789", "user@example.com", testSecret)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
	if called {
		t.Error("expected next handler NOT to be called")
	}
}

func TestGetUserID_NoValueInContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	userID := GetUserID(req.Context())
	if userID != "" {
		t.Errorf("expected empty string, got '%s'", userID)
	}
}

// craftExpiredToken creates a JWT token that is already expired.
func craftExpiredToken(t *testing.T, userID, email, secret string) string {
	t.Helper()

	// We'll use the jwt library directly to create a token with past timestamps.
	claims := auth.Claims{
		UserID: userID,
		Email:  email,
	}
	claims.IssuedAt = jwt.NewNumericDate(time.Now().Add(-48 * time.Hour))
	claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-24 * time.Hour))

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("failed to craft expired token: %v", err)
	}
	return tokenString
}


// --- Tests for AuthMiddlewareWithDaemonTokens ---

// testUserUUID creates a pgtype.UUID from a fixed byte pattern for testing.
func testUserUUID() pgtype.UUID {
	var u pgtype.UUID
	// Use a well-known UUID: "12345678-1234-1234-1234-123456789abc"
	u.Bytes = [16]byte{0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc}
	u.Valid = true
	return u
}

func testTokenID() pgtype.UUID {
	var u pgtype.UUID
	u.Bytes = [16]byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99}
	u.Valid = true
	return u
}

func TestAuthMiddlewareWithDaemonTokens_ValidDaemonToken(t *testing.T) {
	daemonToken := "ab_4f8a2b1c9d3e7f6a0b5c8d2e1f4a7b3c9d6e0f2a5b8c1d4e7f0a3b6c9d2e1f"
	hash := sha256.Sum256([]byte(daemonToken))
	expectedHash := hex.EncodeToString(hash[:])

	userUUID := testUserUUID()
	tokenID := testTokenID()

	var lastUsedCalled bool
	querier := &mockDaemonTokenQuerier{
		getByHashFn: func(ctx context.Context, tokenHash string) (db.DaemonToken, error) {
			if tokenHash != expectedHash {
				return db.DaemonToken{}, errors.New("not found")
			}
			return db.DaemonToken{
				ID:        tokenID,
				UserID:    userUUID,
				Name:      "Test Token",
				TokenHash: tokenHash,
				ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(24 * time.Hour), Valid: true},
				CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			}, nil
		},
		updateLastUsed: func(ctx context.Context, id pgtype.UUID) error {
			lastUsedCalled = true
			if id != tokenID {
				t.Errorf("expected token ID %v, got %v", tokenID, id)
			}
			return nil
		},
	}

	var called bool
	var gotUserID string

	handler := AuthMiddlewareWithDaemonTokens(testSecret, querier)(dummyHandler(&called, &gotUserID))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+daemonToken)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	if !called {
		t.Error("expected next handler to be called")
	}
	// Expected user ID: "12345678-1234-1234-1234-123456789abc"
	expectedUserID := "12345678-1234-1234-1234-123456789abc"
	if gotUserID != expectedUserID {
		t.Errorf("expected user ID '%s', got '%s'", expectedUserID, gotUserID)
	}

	// Give the goroutine time to execute.
	time.Sleep(50 * time.Millisecond)
	if !lastUsedCalled {
		t.Error("expected UpdateDaemonTokenLastUsed to be called")
	}
}

func TestAuthMiddlewareWithDaemonTokens_InvalidDaemonToken(t *testing.T) {
	daemonToken := "ab_invalidtokenthatshouldnotexistindatabaseatall1234567890abcdef"

	querier := &mockDaemonTokenQuerier{
		getByHashFn: func(ctx context.Context, tokenHash string) (db.DaemonToken, error) {
			return db.DaemonToken{}, errors.New("no rows")
		},
	}

	var called bool
	var gotUserID string

	handler := AuthMiddlewareWithDaemonTokens(testSecret, querier)(dummyHandler(&called, &gotUserID))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+daemonToken)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
	if called {
		t.Error("expected next handler NOT to be called")
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] == "" {
		t.Error("expected error message in response body")
	}
}

func TestAuthMiddlewareWithDaemonTokens_FallbackToJWT(t *testing.T) {
	querier := &mockDaemonTokenQuerier{}

	var called bool
	var gotUserID string

	handler := AuthMiddlewareWithDaemonTokens(testSecret, querier)(dummyHandler(&called, &gotUserID))

	// Use a valid JWT (non-ab_ token).
	req := newAuthenticatedRequest(t, "user-jwt-123", "user@example.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
	if !called {
		t.Error("expected next handler to be called")
	}
	if gotUserID != "user-jwt-123" {
		t.Errorf("expected user ID 'user-jwt-123', got '%s'", gotUserID)
	}
}

func TestAuthMiddlewareWithDaemonTokens_InvalidJWTFallback(t *testing.T) {
	querier := &mockDaemonTokenQuerier{}

	var called bool
	var gotUserID string

	handler := AuthMiddlewareWithDaemonTokens(testSecret, querier)(dummyHandler(&called, &gotUserID))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer invalid-jwt-token")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
	if called {
		t.Error("expected next handler NOT to be called")
	}
}

func TestAuthMiddlewareWithDaemonTokens_MissingAuthHeader(t *testing.T) {
	querier := &mockDaemonTokenQuerier{}

	var called bool
	var gotUserID string

	handler := AuthMiddlewareWithDaemonTokens(testSecret, querier)(dummyHandler(&called, &gotUserID))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
	if called {
		t.Error("expected next handler NOT to be called")
	}
}

func TestAuthMiddlewareWithDaemonTokens_EmptyToken(t *testing.T) {
	querier := &mockDaemonTokenQuerier{}

	var called bool
	var gotUserID string

	handler := AuthMiddlewareWithDaemonTokens(testSecret, querier)(dummyHandler(&called, &gotUserID))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer ")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
	if called {
		t.Error("expected next handler NOT to be called")
	}
}

func TestAuthMiddlewareWithDaemonTokens_ExpiredDaemonToken(t *testing.T) {
	// The GetDaemonTokenByHash query already filters by expires_at > now(),
	// so an expired token will return "no rows" from the database.
	daemonToken := "ab_expiredtokenthatshouldnotexistindatabaseatall1234567890abcde"

	querier := &mockDaemonTokenQuerier{
		getByHashFn: func(ctx context.Context, tokenHash string) (db.DaemonToken, error) {
			// Simulates the DB returning no rows because expires_at > now() filter excludes it.
			return db.DaemonToken{}, errors.New("no rows in result set")
		},
	}

	var called bool
	var gotUserID string

	handler := AuthMiddlewareWithDaemonTokens(testSecret, querier)(dummyHandler(&called, &gotUserID))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+daemonToken)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
	if called {
		t.Error("expected next handler NOT to be called")
	}
}

func TestAuthMiddlewareWithDaemonTokens_LastUsedAtNotUpdatedOnFailure(t *testing.T) {
	daemonToken := "ab_failedtokenthatshouldnotexistindatabaseatall1234567890abcde"

	var lastUsedCalled bool
	querier := &mockDaemonTokenQuerier{
		getByHashFn: func(ctx context.Context, tokenHash string) (db.DaemonToken, error) {
			return db.DaemonToken{}, errors.New("no rows")
		},
		updateLastUsed: func(ctx context.Context, id pgtype.UUID) error {
			lastUsedCalled = true
			return nil
		},
	}

	var called bool
	var gotUserID string

	handler := AuthMiddlewareWithDaemonTokens(testSecret, querier)(dummyHandler(&called, &gotUserID))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+daemonToken)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}

	// Give goroutine time to run (if it were incorrectly called).
	time.Sleep(50 * time.Millisecond)
	if lastUsedCalled {
		t.Error("expected UpdateDaemonTokenLastUsed NOT to be called on auth failure")
	}
}
