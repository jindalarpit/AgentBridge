package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/user/agentbridge/server/pkg/protocol"
	"pgregory.net/rapid"
)

// **Validates: Requirements 4.2**
// Property 7: Session List Ordering
// For any user with N chat sessions having various message timestamps,
// listing sessions SHALL return them ordered by most recent activity
// (last message timestamp, or creation timestamp if no messages) in
// descending order, with at most 50 sessions per page.

// TestProperty_SessionListOrdering_DescendingOrder verifies that for any user
// with N sessions (1-60), ListSessions returns them in descending UpdatedAt order.
func TestProperty_SessionListOrdering_DescendingOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := "user-ordering"

		// Generate 1-60 sessions.
		n := rapid.IntRange(1, 60).Draw(t, "num_sessions")

		// Create sessions with varying activity patterns.
		sessionIDs := make([]string, n)
		for i := 0; i < n; i++ {
			session, err := svc.CreateSession(ctx, userID)
			if err != nil {
				t.Fatalf("CreateSession failed: %v", err)
			}
			sessionIDs[i] = session.ID

			// Ensure distinct timestamps between sessions.
			time.Sleep(time.Microsecond)
		}

		// Randomly send messages to some sessions to update their UpdatedAt.
		numUpdates := rapid.IntRange(0, n).Draw(t, "num_updates")
		for i := 0; i < numUpdates; i++ {
			idx := rapid.IntRange(0, n-1).Draw(t, fmt.Sprintf("update_idx_%d", i))
			content := fmt.Sprintf("message-%d", i)
			_, err := svc.SendMessage(ctx, userID, sessionIDs[idx], content)
			if err != nil {
				t.Fatalf("SendMessage failed: %v", err)
			}
			time.Sleep(time.Microsecond)
		}

		// List all sessions (may need multiple pages if n > 50).
		sessions, _, err := svc.ListSessions(ctx, userID, 1, 50)
		if err != nil {
			t.Fatalf("ListSessions failed: %v", err)
		}

		// Verify descending order by UpdatedAt.
		for i := 1; i < len(sessions); i++ {
			if sessions[i].UpdatedAt.After(sessions[i-1].UpdatedAt) {
				t.Fatalf("sessions not in descending UpdatedAt order: index %d (%v) is after index %d (%v)",
					i, sessions[i].UpdatedAt, i-1, sessions[i-1].UpdatedAt)
			}
		}
	})
}

// TestProperty_SessionListOrdering_MaxPageSize50 verifies that for any user
// with N > 50 sessions, the first page returns exactly 50 sessions.
func TestProperty_SessionListOrdering_MaxPageSize50(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := "user-pagesize"

		// Generate 51-60 sessions (always more than 50).
		n := rapid.IntRange(51, 60).Draw(t, "num_sessions")

		for i := 0; i < n; i++ {
			_, err := svc.CreateSession(ctx, userID)
			if err != nil {
				t.Fatalf("CreateSession failed: %v", err)
			}
		}

		// Request first page with pageSize 50.
		sessions, total, err := svc.ListSessions(ctx, userID, 1, 50)
		if err != nil {
			t.Fatalf("ListSessions failed: %v", err)
		}

		if len(sessions) != 50 {
			t.Fatalf("expected exactly 50 sessions on first page, got %d", len(sessions))
		}
		if total != n {
			t.Fatalf("expected total = %d, got %d", n, total)
		}
	})
}

// TestProperty_SessionListOrdering_TotalCountEqualsN verifies that for any user
// with N sessions, the total count always equals N regardless of page.
func TestProperty_SessionListOrdering_TotalCountEqualsN(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := "user-total"

		// Generate 1-60 sessions.
		n := rapid.IntRange(1, 60).Draw(t, "num_sessions")

		for i := 0; i < n; i++ {
			_, err := svc.CreateSession(ctx, userID)
			if err != nil {
				t.Fatalf("CreateSession failed: %v", err)
			}
		}

		// Check total on various pages.
		page := rapid.IntRange(1, 5).Draw(t, "page")
		pageSize := rapid.IntRange(1, 50).Draw(t, "page_size")

		_, total, err := svc.ListSessions(ctx, userID, page, pageSize)
		if err != nil {
			t.Fatalf("ListSessions failed: %v", err)
		}

		if total != n {
			t.Fatalf("expected total = %d, got %d (page=%d, pageSize=%d)", n, total, page, pageSize)
		}
	})
}

