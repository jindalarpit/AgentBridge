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
)

// setupSessionHandler creates a SessionHandler with an in-memory ChatService.
func setupSessionHandler(t *testing.T) (*SessionHandler, *service.InMemoryChatService) {
	t.Helper()
	chatSvc := service.NewInMemoryChatService()
	handler := NewSessionHandler(chatSvc)
	return handler, chatSvc
}

// --- CreateSession tests ---

func TestCreateSession_Unauthenticated(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	w := httptest.NewRecorder()

	handler.CreateSession(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestCreateSession_Success(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions", nil)
	req = withUserID(req, "user-1")
	w := httptest.NewRecorder()

	handler.CreateSession(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var session service.ChatSession
	if err := json.NewDecoder(w.Body).Decode(&session); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if session.ID == "" {
		t.Error("expected non-empty session ID")
	}
	if session.Title != "New Chat" {
		t.Errorf("expected title 'New Chat', got '%s'", session.Title)
	}
	if session.Status != "active" {
		t.Errorf("expected status 'active', got '%s'", session.Status)
	}
	if session.UserID != "user-1" {
		t.Errorf("expected user_id 'user-1', got '%s'", session.UserID)
	}
}

// --- ListSessions tests ---

func TestListSessions_Unauthenticated(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()

	handler.ListSessions(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestListSessions_EmptyList(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req = withUserID(req, "user-1")
	w := httptest.NewRecorder()

	handler.ListSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	sessions := resp["sessions"].([]interface{})
	if len(sessions) != 0 {
		t.Errorf("expected empty sessions list, got %d items", len(sessions))
	}

	total := int(resp["total"].(float64))
	if total != 0 {
		t.Errorf("expected total 0, got %d", total)
	}
}

func TestListSessions_WithPagination(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	// Create 3 sessions for user-1.
	for i := 0; i < 3; i++ {
		_, err := chatSvc.CreateSession(context.Background(), "user-1")
		if err != nil {
			t.Fatalf("failed to create session: %v", err)
		}
	}

	// Request page 1 with page_size 2.
	req := httptest.NewRequest(http.MethodGet, "/api/sessions?page=1&page_size=2", nil)
	req = withUserID(req, "user-1")
	w := httptest.NewRecorder()

	handler.ListSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	sessions := resp["sessions"].([]interface{})
	if len(sessions) != 2 {
		t.Errorf("expected 2 sessions on page 1, got %d", len(sessions))
	}

	total := int(resp["total"].(float64))
	if total != 3 {
		t.Errorf("expected total 3, got %d", total)
	}
}

func TestListSessions_UserIsolation(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	// Create sessions for two different users.
	_, _ = chatSvc.CreateSession(context.Background(), "user-1")
	_, _ = chatSvc.CreateSession(context.Background(), "user-1")
	_, _ = chatSvc.CreateSession(context.Background(), "user-2")

	// user-1 should only see their own sessions.
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req = withUserID(req, "user-1")
	w := httptest.NewRecorder()

	handler.ListSessions(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	total := int(resp["total"].(float64))
	if total != 2 {
		t.Errorf("expected total 2 for user-1, got %d", total)
	}
}

// --- GetSession tests ---

func TestGetSession_Unauthenticated(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/some-id", nil)
	req = withChiURLParam(req, "id", "some-id")
	w := httptest.NewRecorder()

	handler.GetSession(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/nonexistent", nil)
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", "nonexistent")
	w := httptest.NewRecorder()

	handler.GetSession(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestGetSession_CrossUserDenied(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+session.ID, nil)
	req = withUserID(req, "user-2")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.GetSession(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestGetSession_Success(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+session.ID, nil)
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.GetSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp service.ChatSession
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ID != session.ID {
		t.Errorf("expected session ID '%s', got '%s'", session.ID, resp.ID)
	}
}

// --- DeleteSession tests ---

func TestDeleteSession_Unauthenticated(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/some-id", nil)
	req = withChiURLParam(req, "id", "some-id")
	w := httptest.NewRecorder()

	handler.DeleteSession(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestDeleteSession_NotFound(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/nonexistent", nil)
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", "nonexistent")
	w := httptest.NewRecorder()

	handler.DeleteSession(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestDeleteSession_CrossUserDenied(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID, nil)
	req = withUserID(req, "user-2")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.DeleteSession(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestDeleteSession_Success(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID, nil)
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.DeleteSession(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", w.Code)
	}

	// Verify session is actually deleted.
	_, err := chatSvc.GetSession(context.Background(), "user-1", session.ID)
	if err != service.ErrSessionNotFound {
		t.Errorf("expected ErrSessionNotFound after deletion, got %v", err)
	}
}

// --- RenameSession tests ---

func TestRenameSession_Unauthenticated(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	body := `{"title": "New Title"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/some-id", bytes.NewBufferString(body))
	req = withChiURLParam(req, "id", "some-id")
	w := httptest.NewRecorder()

	handler.RenameSession(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestRenameSession_InvalidBody(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+session.ID, bytes.NewBufferString("not json"))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.RenameSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestRenameSession_EmptyTitle(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	body := `{"title": "   "}`
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+session.ID, bytes.NewBufferString(body))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.RenameSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestRenameSession_TitleTooLong(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	// Create a title longer than 100 characters.
	longTitle := make([]byte, 101)
	for i := range longTitle {
		longTitle[i] = 'a'
	}
	body, _ := json.Marshal(RenameSessionRequest{Title: string(longTitle)})
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+session.ID, bytes.NewBuffer(body))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.RenameSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestRenameSession_CrossUserDenied(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	body := `{"title": "Hacked"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+session.ID, bytes.NewBufferString(body))
	req = withUserID(req, "user-2")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.RenameSession(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestRenameSession_Success(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	body := `{"title": "My Project Chat"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+session.ID, bytes.NewBufferString(body))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.RenameSession(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var resp service.ChatSession
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Title != "My Project Chat" {
		t.Errorf("expected title 'My Project Chat', got '%s'", resp.Title)
	}
}

// --- GetMessages tests ---

func TestGetMessages_Unauthenticated(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/some-id/messages", nil)
	req = withChiURLParam(req, "id", "some-id")
	w := httptest.NewRecorder()

	handler.GetMessages(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestGetMessages_SessionNotFound(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/nonexistent/messages", nil)
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", "nonexistent")
	w := httptest.NewRecorder()

	handler.GetMessages(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestGetMessages_CrossUserDenied(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+session.ID+"/messages", nil)
	req = withUserID(req, "user-2")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.GetMessages(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestGetMessages_EmptyHistory(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+session.ID+"/messages", nil)
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.GetMessages(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string][]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp["messages"]) != 0 {
		t.Errorf("expected empty messages list, got %d items", len(resp["messages"]))
	}
}

func TestGetMessages_WithMessages(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	// Send a couple of messages.
	_, _ = chatSvc.SendMessage(context.Background(), "user-1", session.ID, "Hello")
	_, _ = chatSvc.SendMessage(context.Background(), "user-1", session.ID, "How are you?")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+session.ID+"/messages", nil)
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.GetMessages(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var resp map[string][]map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	messages := resp["messages"]
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(messages))
	}

	// Verify ordering by sequence number.
	seq1 := int(messages[0]["seq"].(float64))
	seq2 := int(messages[1]["seq"].(float64))
	if seq1 >= seq2 {
		t.Errorf("expected messages in ascending seq order, got seq1=%d, seq2=%d", seq1, seq2)
	}
}

// --- SendMessage tests ---

func TestSendMessage_Unauthenticated(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	body := `{"content": "Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/some-id/messages", bytes.NewBufferString(body))
	req = withChiURLParam(req, "id", "some-id")
	w := httptest.NewRecorder()

	handler.SendMessage(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestSendMessage_InvalidBody(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/messages", bytes.NewBufferString("not json"))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.SendMessage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestSendMessage_EmptyContent(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	body := `{"content": "   "}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/messages", bytes.NewBufferString(body))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.SendMessage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestSendMessage_SessionNotFound(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	body := `{"content": "Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/nonexistent/messages", bytes.NewBufferString(body))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", "nonexistent")
	w := httptest.NewRecorder()

	handler.SendMessage(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}
}

func TestSendMessage_CrossUserDenied(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	body := `{"content": "Hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/messages", bytes.NewBufferString(body))
	req = withUserID(req, "user-2")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.SendMessage(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}

func TestSendMessage_Success(t *testing.T) {
	handler, chatSvc := setupSessionHandler(t)

	session, _ := chatSvc.CreateSession(context.Background(), "user-1")

	body := `{"content": "Hello, agent!"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID+"/messages", bytes.NewBufferString(body))
	req = withUserID(req, "user-1")
	req = withChiURLParam(req, "id", session.ID)
	w := httptest.NewRecorder()

	handler.SendMessage(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201, got %d; body: %s", w.Code, w.Body.String())
	}

	var msg service.ChatMessage
	if err := json.NewDecoder(w.Body).Decode(&msg); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if msg.ID == "" {
		t.Error("expected non-empty message ID")
	}
	if msg.Role != "user" {
		t.Errorf("expected role 'user', got '%s'", msg.Role)
	}
	if msg.Content != "Hello, agent!" {
		t.Errorf("expected content 'Hello, agent!', got '%s'", msg.Content)
	}
	if msg.Seq != 1 {
		t.Errorf("expected seq 1, got %d", msg.Seq)
	}
	if msg.ChatSessionID != session.ID {
		t.Errorf("expected chat_session_id '%s', got '%s'", session.ID, msg.ChatSessionID)
	}
}

// --- Missing session ID tests ---

func TestGetSession_MissingID(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/", nil)
	req = withUserID(req, "user-1")
	rctx := chi.NewRouteContext()
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.GetSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestDeleteSession_MissingID(t *testing.T) {
	handler, _ := setupSessionHandler(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/", nil)
	req = withUserID(req, "user-1")
	rctx := chi.NewRouteContext()
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.DeleteSession(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}
