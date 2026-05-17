package connection

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// **Validates: Requirements 2.6**
// Property 4: Exponential Backoff Sequence
// For any retry attempt number N (starting at 1), the reconnection delay SHALL
// equal min(2^(N-1) seconds, 60 seconds). The sequence SHALL be deterministic
// and monotonically non-decreasing.

// TestProperty_BackoffDelayFormula verifies that for any attempt N >= 1,
// BackoffDelay(N) == min(2^(N-1) seconds, 60 seconds).
func TestProperty_BackoffDelayFormula(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		attempt := rapid.IntRange(1, 100).Draw(t, "attempt")

		got := BackoffDelay(attempt)

		// Calculate expected: min(2^(attempt-1) seconds, 60 seconds)
		shift := attempt - 1
		var expected time.Duration
		if shift >= 63 {
			// Overflow protection: 2^63 overflows int64
			expected = 60 * time.Second
		} else {
			raw := time.Duration(1<<uint(shift)) * time.Second
			if raw > 60*time.Second || raw <= 0 {
				// raw <= 0 handles overflow for large shifts
				expected = 60 * time.Second
			} else {
				expected = raw
			}
		}

		if got != expected {
			t.Fatalf("BackoffDelay(%d) = %v, want %v", attempt, got, expected)
		}
	})
}

// TestProperty_BackoffDelayCap verifies that for any attempt N >= 1,
// BackoffDelay(N) <= 60 seconds (cap is always respected).
func TestProperty_BackoffDelayCap(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		attempt := rapid.IntRange(1, 1000).Draw(t, "attempt")

		got := BackoffDelay(attempt)

		if got > 60*time.Second {
			t.Fatalf("BackoffDelay(%d) = %v, exceeds 60s cap", attempt, got)
		}
	})
}

// TestProperty_BackoffDelayMonotonicallyNonDecreasing verifies that for any two
// consecutive attempts N and N+1, BackoffDelay(N+1) >= BackoffDelay(N).
func TestProperty_BackoffDelayMonotonicallyNonDecreasing(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		attempt := rapid.IntRange(1, 999).Draw(t, "attempt")

		current := BackoffDelay(attempt)
		next := BackoffDelay(attempt + 1)

		if next < current {
			t.Fatalf("BackoffDelay is not monotonically non-decreasing: "+
				"BackoffDelay(%d) = %v > BackoffDelay(%d) = %v",
				attempt, current, attempt+1, next)
		}
	})
}

// TestProperty_BackoffDelayLargeAttemptsCapped verifies that for any attempt
// N >= 7, BackoffDelay(N) == 60 seconds (all large attempts are capped).
func TestProperty_BackoffDelayLargeAttemptsCapped(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		attempt := rapid.IntRange(7, 10000).Draw(t, "attempt")

		got := BackoffDelay(attempt)

		if got != 60*time.Second {
			t.Fatalf("BackoffDelay(%d) = %v, want 60s (should be capped for attempt >= 7)",
				attempt, got)
		}
	})
}

// TestProperty_BackoffDelayDeterministic verifies that BackoffDelay is
// deterministic: calling it twice with the same N gives the same result.
func TestProperty_BackoffDelayDeterministic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		attempt := rapid.IntRange(1, 10000).Draw(t, "attempt")

		first := BackoffDelay(attempt)
		second := BackoffDelay(attempt)

		if first != second {
			t.Fatalf("BackoffDelay(%d) is not deterministic: first=%v, second=%v",
				attempt, first, second)
		}
	})
}
