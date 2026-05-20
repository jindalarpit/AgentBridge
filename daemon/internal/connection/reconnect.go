package connection

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

// ReconnectingConnection wraps a Connection with automatic reconnection logic
// using exponential backoff. When the underlying connection is lost, it
// automatically attempts to reconnect and re-sends the DaemonRegister message
// on success via the onReconnect callback.
//
// If a 401 authentication error is received during reconnection, the loop stops
// immediately and the error is sent on the AuthErr channel.
type ReconnectingConnection struct {
	serverURL string
	token     string

	conn    *Connection
	connMu  sync.RWMutex
	attempt int32 // atomic reconnection attempt counter

	onReconnect func() // called after successful reconnect (re-send DaemonRegister)

	cancel  context.CancelFunc
	ctx     context.Context
	stopped atomic.Bool

	monitorDone chan struct{} // closed when monitor goroutine exits

	// AuthErr receives ErrAuthFailed when a 401 is encountered during reconnection.
	// The caller should select on this channel and exit with code 2 when it fires.
	AuthErr chan error
}

// NewReconnectingConnection creates a new ReconnectingConnection configured to
// connect to the given server URL with the provided authentication token.
// The onReconnect callback is invoked after each successful reconnection and
// should be used to re-send the DaemonRegister message.
func NewReconnectingConnection(serverURL, token string, onReconnect func()) *ReconnectingConnection {
	return &ReconnectingConnection{
		serverURL:   serverURL,
		token:       token,
		onReconnect: onReconnect,
		monitorDone: make(chan struct{}),
		AuthErr:     make(chan error, 1),
	}
}

// Start establishes the initial connection and starts the monitoring goroutine
// that handles automatic reconnection on connection loss.
// Returns ErrAuthFailed if the server responds with HTTP 401 during initial connect.
func (rc *ReconnectingConnection) Start(ctx context.Context) error {
	rc.ctx, rc.cancel = context.WithCancel(ctx)

	conn := NewConnection(rc.serverURL, rc.token)

	if err := conn.Connect(rc.ctx); err != nil {
		if errors.Is(err, ErrAuthFailed) {
			return ErrAuthFailed
		}
		return fmt.Errorf("initial connection failed: %w", err)
	}

	rc.connMu.Lock()
	rc.conn = conn
	rc.connMu.Unlock()

	go rc.monitor()

	return nil
}

// Stop cancels the context, stops the monitoring goroutine, and closes the
// underlying connection.
func (rc *ReconnectingConnection) Stop() {
	if rc.stopped.Swap(true) {
		return // already stopped
	}

	if rc.cancel != nil {
		rc.cancel()
	}

	// Wait for monitor goroutine to exit
	<-rc.monitorDone

	rc.connMu.RLock()
	conn := rc.conn
	rc.connMu.RUnlock()

	if conn != nil {
		_ = conn.Close()
	}
}

// Send delegates to the underlying connection's Send method.
func (rc *ReconnectingConnection) Send(msg protocol.Message) error {
	rc.connMu.RLock()
	conn := rc.conn
	rc.connMu.RUnlock()

	if conn == nil {
		return fmt.Errorf("no active connection")
	}
	return conn.Send(msg)
}

// OnMessage registers a handler function that will be called for each incoming
// message. The handler is set on the current underlying connection and will be
// re-applied on reconnection.
func (rc *ReconnectingConnection) OnMessage(handler func(protocol.Message)) {
	rc.connMu.RLock()
	conn := rc.conn
	rc.connMu.RUnlock()

	if conn != nil {
		conn.OnMessage(handler)
	}
}

// IsConnected returns whether the underlying connection is currently active.
func (rc *ReconnectingConnection) IsConnected() bool {
	rc.connMu.RLock()
	conn := rc.conn
	rc.connMu.RUnlock()

	if conn == nil {
		return false
	}
	return conn.IsConnected()
}

// monitor watches the connection state and triggers reconnection when the
// connection is lost. It respects context cancellation for clean shutdown.
func (rc *ReconnectingConnection) monitor() {
	defer close(rc.monitorDone)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-rc.ctx.Done():
			return
		case <-ticker.C:
			if !rc.IsConnected() && !rc.stopped.Load() {
				rc.reconnectLoop()
			}
		}
	}
}

// reconnectLoop attempts to reconnect with exponential backoff until successful
// or the context is cancelled.
func (rc *ReconnectingConnection) reconnectLoop() {
	for {
		select {
		case <-rc.ctx.Done():
			return
		default:
		}

		attempt := int(atomic.AddInt32(&rc.attempt, 1))
		delay := BackoffDelay(attempt)

		log.Printf("reconnect: attempt %d, waiting %v before retry", attempt, delay)

		// Sleep with context cancellation support
		select {
		case <-rc.ctx.Done():
			return
		case <-time.After(delay):
		}

		// Get the handler from the old connection before creating a new one
		rc.connMu.RLock()
		oldConn := rc.conn
		rc.connMu.RUnlock()

		var handler func(protocol.Message)
		if oldConn != nil {
			oldConn.handlerMu.RLock()
			handler = oldConn.handler
			oldConn.handlerMu.RUnlock()
		}

		// Create a new connection and attempt to connect
		newConn := NewConnection(rc.serverURL, rc.token)
		if handler != nil {
			newConn.OnMessage(handler)
		}

		if err := newConn.Connect(rc.ctx); err != nil {
			log.Printf("reconnect: attempt %d failed: %v", attempt, err)
			// If the server returned 401, stop reconnecting immediately.
			// The token is invalid/expired and retrying won't help.
			if errors.Is(err, ErrAuthFailed) {
				log.Printf("reconnect: authentication failed, stopping reconnection loop")
				// Notify the caller via the AuthErr channel.
				select {
				case rc.AuthErr <- ErrAuthFailed:
				default:
				}
				return
			}
			continue
		}

		// Close the old connection (if any) and swap in the new one
		rc.connMu.Lock()
		if rc.conn != nil {
			_ = rc.conn.Close()
		}
		rc.conn = newConn
		rc.connMu.Unlock()

		// Reset attempt counter on success
		atomic.StoreInt32(&rc.attempt, 0)

		log.Printf("reconnect: successfully reconnected after %d attempts", attempt)

		// Call the onReconnect callback to re-send DaemonRegister
		if rc.onReconnect != nil {
			rc.onReconnect()
		}

		return
	}
}
