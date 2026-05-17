package connection

import "time"

// maxBackoffDelay is the maximum reconnection delay (60 seconds).
const maxBackoffDelay = 60 * time.Second

// BackoffDelay calculates the reconnection delay for a given retry attempt
// number N (starting at 1). The delay follows exponential backoff:
// min(2^(N-1) seconds, 60 seconds).
//
// The sequence is deterministic and monotonically non-decreasing.
func BackoffDelay(attempt int) time.Duration {
	if attempt < 1 {
		return time.Second
	}

	// Calculate 2^(attempt-1) seconds
	shift := attempt - 1
	if shift >= 6 {
		// 2^6 = 64 > 60, so anything at shift >= 6 is capped
		return maxBackoffDelay
	}

	delay := time.Duration(1<<uint(shift)) * time.Second
	if delay > maxBackoffDelay {
		return maxBackoffDelay
	}
	return delay
}
