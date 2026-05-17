package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/user/agentbridge/server/pkg/protocol"
	"pgregory.net/rapid"
)

// **Validates: Requirements 5.2, 5.6, 5.7**
// Property 11: Binding Replacement and Offline Rejection
// For any chat session, after N sequential bind operations to valid online runtimes,
// the session SHALL have exactly one binding pointing to the most recently selected
// runtime, and all previously sent messages SHALL be preserved. For any runtime with
// status "offline", attempting to bind SHALL be rejected with an error.

// TestProperty_BindingReplacement_LastRuntimeBound verifies that after N sequential
// bind operations to valid online runtimes, the session has exactly one binding
// pointing to the most recently selected runtime.
func TestProperty_BindingReplacement_LastRuntimeBound(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		chatSvc := NewInMemoryChatService()
		runtimeSvc := NewInMemoryRuntimeService()
		ctx := context.Background()

		userID := "user-binding-test"
		daemonID := "daemon-binding-test"

		// Generate 2-10 available runtimes for the user's daemon.
		numRuntimes := rapid.IntRange(2, 10).Draw(t, "num_runtimes")
		runtimes := make([]protocol.RuntimeInfo, numRuntimes)
		for i := range numRuntimes {
			runtimes[i] = protocol.RuntimeInfo{
				AgentType:  fmt.Sprintf("agent-%d", i),
				BinaryPath: fmt.Sprintf("/usr/bin/agent-%d", i),
				Version:    fmt.Sprintf("%d.0.0", i+1),
				Status:     "available",
			}
		}

		// Register daemon with all runtimes.
		err := runtimeSvc.RegisterDaemon(ctx, DaemonRegistration{
			DaemonID: daemonID,
			UserID:   userID,
			Runtimes: runtimes,
		})
		if err != nil {
			t.Fatalf("RegisterDaemon failed: %v", err)
		}

		// Get the actual runtime IDs from the service.
		availableRuntimes, err := runtimeSvc.GetUserRuntimes(ctx, userID)
		if err != nil {
			t.Fatalf("GetUserRuntimes failed: %v", err)
		}
		if len(availableRuntimes) != numRuntimes {
			t.Fatalf("expected %d available runtimes, got %d", numRuntimes, len(availableRuntimes))
		}

		// Create a chat session.
		session, err := chatSvc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Perform N sequential bind operations (2-10 binds).
		numBinds := rapid.IntRange(2, 10).Draw(t, "num_binds")
		var lastRuntimeID string
		for i := 0; i < numBinds; i++ {
			// Pick a random runtime to bind.
			idx := rapid.IntRange(0, len(availableRuntimes)-1).Draw(t, fmt.Sprintf("bind_idx_%d", i))
			runtimeID := availableRuntimes[idx].ID
			lastRuntimeID = runtimeID

			err := chatSvc.BindSessionRuntime(ctx, userID, session.ID, runtimeID, runtimeSvc)
			if err != nil {
				t.Fatalf("BindSessionRuntime failed on bind %d: %v", i, err)
			}
		}

		// Verify the session has exactly the last runtime bound.
		updatedSession, err := chatSvc.GetSession(ctx, userID, session.ID)
		if err != nil {
			t.Fatalf("GetSession failed: %v", err)
		}

		if updatedSession.RuntimeID == nil {
			t.Fatal("session RuntimeID is nil after binding")
		}
		if *updatedSession.RuntimeID != lastRuntimeID {
			t.Fatalf("session RuntimeID = %q, want %q (last bound runtime)",
				*updatedSession.RuntimeID, lastRuntimeID)
		}
	})
}

