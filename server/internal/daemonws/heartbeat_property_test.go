package daemonws

import (
	"testing"
	"time"

	"pgregory.net/rapid"
)

// **Validates: Requirements 2.5**
//
// Property 3: Heartbeat Timeout Detection
// For any heartbeat interval I and any time gap G since the last heartbeat,
// the server SHALL mark the daemon as offline if and only if G ≥ 3 × I.
// For gaps less than 3 × I, the daemon SHALL remain in "online" status.

// genHeartbeatInterval generates a valid heartbeat interval between 5s and 120s.
func genHeartbeatInterval() *rapid.Generator[time.Duration] {
	return rapid.Custom(func(t *rapid.T) time.Duration {
		seconds := rapid.IntRange(5, 120).Draw(t, "interval_seconds")
		return time.Duration(seconds) * time.Second
	})
}

// TestProperty_HeartbeatTimeout_GapAtOrAboveThreshold verifies that for any
// heartbeat interval I (5s-120s) and any gap G where G >= 3*I,
// ShouldMarkOffline returns true (daemon should be marked offline).
func TestProperty_HeartbeatTimeout_GapAtOrAboveThreshold(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		interval := genHeartbeatInterval().Draw(t, "interval")
		threshold := 3 * interval

		// Generate a gap that is at or above the threshold.
		// Add 0 to 10 minutes of extra time beyond the threshold.
		extraNanos := rapid.Int64Range(0, int64(10*time.Minute)).Draw(t, "extra_nanos")
		gap := threshold + time.Duration(extraNanos)

		result := ShouldMarkOffline(interval, gap)
		if !result {
			t.Fatalf("expected ShouldMarkOffline=true for interval=%v, gap=%v (threshold=%v), got false",
				interval, gap, threshold)
		}
	})
}

// TestProperty_HeartbeatTimeout_GapBelowThreshold verifies that for any
// heartbeat interval I (5s-120s) and any gap G where G < 3*I,
// ShouldMarkOffline returns false (daemon should remain online).
func TestProperty_HeartbeatTimeout_GapBelowThreshold(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		interval := genHeartbeatInterval().Draw(t, "interval")
		threshold := 3 * interval

		// Generate a gap that is strictly below the threshold.
		// Gap ranges from 0 to threshold - 1 nanosecond.
		gapNanos := rapid.Int64Range(0, int64(threshold)-1).Draw(t, "gap_nanos")
		gap := time.Duration(gapNanos)

		result := ShouldMarkOffline(interval, gap)
		if result {
			t.Fatalf("expected ShouldMarkOffline=false for interval=%v, gap=%v (threshold=%v), got true",
				interval, gap, threshold)
		}
	})
}

// TestProperty_HeartbeatTimeout_ExactBoundaryIsOffline verifies that for any
// heartbeat interval I (5s-120s), the exact boundary (G == 3*I) returns true
// (daemon should be marked offline).
func TestProperty_HeartbeatTimeout_ExactBoundaryIsOffline(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		interval := genHeartbeatInterval().Draw(t, "interval")
		gap := 3 * interval // Exact boundary

		result := ShouldMarkOffline(interval, gap)
		if !result {
			t.Fatalf("expected ShouldMarkOffline=true at exact boundary for interval=%v, gap=%v, got false",
				interval, gap)
		}
	})
}

// TestProperty_HeartbeatTimeout_JustBelowBoundaryIsOnline verifies that for any
// heartbeat interval I (5s-120s), just below the boundary (G == 3*I - 1ns)
// returns false (daemon should remain online).
func TestProperty_HeartbeatTimeout_JustBelowBoundaryIsOnline(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		interval := genHeartbeatInterval().Draw(t, "interval")
		gap := 3*interval - 1*time.Nanosecond // Just below boundary

		result := ShouldMarkOffline(interval, gap)
		if result {
			t.Fatalf("expected ShouldMarkOffline=false just below boundary for interval=%v, gap=%v, got true",
				interval, gap)
		}
	})
}
