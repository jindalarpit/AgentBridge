// Package heartbeat implements the periodic heartbeat ticker for daemon liveness.
package heartbeat

import (
	"log"
	"sync"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

// DefaultInterval is the default heartbeat interval.
const DefaultInterval = 15 * time.Second

// Ticker sends periodic heartbeat messages to the server at a configurable interval.
type Ticker struct {
	interval time.Duration
	sendFn   func(protocol.Message) error
	done     chan struct{}
	wg       sync.WaitGroup
	mu       sync.Mutex
	running  bool
}

// NewTicker creates a new heartbeat Ticker with the given interval and send function.
// The sendFn is called to send each heartbeat message to the server.
// If interval is zero or negative, DefaultInterval (15s) is used.
func NewTicker(interval time.Duration, sendFn func(protocol.Message) error) *Ticker {
	if interval <= 0 {
		interval = DefaultInterval
	}
	return &Ticker{
		interval: interval,
		sendFn:   sendFn,
		done:     make(chan struct{}),
	}
}

// Start begins sending heartbeat messages at the configured interval.
// It is safe to call Start multiple times; subsequent calls are no-ops if already running.
func (t *Ticker) Start() {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return
	}
	t.running = true
	t.done = make(chan struct{})
	t.mu.Unlock()

	t.wg.Add(1)
	go t.loop()
}

// Stop signals the heartbeat goroutine to stop and waits for it to finish.
// It is safe to call Stop multiple times; subsequent calls are no-ops if not running.
func (t *Ticker) Stop() {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return
	}
	t.running = false
	close(t.done)
	t.mu.Unlock()

	t.wg.Wait()
}

// Running reports whether the ticker is currently active.
func (t *Ticker) Running() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.running
}

func (t *Ticker) loop() {
	defer t.wg.Done()

	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	for {
		select {
		case <-t.done:
			return
		case <-ticker.C:
			msg := protocol.Message{
				Type: protocol.TypeDaemonHeartbeat,
			}
			if err := t.sendFn(msg); err != nil {
				log.Printf("heartbeat: failed to send: %v", err)
			}
		}
	}
}