// TestProperty_SessionListOrdering_PageSizeClampedTo50 verifies that for any
// pageSize > 50, it is clamped to 50 (i.e., at most 50 results returned).
func TestProperty_SessionListOrdering_PageSizeClampedTo50(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := "user-clamp"

		// Create enough sessions to fill a page.
		n := rapid.IntRange(51, 60).Draw(t, "num_sessions")
		for i := 0; i < n; i++ {
			_, err := svc.CreateSession(ctx, userID)
			if err != nil {
				t.Fatalf("CreateSession failed: %v", err)
			}
		}

		// Request with pageSize > 50.
		largePageSize := rapid.IntRange(51, 200).Draw(t, "large_page_size")
		sessions, _, err := svc.ListSessions(ctx, userID, 1, largePageSize)
		if err != nil {
			t.Fatalf("ListSessions failed: %v", err)
		}

		if len(sessions) > 50 {
			t.Fatalf("expected at most 50 sessions when pageSize=%d, got %d", largePageSize, len(sessions))
		}
	})
}

// **Validates: Requirements 4.4**
// Property 8: Session Deletion Completeness
// For any chat session with N messages, after deletion, querying for that
// session SHALL return not-found, and querying for any of its messages SHALL
// return an empty result set.

// TestProperty_SessionDeletionCompleteness_GetSessionReturnsNotFound verifies that
// for any user creating a session with N messages (0-50), after DeleteSession,
// GetSession returns ErrSessionNotFound.
func TestProperty_SessionDeletionCompleteness_GetSessionReturnsNotFound(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`^user-[a-z0-9]{4,8}$`).Draw(t, "userID")

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Add N messages (0-50).
		n := rapid.IntRange(0, 50).Draw(t, "num_messages")
		for i := 0; i < n; i++ {
			content := fmt.Sprintf("message-%d-content", i+1)
			_, err := svc.SendMessage(ctx, userID, session.ID, content)
			if err != nil {
				t.Fatalf("SendMessage failed at message %d: %v", i+1, err)
			}
		}

		// Delete the session.
		err = svc.DeleteSession(ctx, userID, session.ID)
		if err != nil {
			t.Fatalf("DeleteSession failed: %v", err)
		}

		// Verify GetSession returns ErrSessionNotFound.
		_, err = svc.GetSession(ctx, userID, session.ID)
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound after deletion, got: %v", err)
		}
	})
}

// TestProperty_SessionDeletionCompleteness_GetMessagesReturnsEmpty verifies that
// for any user creating a session with N messages (0-50), after DeleteSession,
// GetMessages returns an empty slice.
func TestProperty_SessionDeletionCompleteness_GetMessagesReturnsEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`^user-[a-z0-9]{4,8}$`).Draw(t, "userID")

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Add N messages (0-50).
		n := rapid.IntRange(0, 50).Draw(t, "num_messages")
		for i := 0; i < n; i++ {
			content := fmt.Sprintf("message-%d-content", i+1)
			_, err := svc.SendMessage(ctx, userID, session.ID, content)
			if err != nil {
				t.Fatalf("SendMessage failed at message %d: %v", i+1, err)
			}
		}

		// Verify messages exist before deletion (if any were added).
		if n > 0 {
			msgs, err := svc.GetMessages(ctx, session.ID)
			if err != nil {
				t.Fatalf("GetMessages before deletion failed: %v", err)
			}
			if len(msgs) != n {
				t.Fatalf("expected %d messages before deletion, got %d", n, len(msgs))
			}
		}

		// Delete the session.
		err = svc.DeleteSession(ctx, userID, session.ID)
		if err != nil {
			t.Fatalf("DeleteSession failed: %v", err)
		}

		// Verify GetMessages returns an empty slice.
		msgs, err := svc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages after deletion returned error: %v", err)
		}
		if len(msgs) != 0 {
			t.Fatalf("expected 0 messages after deletion, got %d", len(msgs))
		}
	})
}

