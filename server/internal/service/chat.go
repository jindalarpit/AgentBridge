package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/user/agentbridge/server/pkg/protocol"
)

// Domain errors for ChatService operations.
var (
	ErrSessionNotFound = errors.New("session not found")
	ErrForbidden       = errors.New("access denied")
	ErrInvalidTitle    = errors.New("title must be between 1 and 100 characters")
	ErrInvalidMessage  = errors.New("message must be between 1 and 32000 characters")
)

// MaxHistoryMessages is the maximum number of recent messages included in a chat:task payload.
const MaxHistoryMessages = 200

// ChatSession represents a persistent conversation thread.
type ChatSession struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	RuntimeID *string   `json:"runtime_id,omitempty"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ChatMessage represents a single message within a chat session.
type ChatMessage struct {
	ID            string    `json:"id"`
	ChatSessionID string    `json:"chat_session_id"`
	Seq           int       `json:"seq"`
	Role          string    `json:"role"`
	Content       string    `json:"content"`
	Status        string    `json:"status"`
	ElapsedMs     *int      `json:"elapsed_ms,omitempty"`
	FailureReason *string   `json:"failure_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// ChatService encapsulates chat business logic.
type ChatService interface {
	CreateSession(ctx context.Context, userID string) (*ChatSession, error)
	ListSessions(ctx context.Context, userID string, page, pageSize int) ([]ChatSession, int, error)
	GetSession(ctx context.Context, userID, sessionID string) (*ChatSession, error)
	DeleteSession(ctx context.Context, userID, sessionID string) error
	RenameSession(ctx context.Context, userID, sessionID, title string) error
	SendMessage(ctx context.Context, userID, sessionID, content string) (*ChatMessage, error)
	GetMessages(ctx context.Context, sessionID string) ([]ChatMessage, error)
}

// InMemoryChatService implements ChatService with an in-memory store.
// It is safe for concurrent use.
type InMemoryChatService struct {
	mu       sync.RWMutex
	sessions map[string]*ChatSession  // keyed by session ID
	messages map[string][]*ChatMessage // keyed by session ID
}

// NewInMemoryChatService creates a new InMemoryChatService.
func NewInMemoryChatService() *InMemoryChatService {
	return &InMemoryChatService{
		sessions: make(map[string]*ChatSession),
		messages: make(map[string][]*ChatMessage),
	}
}

// generateID produces a random hex-encoded ID (16 bytes = 32 hex chars).
func generateID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// CreateSession creates a new chat session with default title and active status.
func (s *InMemoryChatService) CreateSession(_ context.Context, userID string) (*ChatSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	session := &ChatSession{
		ID:        generateID(),
		UserID:    userID,
		Title:     "New Chat",
		Status:    "active",
		CreatedAt: now,
		UpdatedAt: now,
	}

	s.sessions[session.ID] = session
	s.messages[session.ID] = []*ChatMessage{}

	return session, nil
}

// ListSessions returns paginated sessions for a user, ordered by UpdatedAt descending.
func (s *InMemoryChatService) ListSessions(_ context.Context, userID string, page, pageSize int) ([]ChatSession, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Clamp pageSize to max 50.
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 50 {
		pageSize = 50
	}
	if page < 1 {
		page = 1
	}

	// Collect sessions for this user.
	var userSessions []ChatSession
	for _, sess := range s.sessions {
		if sess.UserID == userID {
			userSessions = append(userSessions, *sess)
		}
	}

	// Sort by UpdatedAt descending (most recent first).
	sort.Slice(userSessions, func(i, j int) bool {
		return userSessions[i].UpdatedAt.After(userSessions[j].UpdatedAt)
	})

	total := len(userSessions)

	// Paginate.
	start := (page - 1) * pageSize
	if start >= total {
		return []ChatSession{}, total, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return userSessions[start:end], total, nil
}

// GetSession loads a session by ID with ownership verification.
func (s *InMemoryChatService) GetSession(_ context.Context, userID, sessionID string) (*ChatSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return nil, ErrSessionNotFound
	}

	if session.UserID != userID {
		return nil, ErrForbidden
	}

	// Return a copy.
	result := *session
	return &result, nil
}

