package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

// mockDetector is a test double for AgentDetector that returns configurable results.
type mockDetector struct {
	mu       sync.Mutex
	runtimes []protocol.RuntimeInfo
	interval time.Duration
}

func (m *mockDetector) Scan() []protocol.RuntimeInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]protocol.RuntimeInfo, len(m.runtimes))
	copy(result, m.runtimes)
	return result
}

func (m *mockDetector) RescanInterval() time.Duration {
	return m.interval
}

func (m *mockDetector) setRuntimes(runtimes []protocol.RuntimeInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtimes = runtimes
}

func TestNewScanner(t *testing.T) {
	det := &mockDetector{interval: 50 * time.Millisecond}
	s := NewScanner(det, nil)
	if s == nil {
		t.Fatal("NewScanner returned nil")
	}
	if s.detector != det {
		t.Error("detector not set correctly")
	}
}

func TestScanner_InitialScan(t *testing.T) {
	runtimes := []protocol.RuntimeInfo{
		{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
	}
	det := &mockDetector{runtimes: runtimes, interval: 1 * time.Hour}

	var called bool
	var received []protocol.RuntimeInfo
	onChange := func(r []protocol.RuntimeInfo) {
		called = true
		received = r
	}

	s := NewScanner(det, onChange)
	ctx, cancel := context.WithCancel(context.Background())

	go s.Start(ctx)
	// Give the initial scan time to complete
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-s.done

	if !called {
		t.Fatal("onChange was not called on initial scan")
	}
	if len(received) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(received))
	}
	if received[0].AgentType != "claude" {
		t.Errorf("expected agent type 'claude', got %q", received[0].AgentType)
	}
}

