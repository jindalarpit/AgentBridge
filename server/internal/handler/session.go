package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/user/agentbridge/server/internal/service"
)

// SessionHandler handles HTTP requests for chat session management.
type SessionHandler struct {
	ChatService *service.InMemoryChatService
}

// NewSessionHandler creates a new SessionHandler with the given ChatService.
func NewSessionHandler(chatSvc *service.InMemoryChatService) *SessionHandler {
	return &SessionHandler{
		ChatService: chatSvc,
	}
}

// CreateSession handles POST /api/sessions.
// Creates a new chat session for the authenticated user.
func (h *SessionHandler) CreateSession(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	session, err := h.ChatService.CreateSession(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	writeJSON(w, http.StatusCreated, session)
}

// ListSessions handles GET /api/sessions.
// Returns the authenticated user's chat sessions with pagination.
func (h *SessionHandler) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	// Parse pagination parameters from query string.
	page := 1
	pageSize := 20

	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	if ps := r.URL.Query().Get("page_size"); ps != "" {
		if parsed, err := strconv.Atoi(ps); err == nil && parsed > 0 {
			pageSize = parsed
		}
	}

	sessions, total, err := h.ChatService.ListSessions(r.Context(), userID, page, pageSize)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"sessions": sessions,
		"total":    total,
		"page":     page,
		"page_size": pageSize,
	})
}

// GetSession handles GET /api/sessions/:id.
// Returns the details of a specific chat session owned by the authenticated user.
func (h *SessionHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	session, err := h.ChatService.GetSession(r.Context(), userID, sessionID)
	if err != nil {
		switch err {
		case service.ErrSessionNotFound:
			writeError(w, http.StatusNotFound, "session not found")
		case service.ErrForbidden:
			writeError(w, http.StatusForbidden, "access denied")
		default:
			writeError(w, http.StatusInternalServerError, "failed to get session")
		}
		return
	}

	writeJSON(w, http.StatusOK, session)
}

// DeleteSession handles DELETE /api/sessions/:id.
// Deletes a chat session and all its messages for the authenticated user.
func (h *SessionHandler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	err := h.ChatService.DeleteSession(r.Context(), userID, sessionID)
	if err != nil {
		switch err {
		case service.ErrSessionNotFound:
			writeError(w, http.StatusNotFound, "session not found")
		case service.ErrForbidden:
			writeError(w, http.StatusForbidden, "access denied")
		default:
			writeError(w, http.StatusInternalServerError, "failed to delete session")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// RenameSessionRequest is the expected JSON body for the rename endpoint.
type RenameSessionRequest struct {
	Title string `json:"title"`
}

// RenameSession handles PATCH /api/sessions/:id.
// Renames a chat session for the authenticated user.
func (h *SessionHandler) RenameSession(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	var req RenameSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate title using the shared validation function.
	if err := ValidateSessionTitle(req.Title); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := h.ChatService.RenameSession(r.Context(), userID, sessionID, req.Title)
	if err != nil {
		switch err {
		case service.ErrSessionNotFound:
			writeError(w, http.StatusNotFound, "session not found")
		case service.ErrForbidden:
			writeError(w, http.StatusForbidden, "access denied")
		case service.ErrInvalidTitle:
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to rename session")
		}
		return
	}

	// Return the updated session.
	session, err := h.ChatService.GetSession(r.Context(), userID, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "rename succeeded but failed to load session")
		return
	}

	writeJSON(w, http.StatusOK, session)
}

// GetMessages handles GET /api/sessions/:id/messages.
// Returns the message history for a chat session in chronological order.
func (h *SessionHandler) GetMessages(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	// Verify the user owns this session before returning messages.
	_, err := h.ChatService.GetSession(r.Context(), userID, sessionID)
	if err != nil {
		switch err {
		case service.ErrSessionNotFound:
			writeError(w, http.StatusNotFound, "session not found")
		case service.ErrForbidden:
			writeError(w, http.StatusForbidden, "access denied")
		default:
			writeError(w, http.StatusInternalServerError, "failed to verify session ownership")
		}
		return
	}

	messages, err := h.ChatService.GetMessages(r.Context(), sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get messages")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"messages": messages,
	})
}

// SendMessageRequest is the expected JSON body for sending a message.
type SendMessageRequest struct {
	Content string `json:"content"`
}

// SendMessage handles POST /api/sessions/:id/messages.
// Sends a new message in a chat session, persists it, and triggers WebSocket relay.
func (h *SessionHandler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	sessionID := chi.URLParam(r, "id")
	if sessionID == "" {
		writeError(w, http.StatusBadRequest, "session ID is required")
		return
	}

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Validate message content using the shared validation function.
	if err := ValidateMessageContent(req.Content); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	msg, err := h.ChatService.SendMessage(r.Context(), userID, sessionID, req.Content)
	if err != nil {
		switch err {
		case service.ErrSessionNotFound:
			writeError(w, http.StatusNotFound, "session not found")
		case service.ErrForbidden:
			writeError(w, http.StatusForbidden, "access denied")
		case service.ErrInvalidMessage:
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "failed to send message")
		}
		return
	}

	writeJSON(w, http.StatusCreated, msg)
}
