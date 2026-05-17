package clientws

import (
	"sync"
	"time"
)

const (
	// maxBufferedMessages is the maximum number of messages buffered per user
	// during disconnection (requirement 10.5).
	maxBufferedMessages = 100

	// bufferTTL is the maximum age of buffered messages. Messages older than
	// this are discarded and not delivered on reconnection (requirement 10.5).
	bufferTTL = 5 * time.Minute

	// bufferCleanupInterval is how often the background goroutine sweeps
	// expired buffer entries.
	bufferCleanupInterval = 1 * time.Minute
)

// bufferedMessage holds a single serialized message with its insertion timestamp.
type bufferedMessage struct {
	data      []byte
	createdAt time.Time
}

// MessageBuffer stores serialized messages for disconnected clients and delivers
// them in chronological order upon reconnection. Each user can have at most
// maxBufferedMessages entries, and entries older than bufferTTL are discarded.
type MessageBuffer struct {
	mu      sync.Mutex
	entries map[string][]bufferedMessage // keyed by userID

	nowFunc func() time.Time // injectable clock for testing
	stopCh  chan struct{}
}

// NewMessageBuffer creates a new MessageBuffer and starts a background
// cleanup goroutine that removes expired entries periodically.
func NewMessageBuffer() *MessageBuffer {
	mb := &MessageBuffer{
		entries: make(map[string][]bufferedMessage),
		nowFunc: time.Now,
		stopCh:  make(chan struct{}),
	}
	go mb.cleanupLoop()
	return mb
}

// NewMessageBufferWithClock creates a MessageBuffer with a custom clock function.
// This is primarily useful for testing time-dependent behavior.
// No automatic cleanup loop is started — tests call Cleanup manually.
func NewMessageBufferWithClock(nowFunc func() time.Time) *MessageBuffer {
	return &MessageBuffer{
		entries: make(map[string][]bufferedMessage),
		nowFunc: nowFunc,
		stopCh:  make(chan struct{}),
	}
}

// Stop terminates the background cleanup goroutine.
func (mb *MessageBuffer) Stop() {
	close(mb.stopCh)
}

// Add buffers a serialized message for the given user. If the buffer already
// contains maxBufferedMessages entries, the oldest message is evicted to make room.
// Messages are stored with the current timestamp for TTL enforcement.
func (mb *MessageBuffer) Add(userID string, data []byte) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	now := mb.nowFunc()

	buf := mb.entries[userID]

	// Evict the oldest message if at capacity.
	if len(buf) >= maxBufferedMessages {
		buf = buf[1:]
	}

	buf = append(buf, bufferedMessage{
		data:      data,
		createdAt: now,
	})
	mb.entries[userID] = buf
}

// Drain retrieves and removes all non-expired buffered messages for the given
// user, returning them in chronological order (oldest first). Messages older
// than bufferTTL are discarded and not returned.
func (mb *MessageBuffer) Drain(userID string) [][]byte {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	buf, ok := mb.entries[userID]
	if !ok || len(buf) == 0 {
		return nil
	}

	// Remove the user's buffer entry.
	delete(mb.entries, userID)

	now := mb.nowFunc()
	cutoff := now.Add(-bufferTTL)

	// Filter out expired messages and collect valid ones in chronological order.
	var result [][]byte
	for _, entry := range buf {
		if entry.createdAt.After(cutoff) {
			result = append(result, entry.data)
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// Count returns the number of currently buffered messages for the given user
// (including potentially expired ones that haven't been cleaned up yet).
func (mb *MessageBuffer) Count(userID string) int {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	return len(mb.entries[userID])
}

// Cleanup removes all expired entries from the buffer. This is called
// periodically by the background goroutine and can also be called manually
// (e.g., in tests).
func (mb *MessageBuffer) Cleanup() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	now := mb.nowFunc()
	cutoff := now.Add(-bufferTTL)

	for userID, buf := range mb.entries {
		// Find the first non-expired entry.
		firstValid := -1
		for i, entry := range buf {
			if entry.createdAt.After(cutoff) {
				firstValid = i
				break
			}
		}

		if firstValid == -1 {
			// All entries expired — remove the user's buffer entirely.
			delete(mb.entries, userID)
		} else if firstValid > 0 {
			// Trim expired entries from the front.
			mb.entries[userID] = buf[firstValid:]
		}
	}
}

// cleanupLoop runs periodically to remove expired buffer entries.
func (mb *MessageBuffer) cleanupLoop() {
	ticker := time.NewTicker(bufferCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mb.Cleanup()
		case <-mb.stopCh:
			return
		}
	}
}
