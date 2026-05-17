package daemonws

import (
	"context"
	"log"
	"sync"
	"time"
)

// DefaultHeartbeatInterval is the default interval at which daemons send heartbeats.
const DefaultHeartbeatInterval = 15 * time.Second

// ShouldMarkOffline returns true if the time since the last heartbeat exceeds
// 3× the configured heartbeat interval, indicating the daemon should be marked offline.
func ShouldMarkOffline(heartbeatInterval, timeSinceLastHeartbeat time.Duration) bool {
	return timeSinceLastHeartbeat >= 3*heartbeatInterval
}

// HeartbeatChecker monitors daemon heartbeats and detects timeouts.
// It maintains a map of daemon_id → last_seen_at and periodically checks
// for daemons that have missed their heartbeat threshold.
type HeartbeatChecker struct {
	mu       sync.RWMutex
	daemons  map[string]time.Time // daemon_id → last_seen_at
	interval time.Duration        // heartbeat interval

	cancel context.CancelFunc
	done   chan struct{}
}

// NewHeartbeatChecker creates a new HeartbeatChecker with the given heartbeat interval.
// If interval is zero or negative, DefaultHeartbeatInterval is used.
func NewHeartbeatChecker(interval time.Duration) *HeartbeatChecker {
	if interval <= 0 {
		interval = DefaultHeartbeatInterval
	}
	return &HeartbeatChecker{
		daemons:  make(map[string]time.Time),
		interval: interval,
	}
}

// RecordHeartbeat updates the last_seen_at timestamp for the given daemon to now.
func (hc *HeartbeatChecker) RecordHeartbeat(daemonID string) {
	hc.mu.Lock()
	hc.daemons[daemonID] = time.Now()
	hc.mu.Unlock()
}

// RemoveDaemon removes a daemon from heartbeat tracking.
// This should be called when a daemon disconnects or deregisters.
func (hc *HeartbeatChecker) RemoveDaemon(daemonID string) {
	hc.mu.Lock()
	delete(hc.daemons, daemonID)
	hc.mu.Unlock()
}

// Start begins the background goroutine that periodically checks all tracked
// daemons for missed heartbeats. When a daemon exceeds the timeout threshold
// (3× interval), the onTimeout callback is invoked with the daemon's ID.
// The goroutine runs every heartbeatInterval.
func (hc *HeartbeatChecker) Start(ctx context.Context, onTimeout func(daemonID string)) {
	ctx, cancel := context.WithCancel(ctx)
	hc.cancel = cancel
	hc.done = make(chan struct{})

	go hc.run(ctx, onTimeout)
}

// Stop stops the background heartbeat checker goroutine and waits for it to finish.
func (hc *HeartbeatChecker) Stop() {
	if hc.cancel != nil {
		hc.cancel()
	}
	if hc.done != nil {
		<-hc.done
	}
}

// run is the background loop that checks for timed-out daemons.
func (hc *HeartbeatChecker) run(ctx context.Context, onTimeout func(daemonID string)) {
	defer close(hc.done)

	ticker := time.NewTicker(hc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hc.checkTimeouts(onTimeout)
		}
	}
}

// checkTimeouts iterates over all tracked daemons and invokes onTimeout
// for any daemon whose last heartbeat exceeds the threshold.
func (hc *HeartbeatChecker) checkTimeouts(onTimeout func(daemonID string)) {
	now := time.Now()

	hc.mu.RLock()
	// Collect timed-out daemon IDs while holding the read lock.
	var timedOut []string
	for daemonID, lastSeen := range hc.daemons {
		if ShouldMarkOffline(hc.interval, now.Sub(lastSeen)) {
			timedOut = append(timedOut, daemonID)
		}
	}
	hc.mu.RUnlock()

	// Invoke callbacks and remove timed-out daemons outside the read lock.
	for _, daemonID := range timedOut {
		log.Printf("daemonws: heartbeat timeout for daemon %s", daemonID)
		onTimeout(daemonID)
		hc.RemoveDaemon(daemonID)
	}
}
