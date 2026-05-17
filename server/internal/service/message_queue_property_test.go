package service

import (
	"fmt"
	"testing"

	"github.com/user/agentbridge/server/pkg/protocol"
	"pgregory.net/rapid"
)

// **Validates: Requirements 6.9**
//
// Property 15: Message Queue FIFO Ordering
// For any sequence of N messages sent by a user while a previous response is in progress,
// all N messages SHALL be queued and delivered to the daemon in the exact order they were
// received (FIFO), with delivery occurring only after the current response completes or fails.

func TestProperty15_MessageQueueFIFO_OrderPreserved(t *testing.T) {
	// For any sequence of N messages enqueued while a response is in progress,
	// dequeuing all messages returns them in the exact enqueue order.
	rapid.Check(t, func(t *rapid.T) {
		q := NewMessageQueue()

		// Generate a random number of queued messages (1 to 50).
		n := rapid.IntRange(1, 50).Draw(t, "numMessages")

		// First message starts processing (not queued).
		firstMsg := &QueuedMessage{
			UserID:    "user-1",
			SessionID: "session-1",
			Message:   &ChatMessage{ID: "msg-initial"},
			Task:      &protocol.ChatTaskPayload{SessionID: "session-1", Content: "initial"},
			DaemonID:  "daemon-1",
		}
		queued := q.TryEnqueue(firstMsg)
		if queued {
			t.Fatal("first message should not be queued")
		}

		// Enqueue N messages while response is in progress.
		expectedOrder := make([]string, n)
		for i := 0; i < n; i++ {
			msgID := fmt.Sprintf("msg-%d", i)
			expectedOrder[i] = msgID

			msg := &QueuedMessage{
				UserID:    "user-1",
				SessionID: "session-1",
				Message:   &ChatMessage{ID: msgID},
				Task:      &protocol.ChatTaskPayload{SessionID: "session-1", Content: fmt.Sprintf("content-%d", i)},
				DaemonID:  "daemon-1",
			}
			wasQueued := q.TryEnqueue(msg)
			if !wasQueued {
				t.Fatalf("message %d should be queued (response in progress)", i)
			}
		}

		// Dequeue all messages and verify FIFO order.
		for i := 0; i < n; i++ {
			next := q.Dequeue("session-1")
			if next == nil {
				t.Fatalf("expected message at position %d, got nil", i)
			}
			if next.Message.ID != expectedOrder[i] {
				t.Fatalf("FIFO violation at position %d: got %q, want %q", i, next.Message.ID, expectedOrder[i])
			}
		}

		// After draining, dequeue should return nil and session should be idle.
		final := q.Dequeue("session-1")
		if final != nil {
			t.Fatal("expected nil after draining all messages")
		}
		if q.IsInProgress("session-1") {
			t.Fatal("session should be idle after full drain")
		}
	})
}

func TestProperty15_MessageQueueFIFO_DeliveryOnlyAfterCompletion(t *testing.T) {
	// Messages are only delivered (dequeued) after the current response completes.
	// While in-progress, all new messages are queued and none are delivered.
	rapid.Check(t, func(t *rapid.T) {
		q := NewMessageQueue()

		n := rapid.IntRange(1, 30).Draw(t, "numMessages")

		// Start processing.
		firstMsg := &QueuedMessage{
			UserID:    "user-1",
			SessionID: "session-1",
			Message:   &ChatMessage{ID: "msg-first"},
			Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
			DaemonID:  "daemon-1",
		}
		q.TryEnqueue(firstMsg)

		// Enqueue N messages.
		for i := 0; i < n; i++ {
			msg := &QueuedMessage{
				UserID:    "user-1",
				SessionID: "session-1",
				Message:   &ChatMessage{ID: fmt.Sprintf("queued-%d", i)},
				Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
				DaemonID:  "daemon-1",
			}
			q.TryEnqueue(msg)
		}

		// Verify session is in-progress (no delivery yet).
		if !q.IsInProgress("session-1") {
			t.Fatal("session should be in-progress while messages are queued")
		}

		// Verify queue length matches N.
		if q.QueueLength("session-1") != n {
			t.Fatalf("queue length = %d, want %d", q.QueueLength("session-1"), n)
		}

		// Simulate response completion by dequeuing — this is when delivery happens.
		// Each dequeue represents "current response completed, deliver next".
		for i := 0; i < n; i++ {
			next := q.Dequeue("session-1")
			if next == nil {
				t.Fatalf("expected queued message at position %d", i)
			}
			expectedID := fmt.Sprintf("queued-%d", i)
			if next.Message.ID != expectedID {
				t.Fatalf("delivery order violation: got %q, want %q", next.Message.ID, expectedID)
			}
		}
	})
}

