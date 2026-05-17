package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/user/agentbridge/server/pkg/protocol"
)

func TestCreateSession_Defaults(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	before := time.Now()
	session, err := svc.CreateSession(ctx, "user-1")
	after := time.Now()

	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if session.ID == "" {
		t.Error("session ID is empty")
	}
	if session.UserID != "user-1" {
		t.Errorf("session.UserID = %q, want %q", session.UserID, "user-1")
	}
	if session.Title != "New Chat" {
		t.Errorf("session.Title = %q, want %q", session.Title, "New Chat")
	}
	if session.Status != "active" {
		t.Errorf("session.Status = %q, want %q", session.Status, "active")
	}
	if session.CreatedAt.Before(before) || session.CreatedAt.After(after) {
		t.Errorf("session.CreatedAt = %v, want between %v and %v", session.CreatedAt, before, after)
	}
	if session.UpdatedAt.Before(before) || session.UpdatedAt.After(after) {
		t.Errorf("session.UpdatedAt = %v, want between %v and %v", session.UpdatedAt, before, after)
	}
	if session.RuntimeID != nil {
		t.Errorf("session.RuntimeID = %v, want nil", session.RuntimeID)
	}
}

func TestCreateSession_UniqueIDs(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		session, err := svc.CreateSession(ctx, "user-1")
		if err != nil {
			t.Fatalf("CreateSession failed on iteration %d: %v", i, err)
		}
		if ids[session.ID] {
			t.Fatalf("duplicate session ID: %s", session.ID)
		}
		ids[session.ID] = true
	}
}

func TestListSessions_PaginatedAndOrdered(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	// Create 5 sessions with staggered timestamps.
	var sessionIDs []string
	for i := 0; i < 5; i++ {
		session, err := svc.CreateSession(ctx, "user-1")
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}
		sessionIDs = append(sessionIDs, session.ID)
		// Ensure distinct timestamps.
		time.Sleep(time.Millisecond)
	}

	// Update the 3rd session to make it most recent.
	err := svc.RenameSession(ctx, "user-1", sessionIDs[2], "Updated Session")
	if err != nil {
		t.Fatalf("RenameSession failed: %v", err)
	}

	// List page 1 with pageSize 3.
	sessions, total, err := svc.ListSessions(ctx, "user-1", 1, 3)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}

	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if len(sessions) != 3 {
		t.Fatalf("got %d sessions, want 3", len(sessions))
	}

	// First session should be the most recently updated one.
	if sessions[0].ID != sessionIDs[2] {
		t.Errorf("first session ID = %q, want %q (most recently updated)", sessions[0].ID, sessionIDs[2])
	}

	// Verify descending order.
	for i := 1; i < len(sessions); i++ {
		if sessions[i].UpdatedAt.After(sessions[i-1].UpdatedAt) {
			t.Errorf("sessions not in descending order at index %d", i)
		}
	}

	// List page 2.
	sessions2, total2, err := svc.ListSessions(ctx, "user-1", 2, 3)
	if err != nil {
		t.Fatalf("ListSessions page 2 failed: %v", err)
	}
	if total2 != 5 {
		t.Errorf("total on page 2 = %d, want 5", total2)
	}
	if len(sessions2) != 2 {
		t.Fatalf("got %d sessions on page 2, want 2", len(sessions2))
	}
}

func TestListSessions_MaxPageSize(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	// Request pageSize > 50 should be clamped.
	_, _, err := svc.ListSessions(ctx, "user-1", 1, 100)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	// No error means it was clamped internally.
}

func TestListSessions_EmptyForOtherUser(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	_, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	sessions, total, err := svc.ListSessions(ctx, "user-2", 1, 20)
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d, want 0", total)
	}
	if len(sessions) != 0 {
		t.Errorf("got %d sessions, want 0", len(sessions))
	}
}

