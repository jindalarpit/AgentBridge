package service

import (
	"sync"

	"github.com/user/agentbridge/server/pkg/protocol"
)

// QueuedMessage represents a message waiting to be delivered to the daemon.
type QueuedMessage struct {
	UserID    string
	SessionID string
	Message   *ChatMessage
	Task      *protocol.ChatTaskPayload
	RuntimeID string
	DaemonID  string
}

// MessageQueue manages per-session message queuing for concurrent sends.
// When a response is in progress for a session, subsequent messages are queued
// and delivered in FIFO order after the current response completes or fails.
type MessageQueue struct {
	mu sync.Mutex
	// inProgress tracks whether a session currently has a response in progress.
	inProgress map[string]bool
	// queues holds pending messages per session in FIFO order.
	queues map[string][]*QueuedMessage
}

// NewMessageQueue creates a new MessageQueue.
func NewMessageQueue() *MessageQueue {
	return &MessageQueue{
		inProgress: make(map[string]bool),
		queues:     make(map[string][]*QueuedMessage),
	}
}

// TryEnqueue checks if a session has a response in progress.
// If yes, it queues the message and returns true (message was queued).
// If no, it marks the session as in-progress and returns false (caller should send immediately).
func (q *MessageQueue) TryEnqueue(msg *QueuedMessage) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.inProgress[msg.SessionID] {
		// Response in progress — queue the message.
		q.queues[msg.SessionID] = append(q.queues[msg.SessionID], msg)
		return true
	}

	// No response in progress — mark as in-progress and let caller send.
	q.inProgress[msg.SessionID] = true
	return false
}

// Dequeue marks the current response as complete for a session and returns
// the next queued message, if any. If a message is returned, the session
// remains marked as in-progress (the caller is expected to send it).
// If no messages are queued, the session is marked as idle.
func (q *MessageQueue) Dequeue(sessionID string) *QueuedMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	queue := q.queues[sessionID]
	if len(queue) == 0 {
		// No pending messages — mark session as idle.
		delete(q.inProgress, sessionID)
		delete(q.queues, sessionID)
		return nil
	}

	// Pop the first message (FIFO).
	next := queue[0]
	q.queues[sessionID] = queue[1:]

	// Session remains in-progress since we're delivering the next message.
	return next
}

// IsInProgress returns whether a session currently has a response in progress.
func (q *MessageQueue) IsInProgress(sessionID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.inProgress[sessionID]
}

// QueueLength returns the number of queued messages for a session.
func (q *MessageQueue) QueueLength(sessionID string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.queues[sessionID])
}

// Clear removes all queued messages for a session and marks it as idle.
// This is useful when a session is deleted.
func (q *MessageQueue) Clear(sessionID string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	delete(q.inProgress, sessionID)
	delete(q.queues, sessionID)
}
