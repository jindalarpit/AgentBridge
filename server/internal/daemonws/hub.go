package daemonws

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/user/agentbridge/server/pkg/protocol"
)

const (
	// writeWait is the time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// pongWait is the time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// pingPeriod sends pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// maxMessageSize is the maximum message size allowed from peer.
	maxMessageSize = 64 * 1024

	// sendBufferSize is the size of the send channel buffer per connection.
	sendBufferSize = 256
)

// DaemonIdentity holds the authenticated identity of a connected daemon.
type DaemonIdentity struct {
	DaemonID string
	UserID   string
}

// HeartbeatHandler is a callback invoked when a heartbeat is received from a daemon.
type HeartbeatHandler func(daemonID string)

// RegistrationHandler is a callback invoked when a valid daemon registration is received.
// The application layer can use this to persist daemon/runtime records.
// If it returns an error, the registration is rejected with that error message.
type RegistrationHandler func(identity DaemonIdentity, payload protocol.DaemonRegisterPayload) error

// MessageHandler is a callback invoked when a message is received from a daemon
// that is not handled internally (i.e., not heartbeat or registration).
type MessageHandler func(identity DaemonIdentity, msg protocol.Message)

// DisconnectHandler is a callback invoked when a daemon disconnects.
type DisconnectHandler func(identity DaemonIdentity)

// DaemonHub is the interface for managing daemon WebSocket connections.
type DaemonHub interface {
	HandleWebSocket(w http.ResponseWriter, r *http.Request, identity DaemonIdentity)
	SendToDaemon(daemonID string, msg protocol.Message) error
	IsOnline(daemonID string) bool
	SetHeartbeatHandler(fn HeartbeatHandler)
	SetRegistrationHandler(fn RegistrationHandler)
	SetMessageHandler(fn MessageHandler)
	SetDisconnectHandler(fn DisconnectHandler)
}

// daemonConn wraps a single daemon WebSocket connection.
type daemonConn struct {
	hub      *Hub
	conn     *websocket.Conn
	identity DaemonIdentity
	send     chan []byte
}

// Hub implements DaemonHub, managing all active daemon connections.
type Hub struct {
	mu          sync.RWMutex
	connections map[string]*daemonConn // keyed by daemon_id

	heartbeatHandler    HeartbeatHandler
	registrationHandler RegistrationHandler
	messageHandler      MessageHandler
	disconnectHandler   DisconnectHandler

	upgrader websocket.Upgrader
}

// NewHub creates a new DaemonHub instance.
func NewHub() *Hub {
	return &Hub{
		connections: make(map[string]*daemonConn),
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true // Allow all origins for daemon connections
			},
		},
	}
}

// HandleWebSocket upgrades the HTTP connection to a WebSocket and starts
// read/write pumps for the daemon. The identity should be pre-authenticated
// by the caller (via Bearer token in Authorization header).
func (h *Hub) HandleWebSocket(w http.ResponseWriter, r *http.Request, identity DaemonIdentity) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("daemonws: upgrade error for daemon %s: %v", identity.DaemonID, err)
		return
	}

	dc := &daemonConn{
		hub:      h,
		conn:     conn,
		identity: identity,
		send:     make(chan []byte, sendBufferSize),
	}

	h.register(dc)

	// Start the write pump in a separate goroutine.
	go dc.writePump()
	// Run the read pump in a separate goroutine.
	go dc.readPump()
}

