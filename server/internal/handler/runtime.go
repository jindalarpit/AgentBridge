package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/user/agentbridge/server/internal/middleware"
	"github.com/user/agentbridge/server/internal/service"
)

// RuntimeHandler handles HTTP requests for runtime listing and session binding.
type RuntimeHandler struct {
	RuntimeService service.RuntimeService
	ChatService    *service.InMemoryChatService
}

// NewRuntimeHandler creates a new RuntimeHandler with the given services.
func NewRuntimeHandler(runtimeSvc service.RuntimeService, chatSvc *service.InMemoryChatService) *RuntimeHandler {
	return &RuntimeHandler{
		RuntimeService: runtimeSvc,
		ChatService:    chatSvc,
	}
}

// ListRuntimes handles GET /api/runtimes.
// Returns the authenticated user's available runtimes (online daemons, available status).
func (h *RuntimeHandler) ListRuntimes(w http.ResponseWriter, r *http.Request) {
	userID := getUserID(r)
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	runtimes, err := h.RuntimeService.GetUserRuntimes(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve runtimes")
		return
	}

	// Return empty array instead of null when no runtimes found.
	if runtimes == nil {
		runtimes = []service.Runtime{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"runtimes": runtimes,
	})
}

// BindRuntimeRequest is the expected JSON body for the bind endpoint.
type BindRuntimeRequest struct {
	RuntimeID string `json:"runtime_id"`
}

// BindRuntime handles POST /api/sessions/:id/bind.
// Binds a runtime to the specified chat session for the authenticated user.
func (h *RuntimeHandler) BindRuntime(w http.ResponseWriter, r *http.Request) {
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

	var req BindRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}

	// BindSessionRuntime validates ownership, runtime availability, and performs the binding.
	err := h.ChatService.BindSessionRuntime(r.Context(), userID, sessionID, req.RuntimeID, h.RuntimeService)
	if err != nil {
		switch err {
		case service.ErrSessionNotFound:
			writeError(w, http.StatusNotFound, "session not found")
		case service.ErrForbidden:
			// Return 403 without revealing resource existence (Requirement 7.6).
			writeError(w, http.StatusForbidden, "access denied")
		case service.ErrRuntimeNotFound:
			writeError(w, http.StatusNotFound, "runtime not found")
		case service.ErrRuntimeOffline:
			writeError(w, http.StatusConflict, "selected runtime is not available")
		default:
			writeError(w, http.StatusInternalServerError, "failed to bind runtime")
		}
		return
	}

	// Return the bound session info.
	session, err := h.ChatService.GetSession(r.Context(), userID, sessionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "binding succeeded but failed to load session")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"session_id": session.ID,
		"runtime_id": session.RuntimeID,
	})
}

// getUserID extracts the user ID from the request context.
// It checks both the handler's context key (used in direct handler tests) and
// the middleware's context key (used when requests pass through AuthMiddleware).
// Returns an empty string if not set (unauthenticated request).
func getUserID(r *http.Request) string {
	// Check handler-local context key first (for backward compatibility with tests).
	if val := r.Context().Value(UserIDKey); val != nil {
		if userID, ok := val.(string); ok && userID != "" {
			return userID
		}
	}
	// Check middleware context key (set by AuthMiddleware in production).
	if userID := middleware.GetUserID(r.Context()); userID != "" {
		return userID
	}
	return ""
}
