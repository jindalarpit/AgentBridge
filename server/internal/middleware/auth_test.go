package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/user/agentbridge/server/internal/auth"
)

const testSecret = "test-jwt-secret-key"

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