// SendToDaemon sends a protocol message to the daemon identified by daemonID.
// Returns an error if the daemon is not connected.
func (h *Hub) SendToDaemon(daemonID string, msg protocol.Message) error {
	h.mu.RLock()
	dc, ok := h.connections[daemonID]
	h.mu.RUnlock()

	if !ok {
		return errors.New("daemon not connected: " + daemonID)
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	select {
	case dc.send <- data:
		return nil
	default:
		// Send buffer full — disconnect the daemon.
		h.unregister(daemonID, dc)
		return errors.New("daemon send buffer full: " + daemonID)
	}
}

// IsOnline returns true if the daemon with the given ID has an active connection.
func (h *Hub) IsOnline(daemonID string) bool {
	h.mu.RLock()
	_, ok := h.connections[daemonID]
	h.mu.RUnlock()
	return ok
}

// SetHeartbeatHandler sets the callback function invoked when a heartbeat
// message is received from any connected daemon.
func (h *Hub) SetHeartbeatHandler(fn HeartbeatHandler) {
	h.mu.Lock()
	h.heartbeatHandler = fn
	h.mu.Unlock()
}

// SetRegistrationHandler sets the callback function invoked when a valid
// daemon registration message is received. If the handler returns an error,
// the registration is rejected and the connection is closed.
func (h *Hub) SetRegistrationHandler(fn RegistrationHandler) {
	h.mu.Lock()
	h.registrationHandler = fn
	h.mu.Unlock()
}

// SetMessageHandler sets the callback function invoked when a message is
// received from a daemon that is not handled internally (not heartbeat or registration).
func (h *Hub) SetMessageHandler(fn MessageHandler) {
	h.mu.Lock()
	h.messageHandler = fn
	h.mu.Unlock()
}

// SetDisconnectHandler sets the callback function invoked when a daemon disconnects.
func (h *Hub) SetDisconnectHandler(fn DisconnectHandler) {
	h.mu.Lock()
	h.disconnectHandler = fn
	h.mu.Unlock()
}

// register adds a daemon connection to the hub. If a connection with the same
// daemon_id already exists, the old connection is closed and replaced.
func (h *Hub) register(dc *daemonConn) {
	h.mu.Lock()
	if existing, ok := h.connections[dc.identity.DaemonID]; ok {
		close(existing.send)
		existing.conn.Close()
	}
	h.connections[dc.identity.DaemonID] = dc
	h.mu.Unlock()

	log.Printf("daemonws: daemon registered: %s (user: %s)", dc.identity.DaemonID, dc.identity.UserID)
}

// unregister removes a daemon connection from the hub and closes its resources.
// It only removes the connection if it matches the current one in the map
// (to avoid removing a replacement connection).
func (h *Hub) unregister(daemonID string, dc *daemonConn) {
	h.mu.Lock()
	current, ok := h.connections[daemonID]
	if ok && current == dc {
		delete(h.connections, daemonID)
		close(dc.send)
		disconnectHandler := h.disconnectHandler
		h.mu.Unlock()
		dc.conn.Close()
		log.Printf("daemonws: daemon disconnected: %s", daemonID)

		// Invoke disconnect handler outside the lock.
		if disconnectHandler != nil {
			disconnectHandler(dc.identity)
		}
	} else {
		h.mu.Unlock()
	}
}

// readPump reads messages from the WebSocket connection and dispatches them.
func (dc *daemonConn) readPump() {
	defer func() {
		dc.hub.unregister(dc.identity.DaemonID, dc)
	}()

	dc.conn.SetReadLimit(maxMessageSize)
	dc.conn.SetReadDeadline(time.Now().Add(pongWait))
	dc.conn.SetPongHandler(func(string) error {
		dc.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, message, err := dc.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("daemonws: read error from daemon %s: %v", dc.identity.DaemonID, err)
			}
			return
		}

		dc.handleMessage(message)
	}
}

