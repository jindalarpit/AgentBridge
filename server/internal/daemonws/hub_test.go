package daemonws

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/user/agentbridge/server/pkg/protocol"
)

func TestNewHub(t *testing.T) {
	hub := NewHub()
	if hub == nil {
		t.Fatal("NewHub returned nil")
	}
	if hub.connections == nil {
		t.Fatal("connections map not initialized")
	}
}

func TestIsOnline_NotConnected(t *testing.T) {
	hub := NewHub()
	if hub.IsOnline("nonexistent") {
		t.Error("expected IsOnline to return false for nonexistent daemon")
	}
}

func TestSendToDaemon_NotConnected(t *testing.T) {
	hub := NewHub()
	msg := protocol.Message{Type: protocol.TypeChatTask}
	err := hub.SendToDaemon("nonexistent", msg)
	if err == nil {
		t.Error("expected error when sending to nonexistent daemon")
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"valid bearer", "Bearer abc123", "abc123"},
		{"empty", "", ""},
		{"no prefix", "abc123", ""},
		{"wrong prefix", "Basic abc123", ""},
		{"bearer lowercase", "bearer abc123", ""},
		{"bearer with spaces in token", "Bearer abc 123", "abc 123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractBearerToken(tt.header)
			if got != tt.want {
				t.Errorf("ExtractBearerToken(%q) = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

func TestHandleWebSocket_Integration(t *testing.T) {
	hub := NewHub()

	// Track heartbeat calls.
	heartbeatCalled := make(chan string, 1)
	hub.SetHeartbeatHandler(func(daemonID string) {
		heartbeatCalled <- daemonID
	})

	// Create a test HTTP server that upgrades to WebSocket.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "test-daemon-1",
			UserID:   "user-1",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	// Connect a WebSocket client.
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	// Give the hub time to register the connection.
	time.Sleep(50 * time.Millisecond)

	// Verify daemon is online.
	if !hub.IsOnline("test-daemon-1") {
		t.Error("expected daemon to be online after connection")
	}

	// Send a heartbeat message from the daemon.
	heartbeatMsg := protocol.Message{
		Type:    protocol.TypeDaemonHeartbeat,
		Payload: nil,
	}
	data, _ := json.Marshal(heartbeatMsg)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send heartbeat: %v", err)
	}

	// Verify heartbeat handler was called.
	select {
	case id := <-heartbeatCalled:
		if id != "test-daemon-1" {
			t.Errorf("heartbeat handler called with %q, want %q", id, "test-daemon-1")
		}
	case <-time.After(2 * time.Second):
		t.Error("heartbeat handler not called within timeout")
	}

	// Verify we receive a heartbeat ack.
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, ackData, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read heartbeat ack: %v", err)
	}

	var ackMsg protocol.Message
	if err := json.Unmarshal(ackData, &ackMsg); err != nil {
		t.Fatalf("failed to unmarshal ack: %v", err)
	}
	if ackMsg.Type != protocol.TypeDaemonHeartbeatAck {
		t.Errorf("expected ack type %q, got %q", protocol.TypeDaemonHeartbeatAck, ackMsg.Type)
	}
}

func TestSendToDaemon_Connected(t *testing.T) {
	hub := NewHub()

	// Create a test HTTP server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "test-daemon-2",
			UserID:   "user-2",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	// Connect a WebSocket client.
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	// Give the hub time to register.
	time.Sleep(50 * time.Millisecond)

	// Send a message to the daemon.
	taskPayload, _ := json.Marshal(protocol.ChatTaskPayload{
		SessionID: "session-1",
		MessageID: "msg-1",
		Content:   "Hello, agent!",
		RuntimeID: "runtime-1",
	})
	msg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: taskPayload,
	}

	err = hub.SendToDaemon("test-daemon-2", msg)
	if err != nil {
		t.Fatalf("SendToDaemon failed: %v", err)
	}

	// Read the message on the client side.
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, received, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	var receivedMsg protocol.Message
	if err := json.Unmarshal(received, &receivedMsg); err != nil {
		t.Fatalf("failed to unmarshal received message: %v", err)
	}
	if receivedMsg.Type != protocol.TypeChatTask {
		t.Errorf("expected message type %q, got %q", protocol.TypeChatTask, receivedMsg.Type)
	}
}

func TestDaemonDisconnect(t *testing.T) {
	hub := NewHub()

	// Create a test HTTP server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "test-daemon-3",
			UserID:   "user-3",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	// Connect a WebSocket client.
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}

	// Give the hub time to register.
	time.Sleep(50 * time.Millisecond)

	if !hub.IsOnline("test-daemon-3") {
		t.Fatal("expected daemon to be online")
	}

	// Close the connection from the client side.
	ws.Close()

	// Give the hub time to detect the disconnect.
	time.Sleep(100 * time.Millisecond)

	if hub.IsOnline("test-daemon-3") {
		t.Error("expected daemon to be offline after disconnect")
	}
}

