package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/user/agentbridge/server/internal/service"
	"github.com/user/agentbridge/server/pkg/protocol"
)

// setupTestHandler creates a RuntimeHandler with in-memory services and test data.
func setupTestHandler(t *testing.T) (*RuntimeHandler, *service.InMemoryRuntimeService, *service.InMemoryChatService) {
	t.Helper()
	runtimeSvc := service.NewInMemoryRuntimeService()
	chatSvc := service.NewInMemoryChatService()
	handler := NewRuntimeHandler(runtimeSvc, chatSvc)
	return handler, runtimeSvc, chatSvc
}

// withUserID creates a request context with the given user ID.
func withUserID(r *http.Request, userID string) *http.Request {
	ctx := context.WithValue(r.Context(), UserIDKey, userID)
	return r.WithContext(ctx)
}

// withChiURLParam adds a chi URL parameter to the request context.
func withChiURLParam(r *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	ctx := context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
	return r.WithContext(ctx)
}

func TestListRuntimes_Unauthenticated(t *testing.T) {
	handler, _, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/runtimes", nil)
	w := httptest.NewRecorder()

	handler.ListRuntimes(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestListRuntimes_EmptyList(t *testing.T) {
	handler, _, _ := setupTestHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/runtimes", nil)
	req = withUserID(req, "user-1")
	w := httptest.NewRecorder()

	handler.ListRuntimes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string][]service.Runtime
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp["runtimes"]) != 0 {
		t.Errorf("expected empty runtimes list, got %d items", len(resp["runtimes"]))
	}
}

func TestListRuntimes_ReturnsOnlyUserRuntimes(t *testing.T) {
	handler, runtimeSvc, _ := setupTestHandler(t)

	// Register a daemon for user-1 with two runtimes.
	_ = runtimeSvc.RegisterDaemon(context.Background(), service.DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
			{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "2.0.0", Status: "available"},
		},
	})

	// Register a daemon for user-2 (should not appear in user-1's results).
	_ = runtimeSvc.RegisterDaemon(context.Background(), service.DaemonRegistration{
		DaemonID: "daemon-2",
		UserID:   "user-2",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "kiro-cli", BinaryPath: "/usr/bin/kiro", Version: "3.0.0", Status: "available"},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/runtimes", nil)
	req = withUserID(req, "user-1")
	w := httptest.NewRecorder()

	handler.ListRuntimes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string][]service.Runtime
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	runtimes := resp["runtimes"]
	if len(runtimes) != 2 {
		t.Fatalf("expected 2 runtimes for user-1, got %d", len(runtimes))
	}

	// Verify all returned runtimes belong to user-1's daemon.
	for _, rt := range runtimes {
		if rt.DaemonID != "daemon-1" {
			t.Errorf("expected daemon_id 'daemon-1', got '%s'", rt.DaemonID)
		}
	}
}

func TestBindRuntime_Unauthenticated(t *testing.T) {
	handler, _, _ := setupTestHandler(t)

	body := `{"runtime_id": "rt-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sess-1/bind", bytes.NewBufferString(body))
	req = withChiURLParam(req, "id", "sess-1")
	w := httptest.NewRecorder()

	handler.BindRuntime(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestBindRuntime_MissingSessionID(t *testing.T) {
	handler, _, _ := setupTestHandler(t)

	body := `{"runtime_id": "rt-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions//bind", bytes.NewBufferString(body))
	req = withUserID(req, "user-1")
	// Don't set chi URL param — simulates missing session ID.
	rctx := chi.NewRouteContext()
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.BindRuntime(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestBindRuntime_MissingRuntimeID(t *testing.T) {
	handler, _, _ := setupTestHandler(t)

	body := `{}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sess-1/bind", bytes.NewBufferString(body))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", "sess-1")
	w := httptest.NewRecorder()

	handler.BindRuntime(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestBindRuntime_InvalidBody(t *testing.T) {
	handler, _, _ := setupTestHandler(t)

	body := `not json`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/sess-1/bind", bytes.NewBufferString(body))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", "sess-1")
	w := httptest.NewRecorder()

	handler.BindRuntime(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestBindRuntime_SessionNotFound(t *testing.T) {
	handler, runtimeSvc, _ := setupTestHandler(t)

	// Register a runtime so it exists.
	_ = runtimeSvc.RegisterDaemon(context.Background(), service.DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
	})

	body := `{"runtime_id": "rt-daemon-1-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/nonexistent/bind", bytes.NewBufferString(body))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", "nonexistent")
	w := httptest.NewRecorder()

	handler.BindRuntime(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestBindRuntime_Success(t *testing.T) {
	handler, runtimeSvc, chatSvc := setupTestHandler(t)

	// Register a daemon with a runtime.
	_ = runtimeSvc.RegisterDaemon(context.Background(), service.DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
	})

	// Get the runtime ID (it's generated by the service).
	runtimes, _ := runtimeSvc.GetUserRuntimes(context.Background(), "user-1")
	if len(runtimes) == 0 {
		t.Fatal("expected at least one runtime")
	}
	runtimeID := runtimes[0].ID

	// Create a session for user-1.
	session, err := chatSvc.CreateSession(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	body, _ := json.Marshal(BindRuntimeRequest{RuntimeID: runtimeID})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/bind", bytes.NewBuffer(body))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.BindRuntime(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp["session_id"] != session.ID {
		t.Errorf("expected session_id '%s', got '%v'", session.ID, resp["session_id"])
	}

	if resp["runtime_id"] == nil {
		t.Error("expected runtime_id to be set")
	}
}

func TestBindRuntime_CrossUserDenied(t *testing.T) {
	handler, runtimeSvc, chatSvc := setupTestHandler(t)

	// Register a daemon for user-1.
	_ = runtimeSvc.RegisterDaemon(context.Background(), service.DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
	})

	runtimes, _ := runtimeSvc.GetUserRuntimes(context.Background(), "user-1")
	runtimeID := runtimes[0].ID

	// Create a session for user-1.
	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	// user-2 tries to bind to user-1's session.
	body, _ := json.Marshal(BindRuntimeRequest{RuntimeID: runtimeID})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/bind", bytes.NewBuffer(body))
	req = withUserID(req, "user-2")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.BindRuntime(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestBindRuntime_OfflineRuntime(t *testing.T) {
	handler, runtimeSvc, chatSvc := setupTestHandler(t)

	// Register a daemon then mark it offline.
	_ = runtimeSvc.RegisterDaemon(context.Background(), service.DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
	})

	runtimes, _ := runtimeSvc.GetUserRuntimes(context.Background(), "user-1")
	runtimeID := runtimes[0].ID

	// Mark daemon offline (this marks all runtimes as offline).
	_ = runtimeSvc.MarkOffline(context.Background(), "daemon-1")

	// Create a session for user-1.
	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	body, _ := json.Marshal(BindRuntimeRequest{RuntimeID: runtimeID})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/bind", bytes.NewBuffer(body))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.BindRuntime(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", w.Code)
	}
}
