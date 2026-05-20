// Package connection manages the WebSocket connection to the server with reconnection support.
package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/user/agentbridge/daemon/pkg/protocol"
)

// ErrAuthFailed is returned when the server responds with HTTP 401 during
// WebSocket dial, indicating the authentication token is invalid or expired.
var ErrAuthFailed = errors.New("authentication failed: token is invalid or expired")

// sendBufferSize is the capacity of the outgoing message channel.
const sendBufferSize = 256

// ServerConnection manages the WebSocket link to the server.
type ServerConnection interface {
	Connect(ctx context.Context) error
	Send(msg protocol.Message) error
	OnMessage(handler func(protocol.Message))
	Close() error
	IsConnected() bool
}

// Compile-time check that *Connection implements ServerConnection.
var _ ServerConnection = (*Connection)(nil)

// Connection implements ServerConnection using gorilla/websocket.
type Connection struct {
	serverURL string
	token     string

	conn      *websocket.Conn
	connMu    sync.Mutex
	connected atomic.Bool

	handler   func(protocol.Message)
	handlerMu sync.RWMutex

	sendCh chan protocol.Message
	done   chan struct{}
	once   sync.Once // ensures done is closed only once
}

// NewConnection creates a new Connection instance configured to connect to the
// given server URL with the provided authentication token.
func NewConnection(serverURL, token string) *Connection {
	return &Connection{
		serverURL: serverURL,
		token:     token,
		sendCh:    make(chan protocol.Message, sendBufferSize),
		done:      make(chan struct{}),
	}
}

// Connect establishes the WebSocket connection to the server using Bearer token
// authentication and starts the read and write goroutines.
// Returns ErrAuthFailed if the server responds with HTTP 401.
func (c *Connection) Connect(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.token)

	dialer := websocket.DefaultDialer

	conn, resp, err := dialer.DialContext(ctx, c.serverURL, header)
	if err != nil {
		// Check if the HTTP response indicates an authentication failure.
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			return ErrAuthFailed
		}
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return fmt.Errorf("websocket dial: %w", err)
	}
	if resp != nil && resp.Body != nil {
		resp.Body.Close()
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	c.connected.Store(true)
	log.Printf("connection: Connect() completed, connected=%v", c.connected.Load())

	go c.readLoop()
	go c.writeLoop()

	return nil
}

// Send marshals the message to JSON and sends it through the send channel.
// Returns an error if the connection is closed or the send buffer is full.
func (c *Connection) Send(msg protocol.Message) error {
	select {
	case c.sendCh <- msg:
		return nil
	case <-c.done:
		return errors.New("connection is closed")
	default:
		return errors.New("send buffer full")
	}
}

// OnMessage registers a handler function that will be called for each incoming
// message from the server.
func (c *Connection) OnMessage(handler func(protocol.Message)) {
	c.handlerMu.Lock()
	c.handler = handler
	c.handlerMu.Unlock()
}

// Close gracefully shuts down the WebSocket connection and stops the read/write
// goroutines.
func (c *Connection) Close() error {
	c.once.Do(func() {
		close(c.done)
	})

	c.connected.Store(false)

	c.connMu.Lock()
	conn := c.conn
	c.conn = nil
	c.connMu.Unlock()

	if conn != nil {
		// Send a close message to the server for graceful shutdown.
		_ = conn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		return conn.Close()
	}
	return nil
}

// IsConnected returns whether the connection is currently active.
func (c *Connection) IsConnected() bool {
	return c.connected.Load()
}

// readLoop continuously reads messages from the WebSocket connection and
// dispatches them to the registered handler.
func (c *Connection) readLoop() {
	defer func() {
		log.Printf("connection: readLoop exiting")
		c.handleDisconnect()
	}()

	for {
		select {
		case <-c.done:
			log.Printf("connection: readLoop: done channel closed")
			return
		default:
		}

		c.connMu.Lock()
		conn := c.conn
		c.connMu.Unlock()

		if conn == nil {
			log.Printf("connection: readLoop: conn is nil")
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			// Connection closed or error — exit the read loop.
			log.Printf("connection: readLoop error: %v", err)
			return
		}

		var msg protocol.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			// Skip malformed messages.
			log.Printf("connection: malformed message: %v (data: %s)", err, string(data[:min(len(data), 100)]))
			continue
		}

		c.handlerMu.RLock()
		handler := c.handler
		c.handlerMu.RUnlock()

		if handler != nil {
			handler(msg)
		}
	}
}

// writeLoop reads messages from the send channel and writes them to the
// WebSocket connection as JSON.
func (c *Connection) writeLoop() {
	defer func() {
		log.Printf("connection: writeLoop exiting")
		c.handleDisconnect()
	}()

	for {
		select {
		case <-c.done:
			log.Printf("connection: writeLoop: done channel closed")
			return
		case msg, ok := <-c.sendCh:
			if !ok {
				log.Printf("connection: writeLoop: sendCh closed")
				return
			}

			data, err := json.Marshal(msg)
			if err != nil {
				// Skip messages that can't be marshaled.
				continue
			}

			c.connMu.Lock()
			conn := c.conn
			c.connMu.Unlock()

			if conn == nil {
				log.Printf("connection: writeLoop: conn is nil")
				return
			}

			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				// Write failed — connection is likely broken.
				log.Printf("connection: writeLoop: write error: %v", err)
				return
			}
		}
	}
}

// handleDisconnect updates the connection state when the connection is lost.
func (c *Connection) handleDisconnect() {
	wasConnected := c.connected.Swap(false)
	if wasConnected {
		log.Printf("connection: handleDisconnect called (was connected)")
	}
}