func TestGetSession_Success(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	created, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	session, err := svc.GetSession(ctx, "user-1", created.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if session.ID != created.ID {
		t.Errorf("session.ID = %q, want %q", session.ID, created.ID)
	}
	if session.Title != "New Chat" {
		t.Errorf("session.Title = %q, want %q", session.Title, "New Chat")
	}
}

func TestGetSession_NotFound(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	_, err := svc.GetSession(ctx, "user-1", "nonexistent")
	if err != ErrSessionNotFound {
		t.Errorf("GetSession error = %v, want ErrSessionNotFound", err)
	}
}

func TestGetSession_WrongUser(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	created, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	_, err = svc.GetSession(ctx, "user-2", created.ID)
	if err != ErrForbidden {
		t.Errorf("GetSession error = %v, want ErrForbidden", err)
	}
}

func TestDeleteSession_RemovesSessionAndMessages(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Add some messages.
	_, err = svc.SendMessage(ctx, "user-1", session.ID, "Hello")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	_, err = svc.SendMessage(ctx, "user-1", session.ID, "World")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Delete the session.
	err = svc.DeleteSession(ctx, "user-1", session.ID)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}

	// Session should not be found.
	_, err = svc.GetSession(ctx, "user-1", session.ID)
	if err != ErrSessionNotFound {
		t.Errorf("GetSession after delete: error = %v, want ErrSessionNotFound", err)
	}

	// Messages should be empty.
	msgs, err := svc.GetMessages(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("got %d messages after delete, want 0", len(msgs))
	}
}

func TestDeleteSession_NotFound(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	err := svc.DeleteSession(ctx, "user-1", "nonexistent")
	if err != ErrSessionNotFound {
		t.Errorf("DeleteSession error = %v, want ErrSessionNotFound", err)
	}
}

func TestDeleteSession_WrongUser(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = svc.DeleteSession(ctx, "user-2", session.ID)
	if err != ErrForbidden {
		t.Errorf("DeleteSession error = %v, want ErrForbidden", err)
	}

	// Session should still exist.
	_, err = svc.GetSession(ctx, "user-1", session.ID)
	if err != nil {
		t.Errorf("session was deleted despite wrong user: %v", err)
	}
}

