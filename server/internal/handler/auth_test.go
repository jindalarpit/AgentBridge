package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testJWTSecret = "test-secret-key-for-handler-tests"

func newTestAuthHandler() *AuthHandler {
	return NewAuthHandler(testJWTSecret)
}

func TestRegister_Success(t *testing.T) {
	h := newTestAuthHandler()

	body := `{"email":"test@example.com","password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Register(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp AuthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Token == "" {
		t.Error("expected non-empty token")
	}
	if resp.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.User.Email != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got '%s'", resp.User.Email)
	}
	if resp.User.ID == "" {
		t.Error("expected non-empty user ID")
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	h := newTestAuthHandler()

	body := `{"email":"dup@example.com","password":"secret123"}`

	// First registration
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Register(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("first register failed: %d", w.Code)
	}

	// Second registration with same email
	req = httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.Register(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", w.Code)
	}
}

func TestRegister_InvalidEmail(t *testing.T) {
	h := newTestAuthHandler()

	tests := []struct {
		name  string
		email string
	}{
		{"no at sign", "invalidemail"},
		{"no domain", "user@"},
		{"no dot in domain", "user@domain"},
		{"empty", ""},
		{"only at", "@"},
		{"no local part", "@example.com"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(RegisterRequest{Email: tc.email, Password: "secret123"})
			req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.Register(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("expected status 400 for email '%s', got %d", tc.email, w.Code)
			}
		})
	}
}

func TestRegister_ShortPassword(t *testing.T) {
	h := newTestAuthHandler()

	body := `{"email":"test@example.com","password":"short"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Register(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}
}

func TestLogin_Success(t *testing.T) {
	h := newTestAuthHandler()

	// Register first
	regBody := `{"email":"login@example.com","password":"mypassword"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Register(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("register failed: %d", w.Code)
	}

	// Login
	loginBody := `{"email":"login@example.com","password":"mypassword"}`
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp AuthResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Token == "" {
		t.Error("expected non-empty token")
	}
	if resp.User == nil {
		t.Fatal("expected user in response")
	}
	if resp.User.Email != "login@example.com" {
		t.Errorf("expected email 'login@example.com', got '%s'", resp.User.Email)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	h := newTestAuthHandler()

	// Register
	regBody := `{"email":"wrong@example.com","password":"correctpass"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Register(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("register failed: %d", w.Code)
	}

	// Login with wrong password
	loginBody := `{"email":"wrong@example.com","password":"wrongpass"}`
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(loginBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
}

func TestLogin_NonexistentUser(t *testing.T) {
	h := newTestAuthHandler()

	body := `{"email":"noone@example.com","password":"whatever"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Login(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
}

func TestRefresh_Success(t *testing.T) {
	h := newTestAuthHandler()

	// Register to get a token
	regBody := `{"email":"refresh@example.com","password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Register(w, req)

	var regResp AuthResponse
	json.NewDecoder(w.Body).Decode(&regResp)

	// Refresh
	req = httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.Header.Set("Authorization", "Bearer "+regResp.Token)
	w = httptest.NewRecorder()
	h.Refresh(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["token"] == "" {
		t.Error("expected non-empty refreshed token")
	}
}

func TestRefresh_MissingToken(t *testing.T) {
	h := newTestAuthHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	w := httptest.NewRecorder()
	h.Refresh(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
}

func TestRefresh_InvalidToken(t *testing.T) {
	h := newTestAuthHandler()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	req.Header.Set("Authorization", "Bearer invalid-token-string")
	w := httptest.NewRecorder()
	h.Refresh(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
}

func TestMe_Success(t *testing.T) {
	h := newTestAuthHandler()

	// Register to create a user
	regBody := `{"email":"me@example.com","password":"secret123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", bytes.NewBufferString(regBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.Register(w, req)

	var regResp AuthResponse
	json.NewDecoder(w.Body).Decode(&regResp)

	// Call Me with user ID in context
	req = httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	ctx := context.WithValue(req.Context(), UserIDKey, regResp.User.ID)
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	h.Me(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var user User
	json.NewDecoder(w.Body).Decode(&user)
	if user.Email != "me@example.com" {
		t.Errorf("expected email 'me@example.com', got '%s'", user.Email)
	}
}

func TestMe_Unauthorized(t *testing.T) {
	h := newTestAuthHandler()

	// Call Me without user ID in context
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	h.Me(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", w.Code)
	}
}

func TestMe_UserNotFound(t *testing.T) {
	h := newTestAuthHandler()

	// Call Me with a non-existent user ID
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	ctx := context.WithValue(req.Context(), UserIDKey, "nonexistent-user-id")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h.Me(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", w.Code)
	}
}

func TestIsValidEmail(t *testing.T) {
	valid := []string{
		"user@example.com",
		"a@b.co",
		"test.user@domain.org",
		"user+tag@example.com",
	}
	for _, email := range valid {
		if !isValidEmail(email) {
			t.Errorf("expected '%s' to be valid", email)
		}
	}

	invalid := []string{
		"",
		"noatsign",
		"@domain.com",
		"user@",
		"user@d",
		"user@domain",
		"user@.com",
		"@",
	}
	for _, email := range invalid {
		if isValidEmail(email) {
			t.Errorf("expected '%s' to be invalid", email)
		}
	}
}
