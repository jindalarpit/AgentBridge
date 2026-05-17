package heartbeat

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

func TestTickerSendsHeartbeatsAtInterval(t *testing.T) {
	var count atomic.Int32

	sendFn := func(msg protocol.Message) error {
		if msg.Type != protocol.TypeDaemonHeartbeat {
			t.Errorf("expected message type %q, got %q", protocol.TypeDaemonHeartbeat, msg.Type)
		}
		count.Add(1)
		return nil
	}

	ticker := NewTicker(50*time.Millisecond, sendFn)
	ticker.Start()

	// Wait enough time for approximately 3-5 heartbeats
	time.Sleep(220 * time.Millisecond)
	ticker.Stop()

	got := count.Load()
	if got < 3 || got > 6 {
		t.Errorf("expected 3-6 heartbeats in ~220ms at 50ms interval, got %d", got)
	}
}

func TestTickerStopStopsHeartbeats(t *testing.T) {
	var count atomic.Int32

	sendFn := func(msg protocol.Message) error {
		count.Add(1)
		return nil
	}

	ticker := NewTicker(50*time.Millisecond, sendFn)
	ticker.Start()

	// Let a few heartbeats fire
	time.Sleep(130 * time.Millisecond)
	ticker.Stop()

	countAtStop := count.Load()

	// Wait more time and verify no additional heartbeats are sent
	time.Sleep(150 * time.Millisecond)
	countAfter := count.Load()

	if countAfter != countAtStop {
		t.Errorf("heartbeats continued after Stop(): count at stop=%d, count after=%d", countAtStop, countAfter)
	}
}

func TestTickerDefaultInterval(t *testing.T) {
	sendFn := func(msg protocol.Message) error { return nil }

	// Zero interval should use default
	ticker := NewTicker(0, sendFn)
	if ticker.interval != DefaultInterval {
		t.Errorf("expected default interval %v, got %v", DefaultInterval, ticker.interval)
	}

	// Negative interval should use default
	ticker2 := NewTicker(-1*time.Second, sendFn)
	if ticker2.interval != DefaultInterval {
		t.Errorf("expected default interval %v, got %v", DefaultInterval, ticker2.interval)
	}
}

func TestTickerStartIsIdempotent(t *testing.T) {
	var count atomic.Int32

	sendFn := func(msg protocol.Message) error {
		count.Add(1)
		return nil
	}

	ticker := NewTicker(50*time.Millisecond, sendFn)
	ticker.Start()
	ticker.Start() // second call should be a no-op
	ticker.Start() // third call should be a no-op

	time.Sleep(130 * time.Millisecond)
	ticker.Stop()

	// With a single goroutine at 50ms interval over ~130ms, expect 2-3 heartbeats
	got := count.Load()
	if got < 2 || got > 4 {
		t.Errorf("expected 2-4 heartbeats (single goroutine), got %d", got)
	}
}

func TestTickerStopIsIdempotent(t *testing.T) {
	sendFn := func(msg protocol.Message) error { return nil }

	ticker := NewTicker(50*time.Millisecond, sendFn)
	ticker.Start()
	time.Sleep(60 * time.Millisecond)

	// Multiple stops should not panic
	ticker.Stop()
	ticker.Stop()
	ticker.Stop()
}

func TestTickerContinuesOnSendError(t *testing.T) {
	var count atomic.Int32
	var mu sync.Mutex
	var errors []error

	sendFn := func(msg protocol.Message) error {
		count.Add(1)
		err := &testError{msg: "send failed"}
		mu.Lock()
		errors = append(errors, err)
		mu.Unlock()
		return err
	}

	ticker := NewTicker(50*time.Millisecond, sendFn)
	ticker.Start()

	// Even with errors, the ticker should keep sending
	time.Sleep(180 * time.Millisecond)
	ticker.Stop()

	got := count.Load()
	if got < 2 {
		t.Errorf("expected at least 2 heartbeat attempts despite errors, got %d", got)
	}
}

func TestTickerRunningState(t *testing.T) {
	sendFn := func(msg protocol.Message) error { return nil }

	ticker := NewTicker(50*time.Millisecond, sendFn)

	if ticker.Running() {
		t.Error("ticker should not be running before Start()")
	}

	ticker.Start()
	if !ticker.Running() {
		t.Error("ticker should be running after Start()")
	}

	ticker.Stop()
	if ticker.Running() {
		t.Error("ticker should not be running after Stop()")
	}
}

func TestTickerCanBeRestartedAfterStop(t *testing.T) {
	var count atomic.Int32

	sendFn := func(msg protocol.Message) error {
		count.Add(1)
		return nil
	}

	ticker := NewTicker(50*time.Millisecond, sendFn)

	// First run
	ticker.Start()
	time.Sleep(120 * time.Millisecond)
	ticker.Stop()

	firstRunCount := count.Load()
	if firstRunCount < 1 {
		t.Fatalf("expected at least 1 heartbeat in first run, got %d", firstRunCount)
	}

	// Second run
	ticker.Start()
	time.Sleep(120 * time.Millisecond)
	ticker.Stop()

	totalCount := count.Load()
	if totalCount <= firstRunCount {
		t.Errorf("expected more heartbeats after restart, first=%d, total=%d", firstRunCount, totalCount)
	}
}

type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
