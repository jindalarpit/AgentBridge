package service

import (
	"testing"

	"github.com/user/agentbridge/server/pkg/protocol"
)

func TestMessageQueue_FirstMessage_NotQueued(t *testing.T) {
	q := NewMessageQueue()

	msg := &QueuedMessage{
		UserID:    "user-1",
		SessionID: "session-1",
		Message:   &ChatMessage{ID: "msg-1"},
		Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
		DaemonID:  "daemon-1",
	}

	queued := q.TryEnqueue(msg)
	if queued {
		t.Error("first message should not be queued (no response in progress)")
	}

	// Session should now be marked as in-progress.
	if !q.IsInProgress("session-1") {
		t.Error("session should be in-progress after first message")
	}
}

func TestMessageQueue_SecondMessage_Queued(t *testing.T) {
	q := NewMessageQueue()

	msg1 := &QueuedMessage{
		UserID:    "user-1",
		SessionID: "session-1",
		Message:   &ChatMessage{ID: "msg-1"},
		Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
		DaemonID:  "daemon-1",
	}
	msg2 := &QueuedMessage{
		UserID:    "user-1",
		SessionID: "session-1",
		Message:   &ChatMessage{ID: "msg-2"},
		Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
		DaemonID:  "daemon-1",
	}

	// First message goes through.
	q.TryEnqueue(msg1)

	// Second message should be queued.
	queued := q.TryEnqueue(msg2)
	if !queued {
		t.Error("second message should be queued (response in progress)")
	}

	if q.QueueLength("session-1") != 1 {
		t.Errorf("queue length = %d, want 1", q.QueueLength("session-1"))
	}
}

func TestMessageQueue_Dequeue_FIFO(t *testing.T) {
	q := NewMessageQueue()

	// First message starts processing.
	msg1 := &QueuedMessage{
		UserID:    "user-1",
		SessionID: "session-1",
		Message:   &ChatMessage{ID: "msg-1"},
		Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
		DaemonID:  "daemon-1",
	}
	q.TryEnqueue(msg1)

	// Queue 3 more messages.
	for i := 2; i <= 4; i++ {
		msg := &QueuedMessage{
			UserID:    "user-1",
			SessionID: "session-1",
			Message:   &ChatMessage{ID: "msg-" + string(rune('0'+i))},
			Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
			DaemonID:  "daemon-1",
		}
		q.TryEnqueue(msg)
	}

	if q.QueueLength("session-1") != 3 {
		t.Fatalf("queue length = %d, want 3", q.QueueLength("session-1"))
	}

	// Dequeue should return messages in FIFO order.
	next := q.Dequeue("session-1")
	if next == nil {
		t.Fatal("expected a queued message")
	}
	if next.Message.ID != "msg-2" {
		t.Errorf("first dequeue got %q, want %q", next.Message.ID, "msg-2")
	}

	// Session should still be in-progress.
	if !q.IsInProgress("session-1") {
		t.Error("session should remain in-progress after dequeue with pending messages")
	}
}

func TestMessageQueue_Dequeue_EmptyQueue_MarksIdle(t *testing.T) {
	q := NewMessageQueue()

	// First message starts processing.
	msg1 := &QueuedMessage{
		UserID:    "user-1",
		SessionID: "session-1",
		Message:   &ChatMessage{ID: "msg-1"},
		Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
		DaemonID:  "daemon-1",
	}
	q.TryEnqueue(msg1)

	// Dequeue with no pending messages.
	next := q.Dequeue("session-1")
	if next != nil {
		t.Error("expected nil when no messages are queued")
	}

	// Session should be idle.
	if q.IsInProgress("session-1") {
		t.Error("session should be idle after dequeue with empty queue")
	}
}

func TestMessageQueue_IndependentSessions(t *testing.T) {
	q := NewMessageQueue()

	// Start processing for session-1.
	msg1 := &QueuedMessage{
		UserID:    "user-1",
		SessionID: "session-1",
		Message:   &ChatMessage{ID: "msg-1"},
		Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
		DaemonID:  "daemon-1",
	}
	q.TryEnqueue(msg1)

	// Session-2 should not be affected.
	msg2 := &QueuedMessage{
		UserID:    "user-1",
		SessionID: "session-2",
		Message:   &ChatMessage{ID: "msg-2"},
		Task:      &protocol.ChatTaskPayload{SessionID: "session-2"},
		DaemonID:  "daemon-1",
	}
	queued := q.TryEnqueue(msg2)
	if queued {
		t.Error("session-2 should not be affected by session-1's in-progress state")
	}
}

func TestMessageQueue_Clear(t *testing.T) {
	q := NewMessageQueue()

	// Start processing and queue messages.
	msg1 := &QueuedMessage{
		UserID:    "user-1",
		SessionID: "session-1",
		Message:   &ChatMessage{ID: "msg-1"},
		Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
		DaemonID:  "daemon-1",
	}
	q.TryEnqueue(msg1)

	msg2 := &QueuedMessage{
		UserID:    "user-1",
		SessionID: "session-1",
		Message:   &ChatMessage{ID: "msg-2"},
		Task:      &protocol.ChatTaskPayload{SessionID: "session-1"},
		DaemonID:  "daemon-1",
	}
	q.TryEnqueue(msg2)

	// Clear the session.
	q.Clear("session-1")

	if q.IsInProgress("session-1") {
		t.Error("session should not be in-progress after clear")
	}
	if q.QueueLength("session-1") != 0 {
		t.Error("queue should be empty after clear")
	}
}

func TestMessageQueue_FullDrainCycle(t *testing.T) {
	q := NewMessageQueue()

	// Simulate: send 3 messages while first is processing.
	messages := make([]*QueuedMessage, 3)
	for i := 0; i < 3; i++ {
		messages[i] = &QueuedMessage{
			UserID:    "user-1",
			SessionID: "session-1",
			Message:   &ChatMessage{ID: "msg-" + string(rune('A'+i))},
			Task:      &protocol.ChatTaskPayload{SessionID: "session-1", Content: "content-" + string(rune('A'+i))},
			DaemonID:  "daemon-1",
		}
	}

	// First message goes through immediately.
	if q.TryEnqueue(messages[0]) {
		t.Error("first message should not be queued")
	}

	// Second and third are queued.
	if !q.TryEnqueue(messages[1]) {
		t.Error("second message should be queued")
	}
	if !q.TryEnqueue(messages[2]) {
		t.Error("third message should be queued")
	}

	// First response completes — dequeue next.
	next := q.Dequeue("session-1")
	if next == nil || next.Message.ID != "msg-B" {
		t.Errorf("expected msg-B, got %v", next)
	}

	// Second response completes — dequeue next.
	next = q.Dequeue("session-1")
	if next == nil || next.Message.ID != "msg-C" {
		t.Errorf("expected msg-C, got %v", next)
	}

	// Third response completes — queue is empty.
	next = q.Dequeue("session-1")
	if next != nil {
		t.Error("expected nil when queue is drained")
	}

	// Session should be idle.
	if q.IsInProgress("session-1") {
		t.Error("session should be idle after full drain")
	}
}