// TestProperty_SessionDeletionCompleteness_OtherSessionsUnaffected verifies that
// deletion of one session does not affect other sessions belonging to the same user.
func TestProperty_SessionDeletionCompleteness_OtherSessionsUnaffected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`^user-[a-z0-9]{4,8}$`).Draw(t, "userID")

		// Create 2-5 sessions.
		numSessions := rapid.IntRange(2, 5).Draw(t, "num_sessions")
		sessions := make([]*ChatSession, numSessions)
		for i := 0; i < numSessions; i++ {
			session, err := svc.CreateSession(ctx, userID)
			if err != nil {
				t.Fatalf("CreateSession %d failed: %v", i, err)
			}
			sessions[i] = session
			time.Sleep(time.Microsecond)
		}

		// Add messages to each session.
		sessionMsgCounts := make([]int, numSessions)
		for i := 0; i < numSessions; i++ {
			n := rapid.IntRange(0, 10).Draw(t, fmt.Sprintf("msgs_session_%d", i))
			sessionMsgCounts[i] = n
			for j := 0; j < n; j++ {
				content := fmt.Sprintf("session-%d-msg-%d", i, j+1)
				_, err := svc.SendMessage(ctx, userID, sessions[i].ID, content)
				if err != nil {
					t.Fatalf("SendMessage failed for session %d, msg %d: %v", i, j+1, err)
				}
			}
		}

		// Pick a random session to delete.
		deleteIdx := rapid.IntRange(0, numSessions-1).Draw(t, "delete_idx")
		deletedSessionID := sessions[deleteIdx].ID

		// Delete the chosen session.
		err := svc.DeleteSession(ctx, userID, deletedSessionID)
		if err != nil {
			t.Fatalf("DeleteSession failed: %v", err)
		}

		// Verify the deleted session is gone.
		_, err = svc.GetSession(ctx, userID, deletedSessionID)
		if !errors.Is(err, ErrSessionNotFound) {
			t.Fatalf("expected ErrSessionNotFound for deleted session, got: %v", err)
		}

		// Verify all other sessions are still intact with their messages.
		for i := 0; i < numSessions; i++ {
			if i == deleteIdx {
				continue
			}

			// Session should still be retrievable.
			sess, err := svc.GetSession(ctx, userID, sessions[i].ID)
			if err != nil {
				t.Fatalf("GetSession for surviving session %d failed: %v", i, err)
			}
			if sess.ID != sessions[i].ID {
				t.Fatalf("surviving session %d has wrong ID: expected %s, got %s", i, sessions[i].ID, sess.ID)
			}

			// Messages should still be intact.
			msgs, err := svc.GetMessages(ctx, sessions[i].ID)
			if err != nil {
				t.Fatalf("GetMessages for surviving session %d failed: %v", i, err)
			}
			if len(msgs) != sessionMsgCounts[i] {
				t.Fatalf("surviving session %d: expected %d messages, got %d", i, sessionMsgCounts[i], len(msgs))
			}
		}
	})
}

// **Validates: Requirements 6.1, 9.1**
// Property 17: Message Persistence Round-Trip
// For any valid chat message (user or assistant role) with content C, after
// persisting and then retrieving it, the returned message SHALL have identical
// content C, correct role, correct session_id, a valid timestamp, and a sequence
// number consistent with its position in the session.

// TestProperty_MessagePersistenceRoundTrip_UserMessage verifies that for any valid
// user message with content C, after persisting via SendMessage and retrieving via
// GetMessages, the returned message has identical content (after trimming, as the
// service trims whitespace), role "user", correct session_id, a valid timestamp,
// and the correct sequence number.
func TestProperty_MessagePersistenceRoundTrip_UserMessage(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`^user-[a-z0-9]{4,8}$`).Draw(t, "userID")

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Generate valid message content that won't be empty after trimming.
		// Use non-whitespace-leading content to ensure trimmed == original.
		content := rapid.StringMatching(`^[a-zA-Z][a-zA-Z0-9 .,!?]{0,999}$`).Draw(t, "content")

		// The service trims whitespace, so the expected stored content is the trimmed version.
		expectedContent := strings.TrimSpace(content)

		beforeSend := time.Now()

		// Persist the user message via SendMessage.
		sentMsg, err := svc.SendMessage(ctx, userID, session.ID, content)
		if err != nil {
			t.Fatalf("SendMessage failed: %v", err)
		}

		afterSend := time.Now()

		// Retrieve messages.
		msgs, err := svc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}

		if len(msgs) != 1 {
			t.Fatalf("expected 1 message, got %d", len(msgs))
		}

		retrieved := msgs[0]

		// Verify content matches the trimmed input (SendMessage trims whitespace).
		if retrieved.Content != expectedContent {
			t.Fatalf("content mismatch: expected %q, got %q", expectedContent, retrieved.Content)
		}

		// Verify role is "user".
		if retrieved.Role != "user" {
			t.Fatalf("role mismatch: expected \"user\", got %q", retrieved.Role)
		}

		// Verify session_id matches.
		if retrieved.ChatSessionID != session.ID {
			t.Fatalf("session_id mismatch: expected %q, got %q", session.ID, retrieved.ChatSessionID)
		}

		// Verify timestamp is valid (between beforeSend and afterSend).
		if retrieved.CreatedAt.Before(beforeSend) || retrieved.CreatedAt.After(afterSend) {
			t.Fatalf("timestamp out of range: got %v, expected between %v and %v",
				retrieved.CreatedAt, beforeSend, afterSend)
		}

		// Verify sequence number is 1 (first message in session).
		if retrieved.Seq != 1 {
			t.Fatalf("seq mismatch: expected 1, got %d", retrieved.Seq)
		}

		// Verify the returned message from SendMessage matches the retrieved one.
		if sentMsg.ID != retrieved.ID {
			t.Fatalf("message ID mismatch: sent %q, retrieved %q", sentMsg.ID, retrieved.ID)
		}
	})
}

