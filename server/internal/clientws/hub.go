package clientws

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/user/agentbridge/server/pkg/protocol"
)

const (
	// writeWait is the time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// pingPeriod sends pings to peer with this period (every 30 seconds per requirement 10.3).
	pingPeriod = 30 * time.Second

	// pongWait is the time allowed to read the next pong message from the peer.
	// Set to pingPeriod + 10 seconds: the client must respond to a ping within 10 seconds
	// (requirement 10.4), and the read deadline is reset on each pong receipt.
	pongWait = pingPeriod + 10*time.Second

	// maxMessageSize is the maximum message size allowed from peer.
	maxMessageSize = 64 * 1024

	// sendBufferSize is the size of the send channel buffer per connection.
	sendBufferSize = 256
)

// MessageHandler is a callback invoked when a message is received from a client.
type MessageHandler func(userID string, msg protocol.Message)

// ClientHub is the interface for managing browser WebSocket connections.
type ClientHub interface {
	HandleWebSocket(w http.ResponseWriter, r *http.Request, userID string)
	SendToUser(userID string, msg protocol.Message)
	BroadcastToUser(userID string, msg protocol.Message)
	ConnectionCount(userID string) int
}

// clientConn wraps a single client WebSocket connection.
type clientConn struct {
	hub         *Hub
	conn        *websocket.Conn
	userID      string
	send        chan []byte
	rateLimiter *malformedRateLimiter
	closeOnce   sync.Once
}

// Hub implements ClientHub, managing all active client connections.
// It supports multiple connections per user (multiple browser tabs).
type Hub struct {
	mu          sync.RWMutex
	connections map[string][]*clientConn // keyed by userID → list of connections

	messageHandler MessageHandler

	buffer *MessageBuffer

	upgrader websocket.Upgrader
}

// NewHub creates a new ClientHub instance.
func NewHub() *Hub {
	return &Hub{
		connections: make(map[string][]*clientConn),
		buffer:      NewMessageBuffer(),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins; CORS is handled at the HTTP layer
			},
		},
	}
}

// SetMessageHandler sets the callback function invoked when a message is
// received from any connected client.
func (h *Hub) SetMessageHandler(fn MessageHandler) {
	h.mu.Lock()
	h.messageHandler = fn
	h.mu.Unlock()
}

// HandleWebSocket upgrades the HTTP connection to a WebSocket and starts
// read/write pumps for the client. The userID should be pre-authenticated
// by the caller (via token query param validation).
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request, userID string) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("clientws: upgrade error for user %s: %v", userID, err)
		return
	}

	cc := &clientConn{
		hub:         h,
		conn:        conn,
		userID:      userID,
		send:        make(chan []byte, sendBufferSize),
		rateLimiter: newMalformedRateLimiter(malformedWindowDuration, malformedMaxCount),
	}

	h.register(cc)

	// Start the write pump in a separate goroutine.
	go cc.writePump()
	// Run the read pump in a separate goroutine.
	go cc.readPump()
}

// SendToUser sends a protocol message to the first available connection for
// the given user. If the user has no active connections, the message is buffered
// for delivery on reconnection (up to 100 messages within a 5-minute window).
func (h *Hub) SendToUser(userID string, msg protocol.Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("clientws: failed to marshal message for user %s: %v", userID, err)
		return
	}

	h.mu.RLock()
	conns := h.connections[userID]
	h.mu.RUnlock()

	if len(conns) == 0 {
		// No active connections — buffer the message for later delivery.
		h.buffer.Add(userID, data)
		return
	}

	// Send to the first connection that can accept the message.
	for _, cc := range conns {
		select {
		case cc.send <- data:
			return
		default:
			// This connection's buffer is full, try the next one.
			continue
		}
	}

	// All connections have full buffers — log a warning.
	log.Printf("clientws: all send buffers full for user %s, message dropped", userID)
}

// BroadcastToUser sends a protocol message to ALL active connections for the
// given user (all open tabs). If the user has no active connections, the message
// is buffered for delivery on reconnection. Connections with full send buffers
// are skipped.
func (h *Hub) BroadcastToUser(userID string, msg protocol.Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("clientws: failed to marshal broadcast for user %s: %v", userID, err)
		return
	}

	h.mu.RLock()
	conns := h.connections[userID]
	h.mu.RUnlock()

	if len(conns) == 0 {
		// No active connections — buffer the message for later delivery.
		h.buffer.Add(userID, data)
		return
	}

	for _, cc := range conns {
		select {
		case cc.send <- data:
		default:
			log.Printf("clientws: send buffer full for user %s connection, skipping", userID)
		}
	}
}

