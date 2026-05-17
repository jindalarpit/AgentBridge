package clientws

import (
	"sync"
	"time"
)

const (
	// malformedWindowDuration is the sliding window duration for tracking malformed messages.
	malformedWindowDuration = 60 * time.Second

	// malformedMaxCount is the maximum number of malformed messages allowed within the window.
	// The connection is closed upon receiving the (malformedMaxCount + 1)th malformed message.
	malformedMaxCount = 10
)

// malformedRateLimiter tracks malformed message timestamps within a sliding window
// for a single connection. It is NOT safe for concurrent use without external synchronization.
type malformedRateLimiter struct {
	mu         sync.Mutex
	timestamps []time.Time
	window     time.Duration
	maxCount   int
}

// newMalformedRateLimiter creates a rate limiter that allows up to maxCount malformed
// messages within the given window duration before triggering a close.
func newMalformedRateLimiter(window time.Duration, maxCount int) *malformedRateLimiter {
	return &malformedRateLimiter{
		timestamps: make([]time.Time, 0, maxCount+1),
		window:     window,
		maxCount:   maxCount,
	}
}

// Record records a malformed message at the given time and returns true if the
// connection should be closed (i.e., the count within the window exceeds maxCount).
func (r *malformedRateLimiter) Record(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Evict timestamps outside the sliding window.
	cutoff := now.Add(-r.window)
	r.evict(cutoff)

	// Add the new timestamp.
	r.timestamps = append(r.timestamps, now)

	// If we now have more than maxCount entries in the window, close the connection.
	return len(r.timestamps) > r.maxCount
}

// Count returns the number of malformed messages currently within the sliding window.
func (r *malformedRateLimiter) Count(now time.Time) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	cutoff := now.Add(-r.window)
	r.evict(cutoff)
	return len(r.timestamps)
}

// evict removes all timestamps that are at or before the cutoff time.
// Must be called with r.mu held.
func (r *malformedRateLimiter) evict(cutoff time.Time) {
	// Find the first index that is after the cutoff.
	i := 0
	for i < len(r.timestamps) && !r.timestamps[i].After(cutoff) {
		i++
	}
	if i > 0 {
		// Shift remaining timestamps to the front.
		copy(r.timestamps, r.timestamps[i:])
		r.timestamps = r.timestamps[:len(r.timestamps)-i]
	}
}