// TestProperty_MessagePersistenceRoundTrip_AssistantMessage verifies that for any
// valid assistant message with content C, after persisting via PersistAssistantMessage
// and retrieving via GetMessages, the returned message has identical content, role
// "assistant", correct session_id, a valid timestamp, and the correct sequence number.
func TestProperty_MessagePersistenceRoundTrip_AssistantMessage(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`^user-[a-z0-9]{4,8}$`).Draw(t, "userID")

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// First send a user message (assistant messages follow user messages).
		_, err = svc.SendMessage(ctx, userID, session.ID, "hello agent")
		if err != nil {
			t.Fatalf("SendMessage (user) failed: %v", err)
		}

		// Generate valid assistant message content (1-1000 chars).
		content := rapid.StringMatching(`^[a-zA-Z0-9 .,!?]{1,1000}$`).Draw(t, "content")
		messageID := generateID()
		elapsedMs := int64(rapid.IntRange(10, 5000).Draw(t, "elapsed_ms"))

		beforePersist := time.Now()

		// Persist the assistant message.
		persistedMsg, err := svc.PersistAssistantMessage(ctx, session.ID, messageID, content, elapsedMs)
		if err != nil {
			t.Fatalf("PersistAssistantMessage failed: %v", err)
		}

		afterPersist := time.Now()

		// Retrieve messages.
		msgs, err := svc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}

		if len(msgs) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(msgs))
		}

		// The assistant message should be the second one (seq=2).
		retrieved := msgs[1]

		// Verify content matches exactly.
		if retrieved.Content != content {
			t.Fatalf("content mismatch: expected %q, got %q", content, retrieved.Content)
		}

		// Verify role is "assistant".
		if retrieved.Role != "assistant" {
			t.Fatalf("role mismatch: expected \"assistant\", got %q", retrieved.Role)
		}

		// Verify session_id matches.
		if retrieved.ChatSessionID != session.ID {
			t.Fatalf("session_id mismatch: expected %q, got %q", session.ID, retrieved.ChatSessionID)
		}

		// Verify timestamp is valid (between beforePersist and afterPersist).
		if retrieved.CreatedAt.Before(beforePersist) || retrieved.CreatedAt.After(afterPersist) {
			t.Fatalf("timestamp out of range: got %v, expected between %v and %v",
				retrieved.CreatedAt, beforePersist, afterPersist)
		}

		// Verify sequence number is 2 (second message in session, after user message).
		if retrieved.Seq != 2 {
			t.Fatalf("seq mismatch: expected 2, got %d", retrieved.Seq)
		}

		// Verify the persisted message ID matches.
		if persistedMsg.ID != retrieved.ID {
			t.Fatalf("message ID mismatch: persisted %q, retrieved %q", persistedMsg.ID, retrieved.ID)
		}

		// Verify the message ID matches what we provided.
		if retrieved.ID != messageID {
			t.Fatalf("message ID mismatch: expected %q, got %q", messageID, retrieved.ID)
		}
	})
}

// **Validates: Requirements 9.2, 9.4**
// Property 18: Message Ordering Invariant
// For any chat session with N messages persisted in any order, retrieving the
// message history SHALL return exactly N messages ordered by sequence number
// (1, 2, 3, ..., N) with no gaps or duplicates, and the sequence order SHALL
// correspond to chronological insertion order.

// TestProperty_MessageOrderingInvariant_ExactCountAndSequence verifies that for any
// chat session with N messages (mix of user and assistant), GetMessages returns
// exactly N messages with sequence numbers 1, 2, 3, ..., N with no gaps or duplicates.
func TestProperty_MessageOrderingInvariant_ExactCountAndSequence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`^user-[a-z0-9]{4,8}$`).Draw(t, "userID")

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Generate N messages (1-30) with a random mix of user and assistant roles.
		n := rapid.IntRange(1, 30).Draw(t, "num_messages")

		for i := 0; i < n; i++ {
			// Randomly choose to insert a user message or an assistant message.
			isUser := rapid.Bool().Draw(t, fmt.Sprintf("is_user_%d", i))

			if isUser {
				content := fmt.Sprintf("user-msg-%d-%s", i,
					rapid.StringMatching(`^[a-z]{1,20}$`).Draw(t, fmt.Sprintf("user_content_%d", i)))
				_, err := svc.SendMessage(ctx, userID, session.ID, content)
				if err != nil {
					t.Fatalf("SendMessage failed at message %d: %v", i, err)
				}
			} else {
				content := fmt.Sprintf("assistant-msg-%d-%s", i,
					rapid.StringMatching(`^[a-z]{1,20}$`).Draw(t, fmt.Sprintf("assistant_content_%d", i)))
				elapsedMs := int64(rapid.IntRange(10, 5000).Draw(t, fmt.Sprintf("elapsed_%d", i)))
				_, err := svc.PersistAssistantMessage(ctx, session.ID, generateID(), content, elapsedMs)
				if err != nil {
					t.Fatalf("PersistAssistantMessage failed at message %d: %v", i, err)
				}
			}
		}

		// Retrieve messages.
		msgs, err := svc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}

		// Verify exactly N messages returned.
		if len(msgs) != n {
			t.Fatalf("expected %d messages, got %d", n, len(msgs))
		}

		// Verify sequence numbers are 1, 2, 3, ..., N with no gaps or duplicates.
		seenSeqs := make(map[int]bool)
		for i, msg := range msgs {
			expectedSeq := i + 1
			if msg.Seq != expectedSeq {
				t.Fatalf("message at index %d: expected seq %d, got %d", i, expectedSeq, msg.Seq)
			}
			if seenSeqs[msg.Seq] {
				t.Fatalf("duplicate sequence number %d found", msg.Seq)
			}
			seenSeqs[msg.Seq] = true
		}
	})
}