// TestProperty_BindingReplacement_MessagesPreserved verifies that all previously
// sent messages are preserved after rebinding to a different runtime.
func TestProperty_BindingReplacement_MessagesPreserved(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		chatSvc := NewInMemoryChatService()
		runtimeSvc := NewInMemoryRuntimeService()
		ctx := context.Background()

		userID := "user-msg-preserve"
		daemonID := "daemon-msg-preserve"

		// Register daemon with multiple available runtimes.
		numRuntimes := rapid.IntRange(2, 5).Draw(t, "num_runtimes")
		runtimes := make([]protocol.RuntimeInfo, numRuntimes)
		for i := range numRuntimes {
			runtimes[i] = protocol.RuntimeInfo{
				AgentType:  fmt.Sprintf("agent-%d", i),
				BinaryPath: fmt.Sprintf("/usr/bin/agent-%d", i),
				Version:    "1.0.0",
				Status:     "available",
			}
		}

		err := runtimeSvc.RegisterDaemon(ctx, DaemonRegistration{
			DaemonID: daemonID,
			UserID:   userID,
			Runtimes: runtimes,
		})
		if err != nil {
			t.Fatalf("RegisterDaemon failed: %v", err)
		}

		availableRuntimes, err := runtimeSvc.GetUserRuntimes(ctx, userID)
		if err != nil {
			t.Fatalf("GetUserRuntimes failed: %v", err)
		}

		// Create a chat session.
		session, err := chatSvc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Bind to the first runtime.
		err = chatSvc.BindSessionRuntime(ctx, userID, session.ID, availableRuntimes[0].ID, runtimeSvc)
		if err != nil {
			t.Fatalf("initial BindSessionRuntime failed: %v", err)
		}

		// Send N messages (1-20) with deterministic content to avoid trimming issues.
		numMessages := rapid.IntRange(1, 20).Draw(t, "num_messages")
		sentContents := make([]string, numMessages)
		for i := 0; i < numMessages; i++ {
			// Use alphanumeric content to avoid whitespace-only strings that get trimmed differently.
			suffix := rapid.StringMatching(`[a-z0-9]{1,20}`).Draw(t, fmt.Sprintf("content_%d", i))
			content := fmt.Sprintf("message-%d-%s", i, suffix)
			sentContents[i] = content

			_, err := chatSvc.SendMessage(ctx, userID, session.ID, content)
			if err != nil {
				t.Fatalf("SendMessage %d failed: %v", i, err)
			}
		}

		// Perform N sequential rebinds (1-5).
		numRebinds := rapid.IntRange(1, 5).Draw(t, "num_rebinds")
		for i := 0; i < numRebinds; i++ {
			idx := rapid.IntRange(0, len(availableRuntimes)-1).Draw(t, fmt.Sprintf("rebind_idx_%d", i))
			err := chatSvc.BindSessionRuntime(ctx, userID, session.ID, availableRuntimes[idx].ID, runtimeSvc)
			if err != nil {
				t.Fatalf("BindSessionRuntime rebind %d failed: %v", i, err)
			}
		}

		// Verify all messages are still present and in correct order.
		messages, err := chatSvc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}

		if len(messages) != numMessages {
			t.Fatalf("expected %d messages after rebinding, got %d", numMessages, len(messages))
		}

		for i, msg := range messages {
			if msg.Seq != i+1 {
				t.Errorf("message %d has seq %d, want %d", i, msg.Seq, i+1)
			}
			if msg.Content != sentContents[i] {
				t.Errorf("message %d content mismatch: got %q, want %q",
					i, msg.Content, sentContents[i])
			}
			if msg.ChatSessionID != session.ID {
				t.Errorf("message %d session ID = %q, want %q",
					i, msg.ChatSessionID, session.ID)
			}
		}
	})
}

// TestProperty_BindingReplacement_OfflineRuntimeRejected verifies that binding
// to a runtime with status "offline" is always rejected with an error.
func TestProperty_BindingReplacement_OfflineRuntimeRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		chatSvc := NewInMemoryChatService()
		runtimeSvc := NewInMemoryRuntimeService()
		ctx := context.Background()

		userID := "user-offline-reject"
		daemonID := "daemon-offline-reject"

		// Register daemon with available runtimes.
		numRuntimes := rapid.IntRange(1, 5).Draw(t, "num_runtimes")
		runtimes := make([]protocol.RuntimeInfo, numRuntimes)
		for i := range numRuntimes {
			runtimes[i] = protocol.RuntimeInfo{
				AgentType:  fmt.Sprintf("agent-%d", i),
				BinaryPath: fmt.Sprintf("/usr/bin/agent-%d", i),
				Version:    "1.0.0",
				Status:     "available",
			}
		}

		err := runtimeSvc.RegisterDaemon(ctx, DaemonRegistration{
			DaemonID: daemonID,
			UserID:   userID,
			Runtimes: runtimes,
		})
		if err != nil {
			t.Fatalf("RegisterDaemon failed: %v", err)
		}

		// Get runtime IDs before marking offline.
		availableRuntimes, err := runtimeSvc.GetUserRuntimes(ctx, userID)
		if err != nil {
			t.Fatalf("GetUserRuntimes failed: %v", err)
		}
		if len(availableRuntimes) == 0 {
			t.Skip("no available runtimes generated")
		}

		// Create a chat session.
		session, err := chatSvc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		// Mark the daemon (and all its runtimes) as offline.
		err = runtimeSvc.MarkOffline(ctx, daemonID)
		if err != nil {
			t.Fatalf("MarkOffline failed: %v", err)
		}

		// Attempt to bind to each runtime — all should fail.
		for _, rt := range availableRuntimes {
			err := chatSvc.BindSessionRuntime(ctx, userID, session.ID, rt.ID, runtimeSvc)
			if err == nil {
				t.Fatalf("BindSessionRuntime to offline runtime %s should have failed but succeeded", rt.ID)
			}
			if err != ErrRuntimeOffline {
				t.Fatalf("BindSessionRuntime to offline runtime %s returned error %v, want ErrRuntimeOffline",
					rt.ID, err)
			}
		}

		// Verify session still has no binding.
		updatedSession, err := chatSvc.GetSession(ctx, userID, session.ID)
		if err != nil {
			t.Fatalf("GetSession failed: %v", err)
		}
		if updatedSession.RuntimeID != nil {
			t.Fatalf("session RuntimeID should be nil after failed offline binds, got %q",
				*updatedSession.RuntimeID)
		}
	})
}