func TestSetHeartbeatHandler(t *testing.T) {
	hub := NewHub()

	called := false
	hub.SetHeartbeatHandler(func(daemonID string) {
		called = true
	})

	hub.mu.RLock()
	if hub.heartbeatHandler == nil {
		t.Error("heartbeat handler not set")
	}
	hub.mu.RUnlock()

	// Verify it's not called without a connection (just checking it's stored).
	if called {
		t.Error("handler should not be called without a heartbeat")
	}
}

func TestSetRegistrationHandler(t *testing.T) {
	hub := NewHub()

	hub.SetRegistrationHandler(func(identity DaemonIdentity, payload protocol.DaemonRegisterPayload) error {
		return nil
	})

	hub.mu.RLock()
	if hub.registrationHandler == nil {
		t.Error("registration handler not set")
	}
	hub.mu.RUnlock()
}

func TestRegistration_ValidPayload(t *testing.T) {
	hub := NewHub()

	// Track registration handler calls.
	regCalled := make(chan protocol.DaemonRegisterPayload, 1)
	hub.SetRegistrationHandler(func(identity DaemonIdentity, payload protocol.DaemonRegisterPayload) error {
		regCalled <- payload
		return nil
	})

	// Create a test HTTP server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "reg-daemon-1",
			UserID:   "user-1",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	// Connect a WebSocket client.
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send a valid registration message.
	regPayload := protocol.DaemonRegisterPayload{
		DaemonID: "reg-daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
	}
	payloadData, _ := json.Marshal(regPayload)
	regMsg := protocol.Message{
		Type:    protocol.TypeDaemonRegister,
		Payload: payloadData,
	}
	data, _ := json.Marshal(regMsg)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send registration: %v", err)
	}

	// Verify registration handler was called.
	select {
	case payload := <-regCalled:
		if payload.DaemonID != "reg-daemon-1" {
			t.Errorf("expected daemon_id %q, got %q", "reg-daemon-1", payload.DaemonID)
		}
		if payload.UserID != "user-1" {
			t.Errorf("expected user_id %q, got %q", "user-1", payload.UserID)
		}
		if len(payload.Runtimes) != 1 {
			t.Errorf("expected 1 runtime, got %d", len(payload.Runtimes))
		}
	case <-time.After(2 * time.Second):
		t.Error("registration handler not called within timeout")
	}

	// Verify we receive a register_ack.
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, ackData, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read register ack: %v", err)
	}

	var ackMsg protocol.Message
	if err := json.Unmarshal(ackData, &ackMsg); err != nil {
		t.Fatalf("failed to unmarshal ack: %v", err)
	}
	if ackMsg.Type != protocol.TypeDaemonRegisterAck {
		t.Errorf("expected ack type %q, got %q", protocol.TypeDaemonRegisterAck, ackMsg.Type)
	}
}

func TestRegistration_EmptyRuntimes(t *testing.T) {
	hub := NewHub()

	regCalled := make(chan struct{}, 1)
	hub.SetRegistrationHandler(func(identity DaemonIdentity, payload protocol.DaemonRegisterPayload) error {
		regCalled <- struct{}{}
		return nil
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "reg-daemon-empty",
			UserID:   "user-1",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send registration with empty (but non-nil) runtimes — should be valid.
	regPayload := protocol.DaemonRegisterPayload{
		DaemonID: "reg-daemon-empty",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{},
	}
	payloadData, _ := json.Marshal(regPayload)
	regMsg := protocol.Message{
		Type:    protocol.TypeDaemonRegister,
		Payload: payloadData,
	}
	data, _ := json.Marshal(regMsg)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send registration: %v", err)
	}

	// Verify handler was called (empty runtimes is valid).
	select {
	case <-regCalled:
		// OK
	case <-time.After(2 * time.Second):
		t.Error("registration handler not called for empty runtimes")
	}

	// Verify we receive a register_ack.
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, ackData, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read register ack: %v", err)
	}

	var ackMsg protocol.Message
	if err := json.Unmarshal(ackData, &ackMsg); err != nil {
		t.Fatalf("failed to unmarshal ack: %v", err)
	}
	if ackMsg.Type != protocol.TypeDaemonRegisterAck {
		t.Errorf("expected ack type %q, got %q", protocol.TypeDaemonRegisterAck, ackMsg.Type)
	}
}