func TestProperty15_MessageQueueFIFO_SessionIsolation(t *testing.T) {
	// Queuing in one session does not affect another session's queue or ordering.
	rapid.Check(t, func(t *rapid.T) {
		q := NewMessageQueue()

		// Generate random message counts for two sessions.
		n1 := rapid.IntRange(1, 20).Draw(t, "session1Count")
		n2 := rapid.IntRange(1, 20).Draw(t, "session2Count")

		// Start processing for both sessions.
		for _, sid := range []string{"session-1", "session-2"} {
			msg := &QueuedMessage{
				UserID:    "user-1",
				SessionID: sid,
				Message:   &ChatMessage{ID: sid + "-initial"},
				Task:      &protocol.ChatTaskPayload{SessionID: sid},
				DaemonID:  "daemon-1",
			}
			q.TryEnqueue(msg)
		}

		// Enqueue messages for session-1.
		for i := 0; i < n1; i++ {
			msg := &QueuedMessage{
				UserID:    "user-1",
				SessionID: "session-1",
				Message:   &ChatMessage{ID: fmt.Sprintf("s1-msg-%d", i)},
				Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
				DaemonID:  "daemon-1",
			}
			q.TryEnqueue(msg)
		}

		// Enqueue messages for session-2.
		for i := 0; i < n2; i++ {
			msg := &QueuedMessage{
				UserID:    "user-1",
				SessionID: "session-2",
				Message:   &ChatMessage{ID: fmt.Sprintf("s2-msg-%d", i)},
				Task:      &protocol.ChatTaskPayload{SessionID: "session-2"},
				DaemonID:  "daemon-1",
			}
			q.TryEnqueue(msg)
		}

		// Verify each session's queue is independent.
		if q.QueueLength("session-1") != n1 {
			t.Fatalf("session-1 queue length = %d, want %d", q.QueueLength("session-1"), n1)
		}
		if q.QueueLength("session-2") != n2 {
			t.Fatalf("session-2 queue length = %d, want %d", q.QueueLength("session-2"), n2)
		}

		// Drain session-1 and verify FIFO.
		for i := 0; i < n1; i++ {
			next := q.Dequeue("session-1")
			if next == nil {
				t.Fatalf("session-1: expected message at position %d", i)
			}
			expectedID := fmt.Sprintf("s1-msg-%d", i)
			if next.Message.ID != expectedID {
				t.Fatalf("session-1 FIFO violation: got %q, want %q", next.Message.ID, expectedID)
			}
		}

		// Session-2 should still have all its messages.
		if q.QueueLength("session-2") != n2 {
			t.Fatalf("session-2 queue should be unaffected, got length %d", q.QueueLength("session-2"))
		}

		// Drain session-2 and verify FIFO.
		for i := 0; i < n2; i++ {
			next := q.Dequeue("session-2")
			if next == nil {
				t.Fatalf("session-2: expected message at position %d", i)
			}
			expectedID := fmt.Sprintf("s2-msg-%d", i)
			if next.Message.ID != expectedID {
				t.Fatalf("session-2 FIFO violation: got %q, want %q", next.Message.ID, expectedID)
			}
		}
	})
}

func TestProperty15_MessageQueueFIFO_RandomContentPreserved(t *testing.T) {
	// For any sequence of messages with random content, the content is preserved
	// exactly through the queue and delivered in FIFO order.
	rapid.Check(t, func(t *rapid.T) {
		q := NewMessageQueue()

		n := rapid.IntRange(1, 40).Draw(t, "numMessages")

		// Start processing.
		firstMsg := &QueuedMessage{
			UserID:    "user-1",
			SessionID: "session-1",
			Message:   &ChatMessage{ID: "initial"},
			Task:      &protocol.ChatTaskPayload{SessionID: "session-1", Content: "initial-content"},
			DaemonID:  "daemon-1",
		}
		q.TryEnqueue(firstMsg)

		// Generate and enqueue messages with random content.
		type msgData struct {
			id      string
			content string
		}
		expected := make([]msgData, n)
		for i := 0; i < n; i++ {
			content := rapid.String().Draw(t, "content")
			id := fmt.Sprintf("msg-%d", i)
			expected[i] = msgData{id: id, content: content}

			msg := &QueuedMessage{
				UserID:    "user-1",
				SessionID: "session-1",
				Message:   &ChatMessage{ID: id, Content: content},
				Task:      &protocol.ChatTaskPayload{SessionID: "session-1", Content: content},
				DaemonID:  "daemon-1",
			}
			q.TryEnqueue(msg)
		}

		// Dequeue and verify order and content integrity.
		for i := 0; i < n; i++ {
			next := q.Dequeue("session-1")
			if next == nil {
				t.Fatalf("expected message at position %d, got nil", i)
			}
			if next.Message.ID != expected[i].id {
				t.Fatalf("FIFO violation at %d: got ID %q, want %q", i, next.Message.ID, expected[i].id)
			}
			if next.Message.Content != expected[i].content {
				t.Fatalf("content corruption at %d: got %q, want %q", i, next.Message.Content, expected[i].content)
			}
			if next.Task.Content != expected[i].content {
				t.Fatalf("task content corruption at %d: got %q, want %q", i, next.Task.Content, expected[i].content)
			}
		}
	})
}