func TestScanner_PeriodicRescan_DetectsChanges(t *testing.T) {
	initialRuntimes := []protocol.RuntimeInfo{
		{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
	}
	det := &mockDetector{runtimes: initialRuntimes, interval: 30 * time.Millisecond}

	var mu sync.Mutex
	var callCount int
	var lastReceived []protocol.RuntimeInfo
	onChange := func(r []protocol.RuntimeInfo) {
		mu.Lock()
		callCount++
		lastReceived = r
		mu.Unlock()
	}

	s := NewScanner(det, onChange)
	ctx, cancel := context.WithCancel(context.Background())

	go s.Start(ctx)
	// Wait for initial scan
	time.Sleep(20 * time.Millisecond)

	// Change the runtimes
	updatedRuntimes := []protocol.RuntimeInfo{
		{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "2.0.0", Status: "available"},
	}
	det.setRuntimes(updatedRuntimes)

	// Wait for at least one rescan cycle
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-s.done

	mu.Lock()
	defer mu.Unlock()

	if callCount < 2 {
		t.Fatalf("expected onChange to be called at least 2 times (initial + change), got %d", callCount)
	}
	if len(lastReceived) != 2 {
		t.Fatalf("expected 2 runtimes after change, got %d", len(lastReceived))
	}
}

func TestScanner_NoChangeDoesNotTriggerOnChange(t *testing.T) {
	runtimes := []protocol.RuntimeInfo{
		{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
	}
	det := &mockDetector{runtimes: runtimes, interval: 20 * time.Millisecond}

	var mu sync.Mutex
	var callCount int
	onChange := func(r []protocol.RuntimeInfo) {
		mu.Lock()
		callCount++
		mu.Unlock()
	}

	s := NewScanner(det, onChange)
	ctx, cancel := context.WithCancel(context.Background())

	go s.Start(ctx)
	// Wait for initial scan + a few rescan cycles
	time.Sleep(80 * time.Millisecond)
	cancel()
	<-s.done

	mu.Lock()
	defer mu.Unlock()

	// Should only be called once (initial scan), since runtimes don't change
	if callCount != 1 {
		t.Errorf("expected onChange to be called exactly 1 time (initial only), got %d", callCount)
	}
}

func TestScanner_EmptyRuntimesTriggersOnChange(t *testing.T) {
	// Start with some runtimes, then go to empty
	initialRuntimes := []protocol.RuntimeInfo{
		{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
	}
	det := &mockDetector{runtimes: initialRuntimes, interval: 30 * time.Millisecond}

	var mu sync.Mutex
	var callCount int
	var lastReceived []protocol.RuntimeInfo
	onChange := func(r []protocol.RuntimeInfo) {
		mu.Lock()
		callCount++
		lastReceived = r
		mu.Unlock()
	}

	s := NewScanner(det, onChange)
	ctx, cancel := context.WithCancel(context.Background())

	go s.Start(ctx)
	time.Sleep(20 * time.Millisecond)

	// Remove all runtimes
	det.setRuntimes(nil)

	// Wait for rescan
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-s.done

	mu.Lock()
	defer mu.Unlock()

	if callCount < 2 {
		t.Fatalf("expected onChange to be called at least 2 times, got %d", callCount)
	}
	if len(lastReceived) != 0 {
		t.Errorf("expected empty runtime list, got %d runtimes", len(lastReceived))
	}
}

func TestScanner_Stop(t *testing.T) {
	det := &mockDetector{
		runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
		interval: 20 * time.Millisecond,
	}

	s := NewScanner(det, func(r []protocol.RuntimeInfo) {})
	go s.Start(context.Background())

	// Give it time to start
	time.Sleep(30 * time.Millisecond)

	// Stop should not hang
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return within 2 seconds")
	}
}

func TestScanner_Current(t *testing.T) {
	runtimes := []protocol.RuntimeInfo{
		{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "2.0.0", Status: "available"},
	}
	det := &mockDetector{runtimes: runtimes, interval: 1 * time.Hour}

	s := NewScanner(det, nil)
	ctx, cancel := context.WithCancel(context.Background())

	go s.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-s.done

	current := s.Current()
	if len(current) != 2 {
		t.Fatalf("expected 2 runtimes, got %d", len(current))
	}
	if current[0].AgentType != "claude" {
		t.Errorf("expected first runtime to be 'claude', got %q", current[0].AgentType)
	}
	if current[1].AgentType != "gemini" {
		t.Errorf("expected second runtime to be 'gemini', got %q", current[1].AgentType)
	}
}

func TestRuntimesEqual(t *testing.T) {
	tests := []struct {
		name string
		a    []protocol.RuntimeInfo
		b    []protocol.RuntimeInfo
		want bool
	}{
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
		{
			name: "both empty",
			a:    []protocol.RuntimeInfo{},
			b:    []protocol.RuntimeInfo{},
			want: true,
		},
		{
			name: "nil vs empty",
			a:    nil,
			b:    []protocol.RuntimeInfo{},
			want: true,
		},
		{
			name: "same single element",
			a:    []protocol.RuntimeInfo{{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"}},
			b:    []protocol.RuntimeInfo{{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"}},
			want: true,
		},
		{
			name: "different length",
			a:    []protocol.RuntimeInfo{{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"}},
			b:    []protocol.RuntimeInfo{},
			want: false,
		},
		{
			name: "different version",
			a:    []protocol.RuntimeInfo{{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"}},
			b:    []protocol.RuntimeInfo{{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "2.0", Status: "available"}},
			want: false,
		},
		{
			name: "different status",
			a:    []protocol.RuntimeInfo{{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"}},
			b:    []protocol.RuntimeInfo{{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "unavailable"}},
			want: false,
		},
		{
			name: "different order",
			a: []protocol.RuntimeInfo{
				{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"},
				{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "2.0", Status: "available"},
			},
			b: []protocol.RuntimeInfo{
				{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "2.0", Status: "available"},
				{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runtimesEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("runtimesEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScanner_NilOnChange(t *testing.T) {
	runtimes := []protocol.RuntimeInfo{
		{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
	}
	det := &mockDetector{runtimes: runtimes, interval: 1 * time.Hour}

	// Should not panic with nil onChange
	s := NewScanner(det, nil)
	ctx, cancel := context.WithCancel(context.Background())

	go s.Start(ctx)
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-s.done

	// Verify current was still updated
	current := s.Current()
	if len(current) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(current))
	}
}