// ConnectionCount returns the number of active connections for the given user.
func (h *Hub) ConnectionCount(userID string) int {
	h.mu.RLock()
	count := len(h.connections[userID])
	h.mu.RUnlock()
	return count
}

// register adds a client connection to the hub's per-user connection list.
// If the user has buffered messages from a prior disconnection, they are
// delivered in chronological order to the new connection.
func (h *Hub) register(cc *clientConn) {
	h.mu.Lock()
	h.connections[cc.userID] = append(h.connections[cc.userID], cc)
	h.mu.Unlock()

	log.Printf("clientws: client connected: user %s (total connections: %d)",
		cc.userID, h.ConnectionCount(cc.userID))

	// Deliver any buffered messages from the disconnection period.
	buffered := h.buffer.Drain(cc.userID)
	for _, data := range buffered {
		select {
		case cc.send <- data:
		default:
			log.Printf("clientws: send buffer full while delivering buffered messages for user %s", cc.userID)
			return
		}
	}
}

// unregister removes a specific client connection from the hub.
// It is safe to call multiple times; only the first call has any effect.
func (h *Hub) unregister(cc *clientConn) {
	cc.closeOnce.Do(func() {
		h.mu.Lock()
		conns := h.connections[cc.userID]
		for i, c := range conns {
			if c == cc {
				// Remove this connection from the slice.
				h.connections[cc.userID] = append(conns[:i], conns[i+1:]...)
				break
			}
		}
		// Clean up the map entry if no connections remain.
		if len(h.connections[cc.userID]) == 0 {
			delete(h.connections, cc.userID)
		}
		h.mu.Unlock()

		close(cc.send)
		cc.conn.Close()

		log.Printf("clientws: client disconnected: user %s (remaining connections: %d)",
			cc.userID, h.ConnectionCount(cc.userID))
	})
}

// readPump reads messages from the WebSocket connection and dispatches them.
func (cc *clientConn) readPump() {
	defer func() {
		cc.hub.unregister(cc)
	}()

	cc.conn.SetReadLimit(maxMessageSize)
	cc.conn.SetReadDeadline(time.Now().Add(pongWait))
	cc.conn.SetPongHandler(func(string) error {
		cc.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := cc.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("clientws: read error from user %s: %v", cc.userID, err)
			}
			return
		}

		cc.handleMessage(message)
	}
}

// writePump writes messages from the send channel to the WebSocket connection.
func (cc *clientConn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		cc.conn.Close()
	}()

	for {
		select {
		case message, ok := <-cc.send:
			cc.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				cc.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := cc.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Drain queued messages into the current write.
			n := len(cc.send)
			for i := 0; i < n; i++ {
				w.Write([]byte("\n"))
				w.Write(<-cc.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			cc.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := cc.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage dispatches an incoming message based on its type.
func (cc *clientConn) handleMessage(data []byte) {
	var msg protocol.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("clientws: malformed message from user %s: %v", cc.userID, err)
		if cc.rateLimiter.Record(time.Now()) {
			log.Printf("clientws: closing connection for user %s: exceeded malformed message rate limit (%d in %v)",
				cc.userID, malformedMaxCount+1, malformedWindowDuration)
			cc.conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "too many malformed messages"))
			// Close the underlying connection. The readPump's next ReadMessage()
			// will return an error, triggering the deferred unregister.
			cc.conn.Close()
		}
		return
	}

	switch msg.Type {
	case protocol.TypeConnectionPong:
		// Client responded to our ping — nothing to do (read deadline already extended by pong handler).
		return

	default:
		// Dispatch to the message handler if set.
		cc.hub.mu.RLock()
		handler := cc.hub.messageHandler
		cc.hub.mu.RUnlock()

		if handler != nil {
			handler(cc.userID, msg)
		} else {
			log.Printf("clientws: unhandled message type %q from user %s", msg.Type, cc.userID)
		}
	}
}