func TestRegistration_MissingDaemonID(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "reg-daemon-no-id",
			UserID:   "user-1",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send registration with missing daemon_id.
	regPayload := protocol.DaemonRegisterPayload{
		DaemonID: "",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{},
	}
	payloadData, _ := json.Marshal(regPayload)
	regMsg := protocol.Message{
		Type:    protocol.TypeDaemonRegister,
		Payload: payloadData,
	}
	data, _ := json.Marshal(regMsg)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send registration: %v", err)
	}

	// Verify we receive a register_error.
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, errData, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read register error: %v", err)
	}

	var errMsg protocol.Message
	if err := json.Unmarshal(errData, &errMsg); err != nil {
		t.Fatalf("failed to unmarshal error: %v", err)
	}
	if errMsg.Type != protocol.TypeDaemonRegisterErr {
		t.Errorf("expected error type %q, got %q", protocol.TypeDaemonRegisterErr, errMsg.Type)
	}

	// Verify error payload contains the reason.
	var errPayload protocol.ChatErrorPayload
	if err := json.Unmarshal(errMsg.Payload, &errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if !strings.Contains(errPayload.Error, "daemon_id") {
		t.Errorf("expected error to mention daemon_id, got: %s", errPayload.Error)
	}

	// Verify connection is closed after error.
	time.Sleep(200 * time.Millisecond)
	if hub.IsOnline("reg-daemon-no-id") {
		t.Error("expected daemon to be disconnected after registration error")
	}
}

func TestRegistration_MissingUserID(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "reg-daemon-no-user",
			UserID:   "user-1",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send registration with missing user_id.
	regPayload := protocol.DaemonRegisterPayload{
		DaemonID: "reg-daemon-no-user",
		UserID:   "",
		Runtimes: []protocol.RuntimeInfo{},
	}
	payloadData, _ := json.Marshal(regPayload)
	regMsg := protocol.Message{
		Type:    protocol.TypeDaemonRegister,
		Payload: payloadData,
	}
	data, _ := json.Marshal(regMsg)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send registration: %v", err)
	}

	// Verify we receive a register_error.
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, errData, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read register error: %v", err)
	}

	var errMsg protocol.Message
	if err := json.Unmarshal(errData, &errMsg); err != nil {
		t.Fatalf("failed to unmarshal error: %v", err)
	}
	if errMsg.Type != protocol.TypeDaemonRegisterErr {
		t.Errorf("expected error type %q, got %q", protocol.TypeDaemonRegisterErr, errMsg.Type)
	}

	var errPayload protocol.ChatErrorPayload
	if err := json.Unmarshal(errMsg.Payload, &errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if !strings.Contains(errPayload.Error, "user_id") {
		t.Errorf("expected error to mention user_id, got: %s", errPayload.Error)
	}

	// Verify connection is closed after error.
	time.Sleep(200 * time.Millisecond)
	if hub.IsOnline("reg-daemon-no-user") {
		t.Error("expected daemon to be disconnected after registration error")
	}
}

func TestRegistration_NilRuntimes(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "reg-daemon-nil-rt",
			UserID:   "user-1",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send registration with null runtimes (JSON null for the field).
	// We manually construct JSON to ensure runtimes is null, not an empty array.
	rawPayload := json.RawMessage(`{"daemon_id":"reg-daemon-nil-rt","user_id":"user-1","runtimes":null}`)
	regMsg := protocol.Message{
		Type:    protocol.TypeDaemonRegister,
		Payload: rawPayload,
	}
	data, _ := json.Marshal(regMsg)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send registration: %v", err)
	}

	// Verify we receive a register_error.
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, errData, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read register error: %v", err)
	}

	var errMsg protocol.Message
	if err := json.Unmarshal(errData, &errMsg); err != nil {
		t.Fatalf("failed to unmarshal error: %v", err)
	}
	if errMsg.Type != protocol.TypeDaemonRegisterErr {
		t.Errorf("expected error type %q, got %q", protocol.TypeDaemonRegisterErr, errMsg.Type)
	}

	var errPayload protocol.ChatErrorPayload
	if err := json.Unmarshal(errMsg.Payload, &errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if !strings.Contains(errPayload.Error, "runtimes") {
		t.Errorf("expected error to mention runtimes, got: %s", errPayload.Error)
	}

	// Verify connection is closed after error.
	time.Sleep(200 * time.Millisecond)
	if hub.IsOnline("reg-daemon-nil-rt") {
		t.Error("expected daemon to be disconnected after registration error")
	}
}

