package clientws

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// **Validates: Requirements 10.7**
//
// Property 22: Malformed Message Rate Limiting
// For any WebSocket connection, the server SHALL tolerate up to 10 malformed messages
// within any 60-second sliding window without closing the connection. Upon receiving
// the 11th malformed message within 60 seconds, the server SHALL close that WebSocket_Connection.

func TestProperty22_MalformedRateLimit_ToleratesUpTo10WithinWindow(t *testing.T) {
	// For any sequence of malformed messages with timestamps within a 60-second window,
	// the connection is NOT closed if count ≤ 10.
	rapid.Check(t, func(t *rapid.T) {
		window := 60 * time.Second
		maxCount := 10
		rl := newMalformedRateLimiter(window, maxCount)

		// Generate a random count between 1 and 10 (inclusive).
		count := rapid.IntRange(1, 10).Draw(t, "malformedCount")

		// Generate a base time.
		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		// Generate timestamps all within the 60-second window from baseTime.
		for i := 0; i < count; i++ {
			// Each offset is between 0 and 59 seconds (within the window).
			offsetMs := rapid.IntRange(0, 59999).Draw(t, "offsetMs")
			ts := baseTime.Add(time.Duration(offsetMs) * time.Millisecond)

			shouldClose := rl.Record(ts)
			if shouldClose {
				t.Fatalf("connection should NOT be closed after %d malformed messages (≤10), but Record returned true at message %d", count, i+1)
			}
		}
	})
}

func TestProperty22_MalformedRateLimit_ClosesOnExceedingMaxWithinWindow(t *testing.T) {
	// For any sequence where count > 10 within a 60-second window,
	// the connection IS closed (Record returns true on the 11th message).
	rapid.Check(t, func(t *rapid.T) {
		window := 60 * time.Second
		maxCount := 10
		rl := newMalformedRateLimiter(window, maxCount)

		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		// Generate a total count between 11 and 50 (exceeds the limit).
		totalCount := rapid.IntRange(11, 50).Draw(t, "totalMalformedCount")

		closedAt := -1
		for i := 0; i < totalCount; i++ {
			// All timestamps within the 60-second window.
			offsetMs := rapid.IntRange(0, 59999).Draw(t, "offsetMs")
			ts := baseTime.Add(time.Duration(offsetMs) * time.Millisecond)

			shouldClose := rl.Record(ts)
			if shouldClose && closedAt == -1 {
				closedAt = i + 1 // 1-indexed message number that triggered close
			}
		}

		if closedAt == -1 {
			t.Fatalf("connection should have been closed after >10 malformed messages within window, but was never closed (sent %d messages)", totalCount)
		}
		if closedAt != 11 {
			t.Fatalf("connection should be closed on the 11th malformed message, but was closed on message %d", closedAt)
		}
	})
}

func TestProperty22_MalformedRateLimit_SlidingWindowEvictsOldEntries(t *testing.T) {
	// The sliding window correctly evicts old entries — messages older than 60 seconds
	// don't count toward the limit.
	rapid.Check(t, func(t *rapid.T) {
		window := 60 * time.Second
		maxCount := 10
		rl := newMalformedRateLimiter(window, maxCount)

		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		// Phase 1: Record some messages in the "old" period (these should expire).
		oldCount := rapid.IntRange(1, 10).Draw(t, "oldCount")
		// Generate a max offset for old messages so we know when they all expire.
		maxOldOffsetMs := rapid.IntRange(0, 30000).Draw(t, "maxOldOffsetMs")
		for i := 0; i < oldCount; i++ {
			// Old messages at baseTime + [0, maxOldOffsetMs].
			offsetMs := rapid.IntRange(0, maxOldOffsetMs).Draw(t, "oldOffsetMs")
			ts := baseTime.Add(time.Duration(offsetMs) * time.Millisecond)
			rl.Record(ts)
		}

		// Phase 2: Advance time past the window so ALL old messages expire.
		// The latest old message is at baseTime + maxOldOffsetMs.
		// It expires when now - 60s > baseTime + maxOldOffsetMs,
		// i.e., now > baseTime + maxOldOffsetMs + 60s.
		// We add 1ms to ensure strict expiration (eviction uses <= cutoff).
		newBaseTime := baseTime.Add(time.Duration(maxOldOffsetMs)*time.Millisecond + window + time.Millisecond)

		// Record up to 10 new messages — all should be tolerated since old ones expired.
		newCount := rapid.IntRange(1, 10).Draw(t, "newCount")
		for i := 0; i < newCount; i++ {
			// New messages within [0, 59s] from newBaseTime.
			offsetMs := rapid.IntRange(0, 59999).Draw(t, "newOffsetMs")
			ts := newBaseTime.Add(time.Duration(offsetMs) * time.Millisecond)

			shouldClose := rl.Record(ts)
			if shouldClose {
				t.Fatalf("connection should NOT be closed: old messages should have expired. "+
					"Old count: %d, new count so far: %d (message %d triggered close)",
					oldCount, i+1, i+1)
			}
		}

		// Verify the count reflects only the new messages.
		latestTime := newBaseTime.Add(59999 * time.Millisecond)
		count := rl.Count(latestTime)
		if count != newCount {
			t.Fatalf("expected count %d (only new messages), got %d", newCount, count)
		}
	})
}

func TestProperty22_MalformedRateLimit_Deterministic(t *testing.T) {
	// The rate limiter is deterministic for the same input sequence.
	// Running the same sequence of timestamps twice produces the same results.
	rapid.Check(t, func(t *rapid.T) {
		window := 60 * time.Second
		maxCount := 10

		baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		// Generate a sequence of timestamps.
		seqLen := rapid.IntRange(1, 30).Draw(t, "seqLen")
		offsets := make([]int, seqLen)
		for i := 0; i < seqLen; i++ {
			// Offsets can span across multiple windows to test eviction.
			offsets[i] = rapid.IntRange(0, 180000).Draw(t, "offsetMs")
		}

		// Run 1.
		rl1 := newMalformedRateLimiter(window, maxCount)
		results1 := make([]bool, seqLen)
		for i, offset := range offsets {
			ts := baseTime.Add(time.Duration(offset) * time.Millisecond)
			results1[i] = rl1.Record(ts)
		}

		// Run 2 with the same inputs.
		rl2 := newMalformedRateLimiter(window, maxCount)
		results2 := make([]bool, seqLen)
		for i, offset := range offsets {
			ts := baseTime.Add(time.Duration(offset) * time.Millisecond)
			results2[i] = rl2.Record(ts)
		}

		// Results must be identical.
		for i := range results1 {
			if results1[i] != results2[i] {
				t.Fatalf("non-deterministic behavior at message %d: run1=%v, run2=%v", i+1, results1[i], results2[i])
			}
		}
	})
}
