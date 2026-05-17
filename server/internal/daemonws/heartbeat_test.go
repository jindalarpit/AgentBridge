package daemonws

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestShouldMarkOffline(t *testing.T) {
	tests := []struct {
		name       string
		interval   time.Duration
		gap        time.Duration
		wantOffline bool
	}{
		{
			name:        "exactly at threshold marks offline",
			interval:    15 * time.Second,
			gap:         45 * time.Second,
			wantOffline: true,
		},
		{
			name:        "above threshold marks offline",
			interval:    15 * time.Second,
			gap:         60 * time.Second,
			wantOffline: true,
		},
		{
			name:        "just below threshold stays online",
			interval:    15 * time.Second,
			gap:         44 * time.Second,
			wantOffline: false,
		},
		{
			name:        "zero gap stays online",
			interval:    15 * time.Second,
			gap:         0,
			wantOffline: false,
		},
		{
			name:        "one interval stays online",
			interval:    10 * time.Second,
			gap:         10 * time.Second,
			wantOffline: false,
		},
		{
			name:        "two intervals stays online",
			interval:    10 * time.Second,
			gap:         20 * time.Second,
			wantOffline: false,
		},
		{
			name:        "three intervals marks offline",
			interval:    10 * time.Second,
			gap:         30 * time.Second,
			wantOffline: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldMarkOffline(tt.interval, tt.gap)
			if got != tt.wantOffline {
				t.Errorf("ShouldMarkOffline(%v, %v) = %v, want %v",
					tt.interval, tt.gap, got, tt.wantOffline)
			}
		})
	}
}

func TestNewHeartbeatChecker_DefaultInterval(t *testing.T) {
	hc := NewHeartbeatChecker(0)
	if hc.interval != DefaultHeartbeatInterval {
		t.Errorf("expected default interval %v, got %v", DefaultHeartbeatInterval, hc.interval)
	}

	hc2 := NewHeartbeatChecker(-1 * time.Second)
	if hc2.interval != DefaultHeartbeatInterval {
		t.Errorf("expected default interval for negative input, got %v", hc2.interval)
	}
}

func TestNewHeartbeatChecker_CustomInterval(t *testing.T) {
	interval := 30 * time.Second
	hc := NewHeartbeatChecker(interval)
	if hc.interval != interval {
		t.Errorf("expected interval %v, got %v", interval, hc.interval)
	}
}

func TestRecordHeartbeat_UpdatesTimestamp(t *testing.T) {
	hc := NewHeartbeatChecker(15 * time.Second)

	before := time.Now()
	hc.RecordHeartbeat("daemon-1")
	after := time.Now()

	hc.mu.RLock()
	lastSeen, ok := hc.daemons["daemon-1"]
	hc.mu.RUnlock()

	if !ok {
		t.Fatal("expected daemon-1 to be tracked")
	}
	if lastSeen.Before(before) || lastSeen.After(after) {
		t.Errorf("last_seen_at %v not between %v and %v", lastSeen, before, after)
	}
}

func TestRecordHeartbeat_UpdatesExistingDaemon(t *testing.T) {
	hc := NewHeartbeatChecker(15 * time.Second)

	hc.RecordHeartbeat("daemon-1")
	time.Sleep(5 * time.Millisecond)

	hc.mu.RLock()
	firstSeen := hc.daemons["daemon-1"]
	hc.mu.RUnlock()

	hc.RecordHeartbeat("daemon-1")

	hc.mu.RLock()
	secondSeen := hc.daemons["daemon-1"]
	hc.mu.RUnlock()

	if !secondSeen.After(firstSeen) {
		t.Errorf("expected second heartbeat to update timestamp: first=%v, second=%v",
			firstSeen, secondSeen)
	}
}

func TestRemoveDaemon(t *testing.T) {
	hc := NewHeartbeatChecker(15 * time.Second)

	hc.RecordHeartbeat("daemon-1")
	hc.RecordHeartbeat("daemon-2")

	hc.RemoveDaemon("daemon-1")

	hc.mu.RLock()
	_, ok1 := hc.daemons["daemon-1"]
	_, ok2 := hc.daemons["daemon-2"]
	hc.mu.RUnlock()

	if ok1 {
		t.Error("expected daemon-1 to be removed")
	}
	if !ok2 {
		t.Error("expected daemon-2 to still be tracked")
	}
}

func TestRemoveDaemon_NonExistent(t *testing.T) {
	hc := NewHeartbeatChecker(15 * time.Second)

	// Should not panic when removing a non-existent daemon.
	hc.RemoveDaemon("non-existent")

	hc.mu.RLock()
	count := len(hc.daemons)
	hc.mu.RUnlock()

	if count != 0 {
		t.Errorf("expected empty map, got %d entries", count)
	}
}