// DeleteSession removes a session and all its messages after verifying ownership.
func (s *InMemoryChatService) DeleteSession(_ context.Context, userID, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	if session.UserID != userID {
		return ErrForbidden
	}

	// Delete session and all associated messages.
	delete(s.sessions, sessionID)
	delete(s.messages, sessionID)

	return nil
}

// RenameSession updates the session title after validating length constraints.
func (s *InMemoryChatService) RenameSession(_ context.Context, userID, sessionID, title string) error {
	// Trim whitespace and validate.
	trimmed := strings.TrimSpace(title)
	if len(trimmed) < 1 || len(trimmed) > 100 {
		return ErrInvalidTitle
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	if session.UserID != userID {
		return ErrForbidden
	}

	session.Title = trimmed
	session.UpdatedAt = time.Now()

	return nil
}

// SendMessage persists a user message with the next sequence number, then builds
// a chat:task payload with conversation history (up to 200 recent messages).
// The persist-before-relay invariant is enforced: the payload is only built after
// successful persistence. The actual relay to DaemonHub will be wired in the
// integration layer (task 24.1).
func (s *InMemoryChatService) SendMessage(_ context.Context, userID, sessionID, content string) (*ChatMessage, error) {
	// Validate content length.
	trimmed := strings.TrimSpace(content)
	if len(trimmed) < 1 || len(trimmed) > 32000 {
		return nil, ErrInvalidMessage
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return nil, ErrSessionNotFound
	}

	if session.UserID != userID {
		return nil, ErrForbidden
	}

	// Determine next sequence number.
	msgs := s.messages[sessionID]
	nextSeq := len(msgs) + 1

	now := time.Now()
	msg := &ChatMessage{
		ID:            generateID(),
		ChatSessionID: sessionID,
		Seq:           nextSeq,
		Role:          "user",
		Content:       trimmed,
		Status:        "complete",
		CreatedAt:     now,
	}

	s.messages[sessionID] = append(s.messages[sessionID], msg)

	// Update session's UpdatedAt to reflect activity.
	session.UpdatedAt = now

	// Persist-before-relay: message is now persisted. The task payload can be
	// built by the caller using BuildTaskPayload. The actual relay to the daemon
	// is handled by the integration layer.

	return msg, nil
}

// BindSessionRuntime binds a runtime to a chat session after validating ownership and runtime availability.
// It preserves all existing messages and updates the session's RuntimeID and UpdatedAt.
func (s *InMemoryChatService) BindSessionRuntime(ctx context.Context, userID, sessionID, runtimeID string, runtimeSvc RuntimeService) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Verify session exists and belongs to user.
	session, exists := s.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}
	if session.UserID != userID {
		return ErrForbidden
	}

	// Validate the runtime is available via RuntimeService.
	if err := runtimeSvc.BindRuntime(ctx, sessionID, runtimeID); err != nil {
		return err
	}

	// Update session's RuntimeID and timestamp. Messages are untouched.
	session.RuntimeID = &runtimeID
	session.UpdatedAt = time.Now()

	return nil
}

// GetMessages returns all messages for a session ordered by sequence number.
func (s *InMemoryChatService) GetMessages(_ context.Context, sessionID string) ([]ChatMessage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs, exists := s.messages[sessionID]
	if !exists {
		return []ChatMessage{}, nil
	}

	// Return copies sorted by Seq.
	result := make([]ChatMessage, len(msgs))
	for i, m := range msgs {
		result[i] = *m
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Seq < result[j].Seq
	})

	return result, nil
}