// writePump writes messages from the send channel to the WebSocket connection.
func (dc *daemonConn) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		dc.conn.Close()
	}()

	for {
		select {
		case message, ok := <-dc.send:
			dc.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				dc.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := dc.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			dc.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := dc.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage dispatches an incoming message based on its type.
func (dc *daemonConn) handleMessage(data []byte) {
	var msg protocol.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("daemonws: malformed message from daemon %s: %v", dc.identity.DaemonID, err)
		return
	}

	switch msg.Type {
	case protocol.TypeDaemonHeartbeat:
		dc.handleHeartbeat()

	case protocol.TypeDaemonRegister:
		dc.handleRegistration(msg.Payload)

	default:
		// For other message types (chat:stream, chat:done, chat:error, etc.),
		// dispatch to the message handler if set.
		dc.hub.mu.RLock()
		handler := dc.hub.messageHandler
		dc.hub.mu.RUnlock()

		if handler != nil {
			handler(dc.identity, msg)
		} else {
			log.Printf("daemonws: unhandled message type %q from daemon %s", msg.Type, dc.identity.DaemonID)
		}
	}
}

// handleRegistration processes a daemon:register message, validates the payload,
// and either acknowledges or rejects the registration.
func (dc *daemonConn) handleRegistration(payload json.RawMessage) {
	var reg protocol.DaemonRegisterPayload
	if err := json.Unmarshal(payload, &reg); err != nil {
		dc.sendRegistrationError("invalid registration payload: " + err.Error())
		dc.closeConnection()
		return
	}

	// Validate required fields.
	if strings.TrimSpace(reg.DaemonID) == "" {
		dc.sendRegistrationError("daemon_id is required")
		dc.closeConnection()
		return
	}
	if strings.TrimSpace(reg.UserID) == "" {
		dc.sendRegistrationError("user_id is required")
		dc.closeConnection()
		return
	}
	if reg.Runtimes == nil {
		dc.sendRegistrationError("runtimes is required")
		dc.closeConnection()
		return
	}

	// Invoke the registration handler if set.
	dc.hub.mu.RLock()
	handler := dc.hub.registrationHandler
	dc.hub.mu.RUnlock()

	if handler != nil {
		if err := handler(dc.identity, reg); err != nil {
			dc.sendRegistrationError(err.Error())
			dc.closeConnection()
			return
		}
	}

	// Update the connection identity with the actual daemon ID from the registration.
	// Re-key the connection in the hub's map so lookups by daemon_id work.
	oldDaemonID := dc.identity.DaemonID
	dc.identity.DaemonID = reg.DaemonID
	dc.identity.UserID = reg.UserID

	if oldDaemonID != reg.DaemonID {
		dc.hub.mu.Lock()
		// Remove old key (might be empty string from initial connection).
		if oldDaemonID != reg.DaemonID {
			delete(dc.hub.connections, oldDaemonID)
		}
		dc.hub.connections[reg.DaemonID] = dc
		dc.hub.mu.Unlock()
	}

	// Send registration acknowledgment.
	dc.sendRegistrationAck()
	log.Printf("daemonws: registration accepted for daemon %s (user: %s, runtimes: %d)",
		reg.DaemonID, reg.UserID, len(reg.Runtimes))
}

// handleHeartbeat processes a heartbeat message and sends an acknowledgment.
func (dc *daemonConn) handleHeartbeat() {
	// Invoke the heartbeat handler if set.
	dc.hub.mu.RLock()
	handler := dc.hub.heartbeatHandler
	dc.hub.mu.RUnlock()

	if handler != nil {
		handler(dc.identity.DaemonID)
	}

	// Send heartbeat acknowledgment.
	ack := protocol.Message{
		Type:    protocol.TypeDaemonHeartbeatAck,
		Payload: nil,
	}
	data, err := json.Marshal(ack)
	if err != nil {
		log.Printf("daemonws: failed to marshal heartbeat ack: %v", err)
		return
	}

	select {
	case dc.send <- data:
	default:
		log.Printf("daemonws: send buffer full for daemon %s during heartbeat ack", dc.identity.DaemonID)
	}
}

// sendRegistrationError sends a daemon:register_error message to the daemon.
func (dc *daemonConn) sendRegistrationError(reason string) {
	errPayload, _ := json.Marshal(protocol.ChatErrorPayload{
		Error: reason,
		Code:  protocol.ErrCodeValidation,
	})
	msg := protocol.Message{
		Type:    protocol.TypeDaemonRegisterErr,
		Payload: errPayload,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("daemonws: failed to marshal register error: %v", err)
		return
	}

	select {
	case dc.send <- data:
	default:
		log.Printf("daemonws: send buffer full for daemon %s during register error", dc.identity.DaemonID)
	}
}

// sendRegistrationAck sends a daemon:register_ack message to the daemon.
func (dc *daemonConn) sendRegistrationAck() {
	msg := protocol.Message{
		Type:    protocol.TypeDaemonRegisterAck,
		Payload: nil,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		log.Printf("daemonws: failed to marshal register ack: %v", err)
		return
	}

	select {
	case dc.send <- data:
	default:
		log.Printf("daemonws: send buffer full for daemon %s during register ack", dc.identity.DaemonID)
	}
}

// closeConnection closes the daemon connection after sending any pending messages.
// It gives a brief delay to allow the error message to be flushed before closing.
func (dc *daemonConn) closeConnection() {
	// Use a goroutine to close after a short delay so the error message can be sent.
	go func() {
		time.Sleep(100 * time.Millisecond)
		dc.hub.unregister(dc.identity.DaemonID, dc)
	}()
}

// ExtractBearerToken extracts the token from an Authorization header value
// of the form "Bearer <token>".
func ExtractBearerToken(authHeader string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return ""
	}
	return strings.TrimPrefix(authHeader, prefix)
}

// ValidateRegistration checks that a DaemonRegisterPayload has all required
// fields populated. It returns a non-nil error with a descriptive message if
// any required field is missing or invalid.
//
// Required fields:
//   - DaemonID: must be non-empty (after trimming whitespace)
//   - UserID: must be non-empty (after trimming whitespace)
//   - Runtimes: must not be nil (an empty slice is valid)
func ValidateRegistration(payload protocol.DaemonRegisterPayload) error {
	var errs []string

	if strings.TrimSpace(payload.DaemonID) == "" {
		errs = append(errs, "daemon_id is required")
	}
	if strings.TrimSpace(payload.UserID) == "" {
		errs = append(errs, "user_id is required")
	}
	if payload.Runtimes == nil {
		errs = append(errs, "runtimes is required")
	}

	if len(errs) > 0 {
		return errors.New("registration validation failed: " + strings.Join(errs, "; "))
	}
	return nil
}