func TestHeartbeatChecker_TimeoutDetection(t *testing.T) {
	// Use a very short interval for fast testing.
	interval := 20 * time.Millisecond
	hc := NewHeartbeatChecker(interval)

	var mu sync.Mutex
	timedOutDaemons := make(map[string]int)

	onTimeout := func(daemonID string) {
		mu.Lock()
		timedOutDaemons[daemonID]++
		mu.Unlock()
	}

	ctx := context.Background()
	hc.Start(ctx, onTimeout)
	defer hc.Stop()

	// Record heartbeat for daemon-1 (will keep alive) and daemon-2 (will timeout).
	hc.RecordHeartbeat("daemon-1")
	hc.RecordHeartbeat("daemon-2")

	// Keep daemon-1 alive by sending heartbeats, let daemon-2 timeout.
	// Threshold is 3 * 20ms = 60ms. We'll wait long enough for the checker to detect it.
	go func() {
		for i := 0; i < 10; i++ {
			time.Sleep(interval / 2)
			hc.RecordHeartbeat("daemon-1")
		}
	}()

	// Wait for at least 3 intervals + one check cycle for daemon-2 to timeout.
	time.Sleep(4 * interval)

	mu.Lock()
	d2Count := timedOutDaemons["daemon-2"]
	d1Count := timedOutDaemons["daemon-1"]
	mu.Unlock()

	if d2Count == 0 {
		t.Error("expected daemon-2 to be timed out")
	}
	if d1Count != 0 {
		t.Error("expected daemon-1 to NOT be timed out (it was kept alive)")
	}
}

func TestHeartbeatChecker_StopPreventsCallbacks(t *testing.T) {
	interval := 10 * time.Millisecond
	hc := NewHeartbeatChecker(interval)

	var mu sync.Mutex
	callbackCount := 0

	onTimeout := func(daemonID string) {
		mu.Lock()
		callbackCount++
		mu.Unlock()
	}

	ctx := context.Background()
	hc.Start(ctx, onTimeout)

	// Record a heartbeat that will eventually timeout.
	hc.RecordHeartbeat("daemon-stop-test")

	// Stop immediately before timeout can occur.
	hc.Stop()

	// Wait a bit to ensure no callbacks fire after stop.
	time.Sleep(5 * interval)

	mu.Lock()
	count := callbackCount
	mu.Unlock()

	if count != 0 {
		t.Errorf("expected no callbacks after Stop(), got %d", count)
	}
}

func TestHeartbeatChecker_MultipleTimeouts(t *testing.T) {
	interval := 15 * time.Millisecond
	hc := NewHeartbeatChecker(interval)

	var mu sync.Mutex
	timedOutDaemons := make(map[string]bool)

	onTimeout := func(daemonID string) {
		mu.Lock()
		timedOutDaemons[daemonID] = true
		mu.Unlock()
	}

	ctx := context.Background()
	hc.Start(ctx, onTimeout)
	defer hc.Stop()

	// Register multiple daemons that will all timeout.
	hc.RecordHeartbeat("daemon-a")
	hc.RecordHeartbeat("daemon-b")
	hc.RecordHeartbeat("daemon-c")

	// Wait for all to timeout (3 * interval + check cycle).
	time.Sleep(5 * interval)

	mu.Lock()
	aTimedOut := timedOutDaemons["daemon-a"]
	bTimedOut := timedOutDaemons["daemon-b"]
	cTimedOut := timedOutDaemons["daemon-c"]
	mu.Unlock()

	if !aTimedOut {
		t.Error("expected daemon-a to timeout")
	}
	if !bTimedOut {
		t.Error("expected daemon-b to timeout")
	}
	if !cTimedOut {
		t.Error("expected daemon-c to timeout")
	}
}

func TestHeartbeatChecker_TimedOutDaemonRemovedFromTracking(t *testing.T) {
	interval := 15 * time.Millisecond
	hc := NewHeartbeatChecker(interval)

	onTimeout := func(daemonID string) {
		// no-op
	}

	ctx := context.Background()
	hc.Start(ctx, onTimeout)
	defer hc.Stop()

	hc.RecordHeartbeat("daemon-remove-test")

	// Wait for timeout.
	time.Sleep(5 * interval)

	// After timeout, daemon should be removed from tracking.
	hc.mu.RLock()
	_, ok := hc.daemons["daemon-remove-test"]
	hc.mu.RUnlock()

	if ok {
		t.Error("expected timed-out daemon to be removed from tracking")
	}
}