// GetRecentHistory returns the most recent messages for a session as HistoryItems,
// limited to the specified count. Messages are returned in chronological order
// (oldest first within the window).
func (s *InMemoryChatService) GetRecentHistory(_ context.Context, sessionID string, limit int) ([]protocol.HistoryItem, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msgs, exists := s.messages[sessionID]
	if !exists {
		return []protocol.HistoryItem{}, nil
	}

	// Sort by sequence number to ensure chronological order.
	sorted := make([]*ChatMessage, len(msgs))
	copy(sorted, msgs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Seq < sorted[j].Seq
	})

	// Take the last min(N, limit) messages.
	start := 0
	if len(sorted) > limit {
		start = len(sorted) - limit
	}
	window := sorted[start:]

	// Convert to HistoryItems.
	history := make([]protocol.HistoryItem, len(window))
	for i, m := range window {
		history[i] = protocol.HistoryItem{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	return history, nil
}

// BuildTaskPayload constructs a ChatTaskPayload ready for relay to the daemon.
// It includes the most recent messages (up to MaxHistoryMessages) as conversation context.
// The caller must provide the message that triggered the task and the session's bound runtime ID.
func (s *InMemoryChatService) BuildTaskPayload(_ context.Context, sessionID string, msg *ChatMessage) (*protocol.ChatTaskPayload, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return nil, ErrSessionNotFound
	}

	// Get messages sorted by sequence.
	msgs := s.messages[sessionID]
	sorted := make([]*ChatMessage, len(msgs))
	copy(sorted, msgs)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Seq < sorted[j].Seq
	})

	// Take the last min(N, MaxHistoryMessages) messages.
	start := 0
	if len(sorted) > MaxHistoryMessages {
		start = len(sorted) - MaxHistoryMessages
	}
	window := sorted[start:]

	// Convert to HistoryItems.
	history := make([]protocol.HistoryItem, len(window))
	for i, m := range window {
		history[i] = protocol.HistoryItem{
			Role:    m.Role,
			Content: m.Content,
		}
	}

	// Determine runtime ID (may be empty if not bound).
	runtimeID := ""
	if session.RuntimeID != nil {
		runtimeID = *session.RuntimeID
	}

	payload := &protocol.ChatTaskPayload{
		SessionID: sessionID,
		MessageID: msg.ID,
		Content:   msg.Content,
		History:   history,
		RuntimeID: runtimeID,
	}

	return payload, nil
}

// GetSessionOwner returns the user ID that owns the given session.
// This is used by the daemon relay to determine which user to forward messages to.
// Returns ErrSessionNotFound if the session does not exist.
func (s *InMemoryChatService) GetSessionOwner(_ context.Context, sessionID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return "", ErrSessionNotFound
	}

	return session.UserID, nil
}

// PersistAssistantMessage stores a completed assistant response message in the session.
// It assigns the next sequence number and marks the message as "complete".
// Returns the persisted message.
func (s *InMemoryChatService) PersistAssistantMessage(_ context.Context, sessionID, messageID, content string, elapsedMs int64) (*ChatMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return nil, ErrSessionNotFound
	}

	// Determine next sequence number.
	msgs := s.messages[sessionID]
	nextSeq := len(msgs) + 1

	now := time.Now()
	elapsed := int(elapsedMs)
	msg := &ChatMessage{
		ID:            messageID,
		ChatSessionID: sessionID,
		Seq:           nextSeq,
		Role:          "assistant",
		Content:       content,
		Status:        "complete",
		ElapsedMs:     &elapsed,
		CreatedAt:     now,
	}

	s.messages[sessionID] = append(s.messages[sessionID], msg)

	// Update session's UpdatedAt to reflect activity.
	session.UpdatedAt = now

	return msg, nil
}

// MarkMessageError records an error for a message in the session.
// If messageID is provided, it creates an error record; otherwise it creates
// a system-level error message in the session.
func (s *InMemoryChatService) MarkMessageError(_ context.Context, sessionID, messageID, errorMsg, errorCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	session, exists := s.sessions[sessionID]
	if !exists {
		return ErrSessionNotFound
	}

	// Determine next sequence number.
	msgs := s.messages[sessionID]
	nextSeq := len(msgs) + 1

	now := time.Now()
	failureReason := errorCode + ": " + errorMsg
	msg := &ChatMessage{
		ID:            messageID,
		ChatSessionID: sessionID,
		Seq:           nextSeq,
		Role:          "assistant",
		Content:       "",
		Status:        "error",
		FailureReason: &failureReason,
		CreatedAt:     now,
	}

	s.messages[sessionID] = append(s.messages[sessionID], msg)

	// Update session's UpdatedAt to reflect activity.
	session.UpdatedAt = now

	return nil
}