// TestProperty_BindingReplacement_OfflineAfterOnlineBind verifies that if a runtime
// goes offline after a successful bind, attempting to rebind to it fails, but the
// previous binding and messages remain intact.
func TestProperty_BindingReplacement_OfflineAfterOnlineBind(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		chatSvc := NewInMemoryChatService()
		runtimeSvc := NewInMemoryRuntimeService()
		ctx := context.Background()

		userID := "user-offline-after"
		daemonID := "daemon-offline-after"

		// Register daemon with at least 2 runtimes.
		numRuntimes := rapid.IntRange(2, 5).Draw(t, "num_runtimes")
		runtimes := make([]protocol.RuntimeInfo, numRuntimes)
		for i := range numRuntimes {
			runtimes[i] = protocol.RuntimeInfo{
				AgentType:  fmt.Sprintf("agent-%d", i),
				BinaryPath: fmt.Sprintf("/usr/bin/agent-%d", i),
				Version:    "1.0.0",
				Status:     "available",
			}
		}

		err := runtimeSvc.RegisterDaemon(ctx, DaemonRegistration{
			DaemonID: daemonID,
			UserID:   userID,
			Runtimes: runtimes,
		})
		if err != nil {
			t.Fatalf("RegisterDaemon failed: %v", err)
		}

		availableRuntimes, err := runtimeSvc.GetUserRuntimes(ctx, userID)
		if err != nil {
			t.Fatalf("GetUserRuntimes failed: %v", err)
		}

		// Create session and bind to first runtime.
		session, err := chatSvc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession failed: %v", err)
		}

		firstRuntimeID := availableRuntimes[0].ID
		err = chatSvc.BindSessionRuntime(ctx, userID, session.ID, firstRuntimeID, runtimeSvc)
		if err != nil {
			t.Fatalf("initial BindSessionRuntime failed: %v", err)
		}

		// Send some messages.
		numMessages := rapid.IntRange(1, 10).Draw(t, "num_messages")
		for i := 0; i < numMessages; i++ {
			_, err := chatSvc.SendMessage(ctx, userID, session.ID, fmt.Sprintf("msg-%d", i))
			if err != nil {
				t.Fatalf("SendMessage %d failed: %v", i, err)
			}
		}

		// Mark daemon offline (simulates daemon going down).
		err = runtimeSvc.MarkOffline(ctx, daemonID)
		if err != nil {
			t.Fatalf("MarkOffline failed: %v", err)
		}

		// Attempt to rebind to the same runtime — should fail.
		err = chatSvc.BindSessionRuntime(ctx, userID, session.ID, firstRuntimeID, runtimeSvc)
		if err == nil {
			t.Fatal("BindSessionRuntime to offline runtime should have failed")
		}
		if err != ErrRuntimeOffline {
			t.Fatalf("expected ErrRuntimeOffline, got: %v", err)
		}

		// Verify previous binding is still intact.
		updatedSession, err := chatSvc.GetSession(ctx, userID, session.ID)
		if err != nil {
			t.Fatalf("GetSession failed: %v", err)
		}
		if updatedSession.RuntimeID == nil || *updatedSession.RuntimeID != firstRuntimeID {
			t.Fatalf("session RuntimeID should still be %q after failed rebind", firstRuntimeID)
		}

		// Verify all messages are preserved.
		messages, err := chatSvc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages failed: %v", err)
		}
		if len(messages) != numMessages {
			t.Fatalf("expected %d messages preserved, got %d", numMessages, len(messages))
		}
	})
}
