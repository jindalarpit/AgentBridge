package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/user/agentbridge/server/internal/auth"
	"github.com/user/agentbridge/server/internal/clientws"
	"github.com/user/agentbridge/server/internal/daemonws"
	"github.com/user/agentbridge/server/internal/service"
)

// testJWTSecret is declared in auth_test.go within this package.

// newTestRouter creates a fully wired router for testing.
func newTestRouter() (http.Handler, *AuthHandler, *service.InMemoryChatService) {
	authHandler := NewAuthHandler(testJWTSecret)
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	sessionHandler := NewSessionHandler(chatSvc)
	runtimeHandler := NewRuntimeHandler(runtimeSvc, chatSvc)
	clientHub := clientws.NewHub()
	daemonHub := daemonws.NewHub()

	cfg := RouterConfig{
		JWTSecret:    testJWTSecret,
		CORSOrigins:  "http://localhost:3000",
		RateLimitRPS: 100,
	}

	deps := RouterDeps{
		AuthHandler:    authHandler,
		SessionHandler: sessionHandler,
		RuntimeHandler: runtimeHandler,
		ClientHub:      clientHub,
		DaemonHub:      daemonHub,
	}

	router := NewRouter(cfg, deps)
	return router, authHandler, chatSvc
}

// registerAndLogin registers a user and returns a valid JWT token.
func registerAndLogin(t *testing.T, router http.Handler) string {
	t.Helper()

	// Register
	body := `{"email":"router-test@example.com","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("register failed: status %d, body: %s", w.Code, w.Body.String())
	}

	var resp AuthResponse
	json.NewDecoder(w.Body).Decode(&resp)
	return resp.Token
}

func TestRouter_AuthRoutes_Register(t *testing.T) {
	router, _, _ := newTestRouter()

	body := `{"email":"test@example.com","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouter_AuthRoutes_Login(t *testing.T) {
	router, _, _ := newTestRouter()

	// Register first
	body := `{"email":"login@example.com","password":"password123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Login
	req = httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouter_AuthRoutes_Me_RequiresAuth(t *testing.T) {
	router, _, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestRouter_AuthRoutes_Me_WithToken(t *testing.T) {
	router, _, _ := newTestRouter()
	token := registerAndLogin(t, router)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouter_SessionRoutes_RequireAuth(t *testing.T) {
	router, _, _ := newTestRouter()

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/sessions/"},
		{http.MethodGet, "/api/sessions/"},
		{http.MethodGet, "/api/sessions/some-id/"},
		{http.MethodDelete, "/api/sessions/some-id/"},
		{http.MethodPatch, "/api/sessions/some-id/"},
		{http.MethodGet, "/api/sessions/some-id/messages"},
		{http.MethodPost, "/api/sessions/some-id/messages"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d for %s %s", w.Code, tt.method, tt.path)
			}
		})
	}
}

func TestRouter_SessionRoutes_CreateSession(t *testing.T) {
	router, _, _ := newTestRouter()
	token := registerAndLogin(t, router)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouter_SessionRoutes_ListSessions(t *testing.T) {
	router, _, _ := newTestRouter()
	token := registerAndLogin(t, router)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouter_RuntimeRoutes_RequireAuth(t *testing.T) {
	router, _, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/runtimes", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRouter_RuntimeRoutes_ListRuntimes(t *testing.T) {
	router, _, _ := newTestRouter()
	token := registerAndLogin(t, router)

	req := httptest.NewRequest(http.MethodGet, "/api/runtimes", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouter_BindRoute_RequiresAuth(t *testing.T) {
	router, _, _ := newTestRouter()

	body := `{"runtime_id":"some-runtime"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/some-id/bind", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRouter_WebSocket_Client_MissingToken(t *testing.T) {
	router, _, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/ws/client", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouter_WebSocket_Client_InvalidToken(t *testing.T) {
	router, _, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/ws/client?token=invalid-token", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouter_WebSocket_Daemon_MissingAuth(t *testing.T) {
	router, _, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/ws/daemon", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouter_WebSocket_Daemon_InvalidToken(t *testing.T) {
	router, _, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/ws/daemon", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRouter_CORS_PreflightRequest(t *testing.T) {
	router, _, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodOptions, "/api/auth/login", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for preflight, got %d", w.Code)
	}

	if origin := w.Header().Get("Access-Control-Allow-Origin"); origin != "http://localhost:3000" {
		t.Errorf("expected CORS origin header 'http://localhost:3000', got %q", origin)
	}
}

func TestRouter_CORS_DisallowedOrigin(t *testing.T) {
	router, _, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodOptions, "/api/auth/login", nil)
	req.Header.Set("Origin", "http://evil.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should not set the CORS origin header for disallowed origins.
	if origin := w.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Errorf("expected no CORS origin header for disallowed origin, got %q", origin)
	}
}

func TestRouter_WebSocket_Client_ValidToken(t *testing.T) {
	router, _, _ := newTestRouter()

	// Generate a valid token.
	token, err := auth.GenerateToken("user-123", "test@example.com", testJWTSecret)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	// Without a proper WebSocket upgrade, the handler will attempt to upgrade
	// and fail (since httptest doesn't support WebSocket). The key test is that
	// it doesn't return 401 — it should attempt the upgrade (which fails with
	// a non-401 status in test context).
	req := httptest.NewRequest(http.MethodGet, "/ws/client?token="+token, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// The WebSocket upgrade will fail in httptest (no proper upgrade headers),
	// but it should NOT be a 401 — that means auth passed.
	if w.Code == http.StatusUnauthorized {
		t.Errorf("expected auth to pass (non-401), got 401")
	}
}

func TestRouter_WebSocket_Daemon_ValidToken(t *testing.T) {
	router, _, _ := newTestRouter()

	// Generate a valid token.
	token, err := auth.GenerateToken("user-456", "daemon@example.com", testJWTSecret)
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ws/daemon", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Same as above — auth should pass, upgrade will fail in test context.
	if w.Code == http.StatusUnauthorized {
		t.Errorf("expected auth to pass (non-401), got 401")
	}
}

func TestRouter_NotFound(t *testing.T) {
	router, _, _ := newTestRouter()

	req := httptest.NewRequest(http.MethodGet, "/api/nonexistent", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestParseOrigins(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", []string{"*"}},
		{"*", []string{"*"}},
		{"http://localhost:3000", []string{"http://localhost:3000"}},
		{"http://localhost:3000, http://example.com", []string{"http://localhost:3000", "http://example.com"}},
		{" , , ", []string{"*"}}, // all empty after trim
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseOrigins(tt.input)
			if len(result) != len(tt.expected) {
				t.Fatalf("expected %d origins, got %d: %v", len(tt.expected), len(result), result)
			}
			for i, exp := range tt.expected {
				if result[i] != exp {
					t.Errorf("origin[%d]: expected %q, got %q", i, exp, result[i])
				}
			}
		})
	}
}

func TestIsOriginAllowed(t *testing.T) {
	allowed := []string{"http://localhost:3000", "https://app.example.com"}

	tests := []struct {
		origin   string
		expected bool
	}{
		{"http://localhost:3000", true},
		{"https://app.example.com", true},
		{"HTTP://LOCALHOST:3000", true}, // case-insensitive
		{"http://evil.com", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.origin, func(t *testing.T) {
			result := isOriginAllowed(tt.origin, allowed)
			if result != tt.expected {
				t.Errorf("isOriginAllowed(%q) = %v, want %v", tt.origin, result, tt.expected)
			}
		})
	}
}