// TestProperty_MessageOrderingInvariant_ChronologicalInsertionOrder verifies that
// the sequence order corresponds to chronological insertion order: messages inserted
// earlier have lower sequence numbers than messages inserted later.
func TestProperty_MessageOrderingInvariant_ChronologicalInsertionOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`^user-[a-z0-9]{4,8}$`).Draw(t, "userID")

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Generate N messages (2-20) with a random mix of user and assistant roles.
		n := rapid.IntRange(2, 20).Draw(t, "num_messages")

		// Track insertion order.
		type insertedMsg struct {
			insertionIndex int
			role           string
			content        string
		}
		insertions := make([]insertedMsg, 0, n)

		for i := 0; i < n; i++ {
			isUser := rapid.Bool().Draw(t, fmt.Sprintf("is_user_%d", i))

			if isUser {
				content := fmt.Sprintf("user-msg-%d-%s", i,
					rapid.StringMatching(`^[a-z]{1,20}$`).Draw(t, fmt.Sprintf("user_content_%d", i)))
				_, err := svc.SendMessage(ctx, userID, session.ID, content)
				if err != nil {
					t.Fatalf("SendMessage failed at message %d: %v", i, err)
				}
				insertions = append(insertions, insertedMsg{insertionIndex: i, role: "user", content: content})
			} else {
				content := fmt.Sprintf("assistant-msg-%d-%s", i,
					rapid.StringMatching(`^[a-z]{1,20}$`).Draw(t, fmt.Sprintf("assistant_content_%d", i)))
				elapsedMs := int64(rapid.IntRange(10, 5000).Draw(t, fmt.Sprintf("elapsed_%d", i)))
				_, err := svc.PersistAssistantMessage(ctx, session.ID, generateID(), content, elapsedMs)
				if err != nil {
					t.Fatalf("PersistAssistantMessage failed at message %d: %v", i, err)
				}
				insertions = append(insertions, insertedMsg{insertionIndex: i, role: "assistant", content: content})
			}
		}

		// Retrieve messages.
		msgs, err := svc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}

		// Verify that the returned order matches insertion order.
		if len(msgs) != len(insertions) {
			t.Fatalf("expected %d messages, got %d", len(insertions), len(msgs))
		}

		for i, msg := range msgs {
			// Sequence number should match position (1-indexed).
			if msg.Seq != i+1 {
				t.Fatalf("message at index %d: expected seq %d, got %d", i, i+1, msg.Seq)
			}

			// Content should match the insertion order.
			if msg.Content != insertions[i].content {
				t.Fatalf("message at index %d: content mismatch: expected %q, got %q",
					i, insertions[i].content, msg.Content)
			}

			// Role should match the insertion order.
			if msg.Role != insertions[i].role {
				t.Fatalf("message at index %d: role mismatch: expected %q, got %q",
					i, insertions[i].role, msg.Role)
			}
		}

		// Verify timestamps are non-decreasing (chronological order).
		for i := 1; i < len(msgs); i++ {
			if msgs[i].CreatedAt.Before(msgs[i-1].CreatedAt) {
				t.Fatalf("message at index %d has timestamp %v before message at index %d with timestamp %v",
					i, msgs[i].CreatedAt, i-1, msgs[i-1].CreatedAt)
			}
		}
	})
}

// TestProperty_MessageOrderingInvariant_NoDuplicateSequenceNumbers verifies that
// for any session with N messages, no two messages share the same sequence number.
func TestProperty_MessageOrderingInvariant_NoDuplicateSequenceNumbers(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`^user-[a-z0-9]{4,8}$`).Draw(t, "userID")

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Generate N messages (1-30) with a random mix of user and assistant roles.
		n := rapid.IntRange(1, 30).Draw(t, "num_messages")

		for i := 0; i < n; i++ {
			isUser := rapid.Bool().Draw(t, fmt.Sprintf("is_user_%d", i))

			if isUser {
				content := fmt.Sprintf("user-msg-%d", i)
				_, err := svc.SendMessage(ctx, userID, session.ID, content)
				if err != nil {
					t.Fatalf("SendMessage failed at message %d: %v", i, err)
				}
			} else {
				content := fmt.Sprintf("assistant-msg-%d", i)
				_, err := svc.PersistAssistantMessage(ctx, session.ID, generateID(), content, 100)
				if err != nil {
					t.Fatalf("PersistAssistantMessage failed at message %d: %v", i, err)
				}
			}
		}

		// Retrieve messages.
		msgs, err := svc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}

		// Verify no duplicate sequence numbers.
		seenSeqs := make(map[int]bool)
		for _, msg := range msgs {
			if seenSeqs[msg.Seq] {
				t.Fatalf("duplicate sequence number %d found", msg.Seq)
			}
			seenSeqs[msg.Seq] = true
		}

		// Verify no gaps: sequence numbers should be exactly 1..N.
		for i := 1; i <= n; i++ {
			if !seenSeqs[i] {
				t.Fatalf("missing sequence number %d (expected 1..%d)", i, n)
			}
		}
	})
}

