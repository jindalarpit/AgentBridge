package service

import (
	"context"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// **Validates: Requirements 4.1**
// Property 6: Session Creation Defaults
// For any authenticated user, creating a new chat session SHALL produce a session with a unique ID
// (no collisions with existing sessions), title equal to "New Chat", status equal to "active",
// and a creation timestamp within 1 second of the current time.

// TestPropertySessionCreation_TitleIsNewChat verifies that for any userID,
// CreateSession produces a session with title "New Chat".
func TestPropertySessionCreation_TitleIsNewChat(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "user_id")

		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		if session.Title != "New Chat" {
			t.Fatalf("session.Title = %q, want %q", session.Title, "New Chat")
		}
	})
}

// TestPropertySessionCreation_StatusIsActive verifies that for any userID,
// CreateSession produces a session with status "active".
func TestPropertySessionCreation_StatusIsActive(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "user_id")

		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		if session.Status != "active" {
			t.Fatalf("session.Status = %q, want %q", session.Status, "active")
		}
	})
}

// TestPropertySessionCreation_CreatedAtWithinOneSecond verifies that for any userID,
// CreateSession produces a session with CreatedAt within 1 second of the current time.
func TestPropertySessionCreation_CreatedAtWithinOneSecond(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "user_id")

		before := time.Now()
		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}
		after := time.Now()

		// CreatedAt must be within 1 second of the call window.
		if session.CreatedAt.Before(before.Add(-1 * time.Second)) {
			t.Fatalf("session.CreatedAt %v is more than 1 second before call start %v", session.CreatedAt, before)
		}
		if session.CreatedAt.After(after.Add(1 * time.Second)) {
			t.Fatalf("session.CreatedAt %v is more than 1 second after call end %v", session.CreatedAt, after)
		}
	})
}

// TestPropertySessionCreation_UniqueIDs verifies that for any userID, creating N sessions
// produces N unique IDs (no collisions).
func TestPropertySessionCreation_UniqueIDs(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "user_id")
		n := rapid.IntRange(2, 50).Draw(t, "num_sessions")

		ids := make(map[string]bool, n)
		for i := 0; i < n; i++ {
			session, err := svc.CreateSession(ctx, userID)
			if err != nil {
				t.Fatalf("CreateSession #%d failed: %v", i, err)
			}

			if session.ID == "" {
				t.Fatalf("session #%d has empty ID", i)
			}

			if ids[session.ID] {
				t.Fatalf("session #%d has duplicate ID %q", i, session.ID)
			}
			ids[session.ID] = true
		}

		if len(ids) != n {
			t.Fatalf("created %d sessions but only %d unique IDs", n, len(ids))
		}
	})
}

// TestPropertySessionCreation_NilRuntimeID verifies that for any userID,
// CreateSession produces a session with nil RuntimeID.
func TestPropertySessionCreation_NilRuntimeID(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryChatService()
		ctx := context.Background()

		userID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "user_id")

		session, err := svc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		if session.RuntimeID != nil {
			t.Fatalf("session.RuntimeID = %v, want nil", session.RuntimeID)
		}
	})
}
