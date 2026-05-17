package clientws

import (
	"fmt"
	"testing"
	"time"

	"pgregory.net/rapid"
)

// TestProperty21_BufferedMessageDelivery validates Property 21: Buffered Message Delivery.
//
// **Validates: Requirements 10.5**
//
// For any set of N messages buffered during a client disconnection within a 5-minute window,
// upon reconnection the server SHALL deliver min(N, 100) messages in chronological order.
// Messages older than 5 minutes SHALL NOT be delivered.
func TestProperty21_BufferedMessageDelivery(t *testing.T) {
	t.Run("drain returns min(N, 100) non-expired messages", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			// Generate a random number of messages (0 to 250).
			n := rapid.IntRange(0, 250).Draw(t, "numMessages")

			// Use a fixed "now" for the clock so all messages are fresh.
			now := time.Now()
			clock := func() time.Time { return now }
			mb := NewMessageBufferWithClock(clock)

			userID := "prop-user"

			// Add N messages, all within the 5-minute window (same timestamp).
			for i := 0; i < n; i++ {
				mb.Add(userID, []byte(fmt.Sprintf("msg-%d", i)))
			}

			msgs := mb.Drain(userID)

			// Expected count: min(N, maxBufferedMessages)
			expected := n
			if expected > maxBufferedMessages {
				expected = maxBufferedMessages
			}

			if n == 0 {
				// No messages added → Drain returns nil.
				if msgs != nil {
					t.Fatalf("expected nil for 0 messages, got %d", len(msgs))
				}
			} else {
				if len(msgs) != expected {
					t.Fatalf("expected %d messages (min(%d, %d)), got %d", expected, n, maxBufferedMessages, len(msgs))
				}
			}
		})
	})

	t.Run("messages are delivered in chronological order", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			// Generate between 2 and 100 messages to verify ordering.
			n := rapid.IntRange(2, 100).Draw(t, "numMessages")

			baseTime := time.Now()
			msgIndex := 0
			clock := func() time.Time {
				// Each message gets a timestamp 1 second apart.
				return baseTime.Add(time.Duration(msgIndex) * time.Second)
			}
			mb := NewMessageBufferWithClock(clock)

			userID := "prop-order-user"

			for i := 0; i < n; i++ {
				msgIndex = i
				mb.Add(userID, []byte(fmt.Sprintf("msg-%04d", i)))
			}

			// Drain at a time after all messages (so none are expired).
			msgIndex = n
			msgs := mb.Drain(userID)

			if len(msgs) != n {
				t.Fatalf("expected %d messages, got %d", n, len(msgs))
			}

			// Verify chronological order (oldest first).
			for i := 0; i < len(msgs)-1; i++ {
				current := string(msgs[i])
				next := string(msgs[i+1])
				if current >= next {
					t.Fatalf("messages not in chronological order at index %d: %q >= %q", i, current, next)
				}
			}
		})
	})

	t.Run("messages older than 5 minutes are not delivered", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			// Generate a mix of expired and fresh messages.
			numExpired := rapid.IntRange(0, 50).Draw(t, "numExpired")
			numFresh := rapid.IntRange(0, 100).Draw(t, "numFresh")

			baseTime := time.Now()
			currentTime := baseTime
			clock := func() time.Time { return currentTime }
			mb := NewMessageBufferWithClock(clock)

			userID := "prop-expiry-user"

			// Add expired messages (older than 5 minutes).
			for i := 0; i < numExpired; i++ {
				// Each expired message is 6-10 minutes old.
				ageMinutes := rapid.IntRange(6, 10).Draw(t, fmt.Sprintf("expiredAge_%d", i))
				currentTime = baseTime.Add(-time.Duration(ageMinutes) * time.Minute)
				mb.Add(userID, []byte(fmt.Sprintf("expired-%d", i)))
			}

			// Add fresh messages (within 5-minute window).
			for i := 0; i < numFresh; i++ {
				// Each fresh message is 0-4 minutes old.
				ageMinutes := rapid.IntRange(0, 4).Draw(t, fmt.Sprintf("freshAge_%d", i))
				currentTime = baseTime.Add(-time.Duration(ageMinutes) * time.Minute)
				mb.Add(userID, []byte(fmt.Sprintf("fresh-%d", i)))
			}

			// Drain at "now" (baseTime).
			currentTime = baseTime
			msgs := mb.Drain(userID)

			// Only fresh messages should be delivered (capped at 100).
			expectedFresh := numFresh
			if expectedFresh > maxBufferedMessages {
				expectedFresh = maxBufferedMessages
			}

			if numFresh == 0 {
				if msgs != nil {
					t.Fatalf("expected nil when all messages expired, got %d", len(msgs))
				}
			} else {
				if len(msgs) != expectedFresh {
					t.Fatalf("expected %d fresh messages, got %d (numExpired=%d, numFresh=%d)",
						expectedFresh, len(msgs), numExpired, numFresh)
				}

				// Verify no expired messages are present.
				for i, msg := range msgs {
					s := string(msg)
					if len(s) >= 7 && s[:7] == "expired" {
						t.Fatalf("expired message delivered at index %d: %q", i, s)
					}
				}
			}
		})
	})

	t.Run("buffer is empty after drain", func(t *testing.T) {
		rapid.Check(t, func(t *rapid.T) {
			n := rapid.IntRange(1, 200).Draw(t, "numMessages")

			now := time.Now()
			clock := func() time.Time { return now }
			mb := NewMessageBufferWithClock(clock)

			userID := "prop-empty-user"

			for i := 0; i < n; i++ {
				mb.Add(userID, []byte(fmt.Sprintf("msg-%d", i)))
			}

			// Drain all messages.
			_ = mb.Drain(userID)

			// Buffer must be empty after drain.
			if mb.Count(userID) != 0 {
				t.Fatalf("expected 0 messages after drain, got %d", mb.Count(userID))
			}

			// Second drain should return nil.
			secondDrain := mb.Drain(userID)
			if secondDrain != nil {
				t.Fatalf("expected nil on second drain, got %d messages", len(secondDrain))
			}
		})
	})
}