// TestProperty_MessagePersistenceRoundTrip_SequenceConsistency verifies that for
// any interleaved sequence of user and assistant messages, each message's sequence
// number is consistent with its position in the session (1, 2, 3, ..., N).
func TestProperty_MessagePersistenceRoundTrip_SequenceConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`^user-[a-z0-9]{4,8}$`).Draw(t, "userID")

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Generate a sequence of messages (2-20, alternating user/assistant).
		numPairs := rapid.IntRange(1, 10).Draw(t, "num_pairs")

		type expectedMsg struct {
			content string
			role    string
		}
		expected := make([]expectedMsg, 0, numPairs*2)

		for i := 0; i < numPairs; i++ {
			// User message.
			userContent := fmt.Sprintf("user-msg-%d-%s", i,
				rapid.StringMatching(`^[a-z]{1,50}$`).Draw(t, fmt.Sprintf("user_content_%d", i)))
			_, err := svc.SendMessage(ctx, userID, session.ID, userContent)
			if err != nil {
				t.Fatalf("SendMessage (user) %d failed: %v", i, err)
			}
			expected = append(expected, expectedMsg{content: userContent, role: "user"})

			// Assistant message.
			assistantContent := fmt.Sprintf("assistant-msg-%d-%s", i,
				rapid.StringMatching(`^[a-z]{1,50}$`).Draw(t, fmt.Sprintf("assistant_content_%d", i)))
			elapsedMs := int64(rapid.IntRange(10, 5000).Draw(t, fmt.Sprintf("elapsed_%d", i)))
			_, err = svc.PersistAssistantMessage(ctx, session.ID, generateID(), assistantContent, elapsedMs)
			if err != nil {
				t.Fatalf("PersistAssistantMessage %d failed: %v", i, err)
			}
			expected = append(expected, expectedMsg{content: assistantContent, role: "assistant"})
		}

		// Retrieve all messages.
		msgs, err := svc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}

		totalExpected := numPairs * 2
		if len(msgs) != totalExpected {
			t.Fatalf("expected %d messages, got %d", totalExpected, len(msgs))
		}

		// Verify each message has the correct sequence number, content, and role.
		for i, msg := range msgs {
			expectedSeq := i + 1
			if msg.Seq != expectedSeq {
				t.Fatalf("message %d: expected seq %d, got %d", i, expectedSeq, msg.Seq)
			}

			if msg.Content != expected[i].content {
				t.Fatalf("message %d: content mismatch: expected %q, got %q",
					i, expected[i].content, msg.Content)
			}

			if msg.Role != expected[i].role {
				t.Fatalf("message %d: role mismatch: expected %q, got %q",
					i, expected[i].role, msg.Role)
			}

			if msg.ChatSessionID != session.ID {
				t.Fatalf("message %d: session_id mismatch: expected %q, got %q",
					i, session.ID, msg.ChatSessionID)
			}

			// Verify timestamp is not zero.
			if msg.CreatedAt.IsZero() {
				t.Fatalf("message %d: timestamp is zero", i)
			}
		}
	})
}

// **Validates: Requirements 6.2**
// Property 12: History Truncation
// For any conversation with N messages (where N may exceed 200), the history
// passed to the agent CLI for execution SHALL contain exactly min(N, 200)
// messages, taken from the most recent messages in chronological order.

