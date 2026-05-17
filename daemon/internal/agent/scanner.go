package agent

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

// Scanner periodically runs agent detection and notifies when the runtime list changes.
type Scanner struct {
	detector AgentDetector
	onChange func([]protocol.RuntimeInfo)

	mu       sync.Mutex
	current  []protocol.RuntimeInfo
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewScanner creates a new Scanner that uses the given detector and calls onChange
// whenever the detected runtime list changes.
func NewScanner(detector AgentDetector, onChange func([]protocol.RuntimeInfo)) *Scanner {
	return &Scanner{
		detector: detector,
		onChange: onChange,
		done:     make(chan struct{}),
	}
}

// Start runs an initial scan and then periodically rescans at the detector's
// configured interval. It blocks until Stop is called or the context is cancelled.
func (s *Scanner) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)

	// Run initial scan
	s.runScan()

	ticker := time.NewTicker(s.detector.RescanInterval())
	defer ticker.Stop()
	defer close(s.done)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runScan()
		}
	}
}

// Stop cancels the scanner's context and waits for the scan loop to exit.
func (s *Scanner) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	<-s.done
}

// Current returns the most recently detected runtime list.
func (s *Scanner) Current() []protocol.RuntimeInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]protocol.RuntimeInfo, len(s.current))
	copy(result, s.current)
	return result
}

// runScan performs a single detection scan, compares with the current list,
// and triggers onChange if the runtimes have changed.
func (s *Scanner) runScan() {
	newRuntimes := s.detector.Scan()

	if len(newRuntimes) == 0 {
		log.Println("WARNING: no agent CLIs found on this system")
	}

	s.mu.Lock()
	changed := !runtimesEqual(s.current, newRuntimes)
	if changed {
		s.current = newRuntimes
	}
	s.mu.Unlock()

	if changed && s.onChange != nil {
		s.onChange(newRuntimes)
	}
}

// runtimesEqual compares two RuntimeInfo slices for equality.
func runtimesEqual(a, b []protocol.RuntimeInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