func TestRenameSession_ValidTitle(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = svc.RenameSession(ctx, "user-1", session.ID, "My Project Chat")
	if err != nil {
		t.Fatalf("RenameSession failed: %v", err)
	}

	updated, err := svc.GetSession(ctx, "user-1", session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if updated.Title != "My Project Chat" {
		t.Errorf("title = %q, want %q", updated.Title, "My Project Chat")
	}
	if !updated.UpdatedAt.After(session.CreatedAt) || updated.UpdatedAt.Equal(session.CreatedAt) {
		t.Error("UpdatedAt was not advanced after rename")
	}
}

func TestRenameSession_TrimsWhitespace(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = svc.RenameSession(ctx, "user-1", session.ID, "  Trimmed Title  ")
	if err != nil {
		t.Fatalf("RenameSession failed: %v", err)
	}

	updated, err := svc.GetSession(ctx, "user-1", session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if updated.Title != "Trimmed Title" {
		t.Errorf("title = %q, want %q", updated.Title, "Trimmed Title")
	}
}

func TestRenameSession_EmptyTitle(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = svc.RenameSession(ctx, "user-1", session.ID, "")
	if err != ErrInvalidTitle {
		t.Errorf("RenameSession error = %v, want ErrInvalidTitle", err)
	}

	err = svc.RenameSession(ctx, "user-1", session.ID, "   ")
	if err != ErrInvalidTitle {
		t.Errorf("RenameSession with whitespace-only error = %v, want ErrInvalidTitle", err)
	}
}

func TestRenameSession_TitleTooLong(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	longTitle := strings.Repeat("a", 101)
	err = svc.RenameSession(ctx, "user-1", session.ID, longTitle)
	if err != ErrInvalidTitle {
		t.Errorf("RenameSession error = %v, want ErrInvalidTitle", err)
	}
}

func TestRenameSession_ExactlyMaxLength(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	title100 := strings.Repeat("b", 100)
	err = svc.RenameSession(ctx, "user-1", session.ID, title100)
	if err != nil {
		t.Fatalf("RenameSession with 100-char title failed: %v", err)
	}

	updated, err := svc.GetSession(ctx, "user-1", session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if updated.Title != title100 {
		t.Errorf("title length = %d, want 100", len(updated.Title))
	}
}

func TestRenameSession_NotFound(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	err := svc.RenameSession(ctx, "user-1", "nonexistent", "New Title")
	if err != ErrSessionNotFound {
		t.Errorf("RenameSession error = %v, want ErrSessionNotFound", err)
	}
}

func TestRenameSession_WrongUser(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = svc.RenameSession(ctx, "user-2", session.ID, "Hacked Title")
	if err != ErrForbidden {
		t.Errorf("RenameSession error = %v, want ErrForbidden", err)
	}
}

func TestSendMessage_PersistsWithSequence(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	msg1, err := svc.SendMessage(ctx, "user-1", session.ID, "First message")
	if err != nil {
		t.Fatalf("SendMessage 1 failed: %v", err)
	}
	if msg1.Seq != 1 {
		t.Errorf("msg1.Seq = %d, want 1", msg1.Seq)
	}
	if msg1.Role != "user" {
		t.Errorf("msg1.Role = %q, want %q", msg1.Role, "user")
	}
	if msg1.Content != "First message" {
		t.Errorf("msg1.Content = %q, want %q", msg1.Content, "First message")
	}

	msg2, err := svc.SendMessage(ctx, "user-1", session.ID, "Second message")
	if err != nil {
		t.Fatalf("SendMessage 2 failed: %v", err)
	}
	if msg2.Seq != 2 {
		t.Errorf("msg2.Seq = %d, want 2", msg2.Seq)
	}
}

func TestGetMessages_OrderedBySeq(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	for i := 0; i < 5; i++ {
		_, err := svc.SendMessage(ctx, "user-1", session.ID, "Message")
		if err != nil {
			t.Fatalf("SendMessage %d failed: %v", i, err)
		}
	}

	msgs, err := svc.GetMessages(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(msgs) != 5 {
		t.Fatalf("got %d messages, want 5", len(msgs))
	}

	for i, msg := range msgs {
		expectedSeq := i + 1
		if msg.Seq != expectedSeq {
			t.Errorf("msgs[%d].Seq = %d, want %d", i, msg.Seq, expectedSeq)
		}
	}
}

func TestGetMessages_EmptySession(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	msgs, err := svc.GetMessages(ctx, "nonexistent-session")
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("got %d messages, want 0", len(msgs))
	}
}

func TestSendMessage_InvalidContent(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Empty message.
	_, err = svc.SendMessage(ctx, "user-1", session.ID, "")
	if err != ErrInvalidMessage {
		t.Errorf("SendMessage empty error = %v, want ErrInvalidMessage", err)
	}

	// Whitespace-only message.
	_, err = svc.SendMessage(ctx, "user-1", session.ID, "   ")
	if err != ErrInvalidMessage {
		t.Errorf("SendMessage whitespace error = %v, want ErrInvalidMessage", err)
	}

	// Message too long.
	longMsg := strings.Repeat("x", 32001)
	_, err = svc.SendMessage(ctx, "user-1", session.ID, longMsg)
	if err != ErrInvalidMessage {
		t.Errorf("SendMessage too long error = %v, want ErrInvalidMessage", err)
	}
}

func TestSendMessage_UpdatesSessionTimestamp(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	originalUpdatedAt := session.UpdatedAt
	time.Sleep(time.Millisecond)

	_, err = svc.SendMessage(ctx, "user-1", session.ID, "Hello")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	updated, err := svc.GetSession(ctx, "user-1", session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if !updated.UpdatedAt.After(originalUpdatedAt) {
		t.Error("session UpdatedAt was not advanced after SendMessage")
	}
}

// --- GetRecentHistory tests ---

func TestGetRecentHistory_ReturnsAllWhenUnderLimit(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Add 5 messages.
	for i := 0; i < 5; i++ {
		_, err := svc.SendMessage(ctx, "user-1", session.ID, fmt.Sprintf("Message %d", i+1))
		if err != nil {
			t.Fatalf("SendMessage %d failed: %v", i, err)
		}
	}

	history, err := svc.GetRecentHistory(ctx, session.ID, 200)
	if err != nil {
		t.Fatalf("GetRecentHistory failed: %v", err)
	}

	if len(history) != 5 {
		t.Fatalf("got %d history items, want 5", len(history))
	}

	// Verify chronological order.
	for i, item := range history {
		expected := fmt.Sprintf("Message %d", i+1)
		if item.Content != expected {
			t.Errorf("history[%d].Content = %q, want %q", i, item.Content, expected)
		}
		if item.Role != "user" {
			t.Errorf("history[%d].Role = %q, want %q", i, item.Role, "user")
		}
	}
}

func TestGetRecentHistory_TruncatesAtLimit(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Add 250 messages (exceeds MaxHistoryMessages of 200).
	for i := 0; i < 250; i++ {
		_, err := svc.SendMessage(ctx, "user-1", session.ID, fmt.Sprintf("Message %d", i+1))
		if err != nil {
			t.Fatalf("SendMessage %d failed: %v", i, err)
		}
	}

	history, err := svc.GetRecentHistory(ctx, session.ID, MaxHistoryMessages)
	if err != nil {
		t.Fatalf("GetRecentHistory failed: %v", err)
	}

	if len(history) != MaxHistoryMessages {
		t.Fatalf("got %d history items, want %d", len(history), MaxHistoryMessages)
	}

	// Should contain messages 51-250 (the last 200).
	if history[0].Content != "Message 51" {
		t.Errorf("first history item = %q, want %q", history[0].Content, "Message 51")
	}
	if history[199].Content != "Message 250" {
		t.Errorf("last history item = %q, want %q", history[199].Content, "Message 250")
	}
}

func TestGetRecentHistory_CustomLimit(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Add 10 messages.
	for i := 0; i < 10; i++ {
		_, err := svc.SendMessage(ctx, "user-1", session.ID, fmt.Sprintf("Message %d", i+1))
		if err != nil {
			t.Fatalf("SendMessage %d failed: %v", i, err)
		}
	}

	// Request only last 3.
	history, err := svc.GetRecentHistory(ctx, session.ID, 3)
	if err != nil {
		t.Fatalf("GetRecentHistory failed: %v", err)
	}

	if len(history) != 3 {
		t.Fatalf("got %d history items, want 3", len(history))
	}

	// Should contain messages 8, 9, 10.
	expectedContents := []string{"Message 8", "Message 9", "Message 10"}
	for i, item := range history {
		if item.Content != expectedContents[i] {
			t.Errorf("history[%d].Content = %q, want %q", i, item.Content, expectedContents[i])
		}
	}
}

func TestGetRecentHistory_EmptySession(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	history, err := svc.GetRecentHistory(ctx, session.ID, 200)
	if err != nil {
		t.Fatalf("GetRecentHistory failed: %v", err)
	}

	if len(history) != 0 {
		t.Errorf("got %d history items, want 0", len(history))
	}
}

func TestGetRecentHistory_NonexistentSession(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	history, err := svc.GetRecentHistory(ctx, "nonexistent", 200)
	if err != nil {
		t.Fatalf("GetRecentHistory failed: %v", err)
	}

	if len(history) != 0 {
		t.Errorf("got %d history items, want 0", len(history))
	}
}

// --- BuildTaskPayload tests ---

func TestBuildTaskPayload_BasicPayload(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Bind a runtime.
	runtimeID := "rt-test-1"
	session.RuntimeID = &runtimeID

	// Send a message.
	msg, err := svc.SendMessage(ctx, "user-1", session.ID, "Hello agent")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	payload, err := svc.BuildTaskPayload(ctx, session.ID, msg)
	if err != nil {
		t.Fatalf("BuildTaskPayload failed: %v", err)
	}

	if payload.SessionID != session.ID {
		t.Errorf("payload.SessionID = %q, want %q", payload.SessionID, session.ID)
	}
	if payload.MessageID != msg.ID {
		t.Errorf("payload.MessageID = %q, want %q", payload.MessageID, msg.ID)
	}
	if payload.Content != "Hello agent" {
		t.Errorf("payload.Content = %q, want %q", payload.Content, "Hello agent")
	}
	if payload.RuntimeID != runtimeID {
		t.Errorf("payload.RuntimeID = %q, want %q", payload.RuntimeID, runtimeID)
	}
	if len(payload.History) != 1 {
		t.Fatalf("payload.History has %d items, want 1", len(payload.History))
	}
	if payload.History[0].Role != "user" || payload.History[0].Content != "Hello agent" {
		t.Errorf("payload.History[0] = %+v, want {Role:user, Content:Hello agent}", payload.History[0])
	}
}

func TestBuildTaskPayload_TruncatesHistoryAt200(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Add 250 messages.
	var lastMsg *ChatMessage
	for i := 0; i < 250; i++ {
		m, err := svc.SendMessage(ctx, "user-1", session.ID, fmt.Sprintf("Message %d", i+1))
		if err != nil {
			t.Fatalf("SendMessage %d failed: %v", i, err)
		}
		lastMsg = m
	}

	payload, err := svc.BuildTaskPayload(ctx, session.ID, lastMsg)
	if err != nil {
		t.Fatalf("BuildTaskPayload failed: %v", err)
	}

	// History should be capped at 200.
	if len(payload.History) != MaxHistoryMessages {
		t.Fatalf("payload.History has %d items, want %d", len(payload.History), MaxHistoryMessages)
	}

	// Should contain messages 51-250 (the last 200).
	if payload.History[0].Content != "Message 51" {
		t.Errorf("first history item = %q, want %q", payload.History[0].Content, "Message 51")
	}
	if payload.History[199].Content != "Message 250" {
		t.Errorf("last history item = %q, want %q", payload.History[199].Content, "Message 250")
	}
}

func TestBuildTaskPayload_NoRuntimeBound(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	msg, err := svc.SendMessage(ctx, "user-1", session.ID, "Hello")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	payload, err := svc.BuildTaskPayload(ctx, session.ID, msg)
	if err != nil {
		t.Fatalf("BuildTaskPayload failed: %v", err)
	}

	// RuntimeID should be empty string when no runtime is bound.
	if payload.RuntimeID != "" {
		t.Errorf("payload.RuntimeID = %q, want empty string", payload.RuntimeID)
	}
}

func TestBuildTaskPayload_NonexistentSession(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	msg := &ChatMessage{
		ID:      "msg-1",
		Content: "Hello",
	}

	_, err := svc.BuildTaskPayload(ctx, "nonexistent", msg)
	if err != ErrSessionNotFound {
		t.Errorf("BuildTaskPayload error = %v, want ErrSessionNotFound", err)
	}
}

func TestBuildTaskPayload_HistoryInChronologicalOrder(t *testing.T) {
	svc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := svc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Send messages in order.
	contents := []string{"First", "Second", "Third", "Fourth", "Fifth"}
	var lastMsg *ChatMessage
	for _, c := range contents {
		m, err := svc.SendMessage(ctx, "user-1", session.ID, c)
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}
		lastMsg = m
	}

	payload, err := svc.BuildTaskPayload(ctx, session.ID, lastMsg)
	if err != nil {
		t.Fatalf("BuildTaskPayload failed: %v", err)
	}

	if len(payload.History) != 5 {
		t.Fatalf("payload.History has %d items, want 5", len(payload.History))
	}

	// Verify chronological order.
	for i, item := range payload.History {
		if item.Content != contents[i] {
			t.Errorf("payload.History[%d].Content = %q, want %q", i, item.Content, contents[i])
		}
	}
}


// --- BindSessionRuntime tests ---

func setupRuntimeService(t *testing.T, userID, daemonID string, runtimes []struct {
	agentType string
	status    string
}) (*InMemoryRuntimeService, []string) {
	t.Helper()
	ctx := context.Background()
	runtimeSvc := NewInMemoryRuntimeService()

	var infos []struct {
		AgentType  string
		BinaryPath string
		Version    string
		Status     string
	}
	for _, r := range runtimes {
		infos = append(infos, struct {
			AgentType  string
			BinaryPath string
			Version    string
			Status     string
		}{
			AgentType:  r.agentType,
			BinaryPath: "/usr/bin/" + r.agentType,
			Version:    "1.0.0",
			Status:     r.status,
		})
	}

	// Build protocol.RuntimeInfo slice.
	var protoRuntimes []protocol.RuntimeInfo
	for _, info := range infos {
		protoRuntimes = append(protoRuntimes, protocol.RuntimeInfo{
			AgentType:  info.AgentType,
			BinaryPath: info.BinaryPath,
			Version:    info.Version,
			Status:     info.Status,
		})
	}

	err := runtimeSvc.RegisterDaemon(ctx, DaemonRegistration{
		DaemonID: daemonID,
		UserID:   userID,
		Runtimes: protoRuntimes,
	})
	if err != nil {
		t.Fatalf("RegisterDaemon failed: %v", err)
	}

	// Collect runtime IDs.
	runtimeIDs := make([]string, 0)
	for _, rt := range runtimeSvc.runtimes {
		if rt.DaemonID == daemonID {
			runtimeIDs = append(runtimeIDs, rt.ID)
		}
	}

	return runtimeSvc, runtimeIDs
}

func TestBindSessionRuntime_Success(t *testing.T) {
	chatSvc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := chatSvc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	runtimeSvc, runtimeIDs := setupRuntimeService(t, "user-1", "daemon-1", []struct {
		agentType string
		status    string
	}{
		{agentType: "claude", status: "available"},
	})

	if len(runtimeIDs) == 0 {
		t.Fatal("no runtime IDs created")
	}

	time.Sleep(time.Millisecond)
	err = chatSvc.BindSessionRuntime(ctx, "user-1", session.ID, runtimeIDs[0], runtimeSvc)
	if err != nil {
		t.Fatalf("BindSessionRuntime failed: %v", err)
	}

	// Verify RuntimeID is set.
	updated, err := chatSvc.GetSession(ctx, "user-1", session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if updated.RuntimeID == nil {
		t.Fatal("RuntimeID is nil after binding")
	}
	if *updated.RuntimeID != runtimeIDs[0] {
		t.Errorf("RuntimeID = %q, want %q", *updated.RuntimeID, runtimeIDs[0])
	}
	if !updated.UpdatedAt.After(session.CreatedAt) {
		t.Error("UpdatedAt was not advanced after binding")
	}
}

func TestBindSessionRuntime_OfflineRuntime(t *testing.T) {
	chatSvc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := chatSvc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	runtimeSvc, runtimeIDs := setupRuntimeService(t, "user-1", "daemon-1", []struct {
		agentType string
		status    string
	}{
		{agentType: "claude", status: "offline"},
	})

	if len(runtimeIDs) == 0 {
		t.Fatal("no runtime IDs created")
	}

	err = chatSvc.BindSessionRuntime(ctx, "user-1", session.ID, runtimeIDs[0], runtimeSvc)
	if err != ErrRuntimeOffline {
		t.Errorf("BindSessionRuntime error = %v, want ErrRuntimeOffline", err)
	}

	// Verify RuntimeID is still nil.
	updated, err := chatSvc.GetSession(ctx, "user-1", session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if updated.RuntimeID != nil {
		t.Errorf("RuntimeID = %v, want nil (binding should have been rejected)", updated.RuntimeID)
	}
}

func TestBindSessionRuntime_NonexistentRuntime(t *testing.T) {
	chatSvc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := chatSvc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	runtimeSvc := NewInMemoryRuntimeService()

	err = chatSvc.BindSessionRuntime(ctx, "user-1", session.ID, "nonexistent-runtime", runtimeSvc)
	if err != ErrRuntimeNotFound {
		t.Errorf("BindSessionRuntime error = %v, want ErrRuntimeNotFound", err)
	}
}

func TestBindSessionRuntime_RebindPreservesMessages(t *testing.T) {
	chatSvc := NewInMemoryChatService()
	ctx := context.Background()

	session, err := chatSvc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Add messages before binding.
	_, err = chatSvc.SendMessage(ctx, "user-1", session.ID, "Hello agent")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}
	_, err = chatSvc.SendMessage(ctx, "user-1", session.ID, "How are you?")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	runtimeSvc, runtimeIDs := setupRuntimeService(t, "user-1", "daemon-1", []struct {
		agentType string
		status    string
	}{
		{agentType: "claude", status: "available"},
		{agentType: "gemini", status: "available"},
	})

	if len(runtimeIDs) < 2 {
		t.Fatalf("expected at least 2 runtime IDs, got %d", len(runtimeIDs))
	}

	// Bind to first runtime.
	err = chatSvc.BindSessionRuntime(ctx, "user-1", session.ID, runtimeIDs[0], runtimeSvc)
	if err != nil {
		t.Fatalf("BindSessionRuntime (first) failed: %v", err)
	}

	// Add another message after first binding.
	_, err = chatSvc.SendMessage(ctx, "user-1", session.ID, "Third message")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// Rebind to second runtime.
	err = chatSvc.BindSessionRuntime(ctx, "user-1", session.ID, runtimeIDs[1], runtimeSvc)
	if err != nil {
		t.Fatalf("BindSessionRuntime (second) failed: %v", err)
	}

	// Verify the binding was updated.
	updated, err := chatSvc.GetSession(ctx, "user-1", session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if updated.RuntimeID == nil || *updated.RuntimeID != runtimeIDs[1] {
		t.Errorf("RuntimeID = %v, want %q", updated.RuntimeID, runtimeIDs[1])
	}

	// Verify all messages are preserved.
	msgs, err := chatSvc.GetMessages(ctx, session.ID)
	if err != nil {
		t.Fatalf("GetMessages failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Errorf("got %d messages after rebind, want 3", len(msgs))
	}

	// Verify message order.
	expectedContents := []string{"Hello agent", "How are you?", "Third message"}
	for i, msg := range msgs {
		if msg.Content != expectedContents[i] {
			t.Errorf("msgs[%d].Content = %q, want %q", i, msg.Content, expectedContents[i])
		}
		if msg.Seq != i+1 {
			t.Errorf("msgs[%d].Seq = %d, want %d", i, msg.Seq, i+1)
		}
	}
}

func TestBindSessionRuntime_WrongUser(t *testing.T) {
	chatSvc := NewInMemoryChatService()
	ctx := context.Background()

	// Create session for user-1.
	session, err := chatSvc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	runtimeSvc, runtimeIDs := setupRuntimeService(t, "user-2", "daemon-2", []struct {
		agentType string
		status    string
	}{
		{agentType: "claude", status: "available"},
	})

	if len(runtimeIDs) == 0 {
		t.Fatal("no runtime IDs created")
	}

	// user-2 tries to bind to user-1's session.
	err = chatSvc.BindSessionRuntime(ctx, "user-2", session.ID, runtimeIDs[0], runtimeSvc)
	if err != ErrForbidden {
		t.Errorf("BindSessionRuntime error = %v, want ErrForbidden", err)
	}

	// Verify session is unchanged.
	updated, err := chatSvc.GetSession(ctx, "user-1", session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if updated.RuntimeID != nil {
		t.Errorf("RuntimeID = %v, want nil (binding by wrong user should be rejected)", updated.RuntimeID)
	}
}