// TestProperty_HistoryTruncation_ExactCount verifies that for any session with
// N messages (1-300), BuildTaskPayload returns a history with exactly min(N, 200)
// messages.
func TestProperty_HistoryTruncation_ExactCount(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := "user-history-truncation"

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Generate N messages (1-300) with a mix of user and assistant messages.
		n := rapid.IntRange(1, 300).Draw(t, "num_messages")

		for i := 0; i < n; i++ {
			if i%2 == 0 {
				// User message.
				content := fmt.Sprintf("user-msg-%d", i+1)
				_, err := svc.SendMessage(ctx, userID, session.ID, content)
				if err != nil {
					t.Fatalf("SendMessage failed at message %d: %v", i+1, err)
				}
			} else {
				// Assistant message.
				content := fmt.Sprintf("assistant-msg-%d", i+1)
				_, err := svc.PersistAssistantMessage(ctx, session.ID, generateID(), content, 100)
				if err != nil {
					t.Fatalf("PersistAssistantMessage failed at message %d: %v", i+1, err)
				}
			}
		}

		// Get the last message to use as the trigger message for BuildTaskPayload.
		msgs, err := svc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}
		lastMsg := &ChatMessage{
			ID:            msgs[len(msgs)-1].ID,
			ChatSessionID: msgs[len(msgs)-1].ChatSessionID,
			Seq:           msgs[len(msgs)-1].Seq,
			Role:          msgs[len(msgs)-1].Role,
			Content:       msgs[len(msgs)-1].Content,
			Status:        msgs[len(msgs)-1].Status,
			CreatedAt:     msgs[len(msgs)-1].CreatedAt,
		}

		// Build the task payload.
		payload, err := svc.BuildTaskPayload(ctx, session.ID, lastMsg)
		if err != nil {
			t.Fatalf("BuildTaskPayload failed: %v", err)
		}

		// Verify history length is exactly min(N, 200).
		expectedLen := n
		if expectedLen > MaxHistoryMessages {
			expectedLen = MaxHistoryMessages
		}

		if len(payload.History) != expectedLen {
			t.Fatalf("expected history length %d (min(%d, %d)), got %d",
				expectedLen, n, MaxHistoryMessages, len(payload.History))
		}
	})
}

// TestProperty_HistoryTruncation_MostRecentMessages verifies that for any session
// with N messages (where N may exceed 200), the history returned by BuildTaskPayload
// contains the most recent messages in chronological order.
func TestProperty_HistoryTruncation_MostRecentMessages(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := "user-history-recent"

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Generate N messages (1-300) with a mix of user and assistant messages.
		n := rapid.IntRange(1, 300).Draw(t, "num_messages")

		// Track all messages in insertion order.
		type msgRecord struct {
			role    string
			content string
		}
		allMessages := make([]msgRecord, 0, n)

		for i := 0; i < n; i++ {
			if i%2 == 0 {
				// User message.
				content := fmt.Sprintf("user-msg-%d", i+1)
				_, err := svc.SendMessage(ctx, userID, session.ID, content)
				if err != nil {
					t.Fatalf("SendMessage failed at message %d: %v", i+1, err)
				}
				allMessages = append(allMessages, msgRecord{role: "user", content: content})
			} else {
				// Assistant message.
				content := fmt.Sprintf("assistant-msg-%d", i+1)
				_, err := svc.PersistAssistantMessage(ctx, session.ID, generateID(), content, 100)
				if err != nil {
					t.Fatalf("PersistAssistantMessage failed at message %d: %v", i+1, err)
				}
				allMessages = append(allMessages, msgRecord{role: "assistant", content: content})
			}
		}

		// Get the last message to use as the trigger message for BuildTaskPayload.
		msgs, err := svc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}
		lastMsg := &ChatMessage{
			ID:            msgs[len(msgs)-1].ID,
			ChatSessionID: msgs[len(msgs)-1].ChatSessionID,
			Seq:           msgs[len(msgs)-1].Seq,
			Role:          msgs[len(msgs)-1].Role,
			Content:       msgs[len(msgs)-1].Content,
			Status:        msgs[len(msgs)-1].Status,
			CreatedAt:     msgs[len(msgs)-1].CreatedAt,
		}

		// Build the task payload.
		payload, err := svc.BuildTaskPayload(ctx, session.ID, lastMsg)
		if err != nil {
			t.Fatalf("BuildTaskPayload failed: %v", err)
		}

		// Determine the expected window: the last min(N, 200) messages.
		windowStart := 0
		if n > MaxHistoryMessages {
			windowStart = n - MaxHistoryMessages
		}
		expectedWindow := allMessages[windowStart:]

		// Verify the history matches the expected window in order.
		if len(payload.History) != len(expectedWindow) {
			t.Fatalf("history length mismatch: expected %d, got %d",
				len(expectedWindow), len(payload.History))
		}

		for i, item := range payload.History {
			expected := expectedWindow[i]
			if item.Role != expected.role {
				t.Fatalf("history[%d] role mismatch: expected %q, got %q",
					i, expected.role, item.Role)
			}
			if item.Content != expected.content {
				t.Fatalf("history[%d] content mismatch: expected %q, got %q",
					i, expected.content, item.Content)
			}
		}
	})
}