func TestRegistration_HandlerReturnsError(t *testing.T) {
	hub := NewHub()

	// Set a registration handler that always returns an error.
	hub.SetRegistrationHandler(func(identity DaemonIdentity, payload protocol.DaemonRegisterPayload) error {
		return errors.New("user not found")
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "reg-daemon-err",
			UserID:   "user-1",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send a valid registration payload.
	regPayload := protocol.DaemonRegisterPayload{
		DaemonID: "reg-daemon-err",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
	}
	payloadData, _ := json.Marshal(regPayload)
	regMsg := protocol.Message{
		Type:    protocol.TypeDaemonRegister,
		Payload: payloadData,
	}
	data, _ := json.Marshal(regMsg)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send registration: %v", err)
	}

	// Verify we receive a register_error with the handler's error message.
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, errData, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read register error: %v", err)
	}

	var errMsg protocol.Message
	if err := json.Unmarshal(errData, &errMsg); err != nil {
		t.Fatalf("failed to unmarshal error: %v", err)
	}
	if errMsg.Type != protocol.TypeDaemonRegisterErr {
		t.Errorf("expected error type %q, got %q", protocol.TypeDaemonRegisterErr, errMsg.Type)
	}

	var errPayload protocol.ChatErrorPayload
	if err := json.Unmarshal(errMsg.Payload, &errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if errPayload.Error != "user not found" {
		t.Errorf("expected error %q, got %q", "user not found", errPayload.Error)
	}

	// Verify connection is closed after handler error.
	time.Sleep(200 * time.Millisecond)
	if hub.IsOnline("reg-daemon-err") {
		t.Error("expected daemon to be disconnected after handler error")
	}
}

func TestRegistration_WhitespaceOnlyDaemonID(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "reg-daemon-ws",
			UserID:   "user-1",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send registration with whitespace-only daemon_id.
	regPayload := protocol.DaemonRegisterPayload{
		DaemonID: "   ",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{},
	}
	payloadData, _ := json.Marshal(regPayload)
	regMsg := protocol.Message{
		Type:    protocol.TypeDaemonRegister,
		Payload: payloadData,
	}
	data, _ := json.Marshal(regMsg)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send registration: %v", err)
	}

	// Verify we receive a register_error.
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, errData, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read register error: %v", err)
	}

	var errMsg protocol.Message
	if err := json.Unmarshal(errData, &errMsg); err != nil {
		t.Fatalf("failed to unmarshal error: %v", err)
	}
	if errMsg.Type != protocol.TypeDaemonRegisterErr {
		t.Errorf("expected error type %q, got %q", protocol.TypeDaemonRegisterErr, errMsg.Type)
	}

	var errPayload protocol.ChatErrorPayload
	if err := json.Unmarshal(errMsg.Payload, &errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if !strings.Contains(errPayload.Error, "daemon_id") {
		t.Errorf("expected error to mention daemon_id, got: %s", errPayload.Error)
	}
}

func TestRegistration_NoHandler(t *testing.T) {
	hub := NewHub()
	// No registration handler set — should still accept valid registrations.

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "reg-daemon-nohandler",
			UserID:   "user-1",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send a valid registration.
	regPayload := protocol.DaemonRegisterPayload{
		DaemonID: "reg-daemon-nohandler",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{},
	}
	payloadData, _ := json.Marshal(regPayload)
	regMsg := protocol.Message{
		Type:    protocol.TypeDaemonRegister,
		Payload: payloadData,
	}
	data, _ := json.Marshal(regMsg)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send registration: %v", err)
	}

	// Verify we receive a register_ack even without a handler.
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, ackData, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read register ack: %v", err)
	}

	var ackMsg protocol.Message
	if err := json.Unmarshal(ackData, &ackMsg); err != nil {
		t.Fatalf("failed to unmarshal ack: %v", err)
	}
	if ackMsg.Type != protocol.TypeDaemonRegisterAck {
		t.Errorf("expected ack type %q, got %q", protocol.TypeDaemonRegisterAck, ackMsg.Type)
	}

	// Connection should remain open.
	if !hub.IsOnline("reg-daemon-nohandler") {
		t.Error("expected daemon to remain online after successful registration")
	}
}

func TestReplaceDuplicateConnection(t *testing.T) {
	hub := NewHub()

	// Create a test HTTP server.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := DaemonIdentity{
			DaemonID: "test-daemon-dup",
			UserID:   "user-dup",
		}
		hub.HandleWebSocket(w, r, identity)
	}))
	defer server.Close()

	// Connect first client.
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect first WebSocket: %v", err)
	}
	defer ws1.Close()

	time.Sleep(50 * time.Millisecond)

	// Connect second client with same daemon_id (should replace first).
	ws2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect second WebSocket: %v", err)
	}
	defer ws2.Close()

	time.Sleep(50 * time.Millisecond)

	// Daemon should still be online (second connection replaced first).
	if !hub.IsOnline("test-daemon-dup") {
		t.Error("expected daemon to be online after replacement")
	}

	// Send a message — should go to the second connection.
	msg := protocol.Message{
		Type:    protocol.TypeDaemonHeartbeatAck,
		Payload: nil,
	}
	err = hub.SendToDaemon("test-daemon-dup", msg)
	if err != nil {
		t.Errorf("SendToDaemon failed after replacement: %v", err)
	}
}