// TestProperty_HistoryTruncation_ChronologicalOrder verifies that the history
// returned by BuildTaskPayload is in chronological order (oldest first within
// the window), regardless of the total number of messages.
func TestProperty_HistoryTruncation_ChronologicalOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := "user-history-order"

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Generate N messages (1-300).
		n := rapid.IntRange(1, 300).Draw(t, "num_messages")

		for i := 0; i < n; i++ {
			if i%2 == 0 {
				content := fmt.Sprintf("user-msg-%d", i+1)
				_, err := svc.SendMessage(ctx, userID, session.ID, content)
				if err != nil {
					t.Fatalf("SendMessage failed at message %d: %v", i+1, err)
				}
			} else {
				content := fmt.Sprintf("assistant-msg-%d", i+1)
				_, err := svc.PersistAssistantMessage(ctx, session.ID, generateID(), content, 100)
				if err != nil {
					t.Fatalf("PersistAssistantMessage failed at message %d: %v", i+1, err)
				}
			}
		}

		// Get the last message.
		msgs, err := svc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}
		lastMsg := &ChatMessage{
			ID:            msgs[len(msgs)-1].ID,
			ChatSessionID: msgs[len(msgs)-1].ChatSessionID,
			Seq:           msgs[len(msgs)-1].Seq,
			Role:          msgs[len(msgs)-1].Role,
			Content:       msgs[len(msgs)-1].Content,
			Status:        msgs[len(msgs)-1].Status,
			CreatedAt:     msgs[len(msgs)-1].CreatedAt,
		}

		// Build the task payload.
		payload, err := svc.BuildTaskPayload(ctx, session.ID, lastMsg)
		if err != nil {
			t.Fatalf("BuildTaskPayload failed: %v", err)
		}

		// Verify the history is in chronological order by checking that
		// message numbers are strictly increasing (since content encodes the
		// insertion order as "user-msg-X" or "assistant-msg-X").
		// We verify this by checking that each history item's content suffix
		// (the number) is less than the next one.
		history := payload.History
		for i := 1; i < len(history); i++ {
			// Extract the numeric suffix from content.
			prevNum := extractMsgNumber(history[i-1].Content)
			currNum := extractMsgNumber(history[i].Content)
			if currNum <= prevNum {
				t.Fatalf("history not in chronological order: history[%d] has msg number %d, history[%d] has msg number %d",
					i-1, prevNum, i, currNum)
			}
		}
	})
}

// extractMsgNumber extracts the numeric suffix from a message content string
// like "user-msg-42" or "assistant-msg-42".
func extractMsgNumber(content string) int {
	// Content format is "user-msg-N" or "assistant-msg-N".
	parts := strings.Split(content, "-")
	if len(parts) < 3 {
		return 0
	}
	numStr := parts[len(parts)-1]
	var num int
	fmt.Sscanf(numStr, "%d", &num)
	return num
}

// TestProperty_HistoryTruncation_HistoryItemsMatchProtocol verifies that each
// history item in the payload conforms to the protocol.HistoryItem structure
// with valid role and non-empty content.
func TestProperty_HistoryTruncation_HistoryItemsMatchProtocol(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := "user-history-protocol"

		// Create a session.
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Generate N messages (1-300).
		n := rapid.IntRange(1, 300).Draw(t, "num_messages")

		for i := 0; i < n; i++ {
			if i%2 == 0 {
				content := fmt.Sprintf("user-msg-%d", i+1)
				_, err := svc.SendMessage(ctx, userID, session.ID, content)
				if err != nil {
					t.Fatalf("SendMessage failed at message %d: %v", i+1, err)
				}
			} else {
				content := fmt.Sprintf("assistant-msg-%d", i+1)
				_, err := svc.PersistAssistantMessage(ctx, session.ID, generateID(), content, 100)
				if err != nil {
					t.Fatalf("PersistAssistantMessage failed at message %d: %v", i+1, err)
				}
			}
		}

		// Get the last message.
		msgs, err := svc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}
		lastMsg := &ChatMessage{
			ID:            msgs[len(msgs)-1].ID,
			ChatSessionID: msgs[len(msgs)-1].ChatSessionID,
			Seq:           msgs[len(msgs)-1].Seq,
			Role:          msgs[len(msgs)-1].Role,
			Content:       msgs[len(msgs)-1].Content,
			Status:        msgs[len(msgs)-1].Status,
			CreatedAt:     msgs[len(msgs)-1].CreatedAt,
		}

		// Build the task payload.
		payload, err := svc.BuildTaskPayload(ctx, session.ID, lastMsg)
		if err != nil {
			t.Fatalf("BuildTaskPayload failed: %v", err)
		}

		// Verify each history item has a valid role and non-empty content.
		validRoles := map[string]bool{"user": true, "assistant": true}
		for i, item := range payload.History {
			if !validRoles[item.Role] {
				t.Fatalf("history[%d] has invalid role %q (expected 'user' or 'assistant')", i, item.Role)
			}
			if item.Content == "" {
				t.Fatalf("history[%d] has empty content", i)
			}
		}

		// Verify the payload type matches protocol.HistoryItem structure.
		// (This is a compile-time check via the type assertion below.)
		var _ []protocol.HistoryItem = payload.History
	})
}
