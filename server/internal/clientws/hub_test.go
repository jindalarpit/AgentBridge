package clientws

import (
	"encoding/json"
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

func TestConnectionCount_NoConnections(t *testing.T) {
	hub := NewHub()
	if hub.ConnectionCount("nonexistent") != 0 {
		t.Error("expected ConnectionCount to return 0 for nonexistent user")
	}
}

func TestHandleWebSocket_SingleConnection(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-1")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	// Give the hub time to register the connection.
	time.Sleep(50 * time.Millisecond)

	if hub.ConnectionCount("user-1") != 1 {
		t.Errorf("expected 1 connection, got %d", hub.ConnectionCount("user-1"))
	}
}

func TestHandleWebSocket_MultipleTabs(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-tabs")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect three tabs for the same user.
	ws1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect first WebSocket: %v", err)
	}
	defer ws1.Close()

	ws2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect second WebSocket: %v", err)
	}
	defer ws2.Close()

	ws3, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect third WebSocket: %v", err)
	}
	defer ws3.Close()

	time.Sleep(50 * time.Millisecond)

	if hub.ConnectionCount("user-tabs") != 3 {
		t.Errorf("expected 3 connections, got %d", hub.ConnectionCount("user-tabs"))
	}
}

func TestSendToUser_NoConnections(t *testing.T) {
	hub := NewHub()

	// SendToUser should not panic when user has no connections.
	msg := protocol.Message{Type: protocol.TypeChatMsg}
	hub.SendToUser("nonexistent", msg)
	// No error expected — message is silently dropped.
}

func TestSendToUser_SingleConnection(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-send")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send a message to the user.
	payload, _ := json.Marshal(protocol.ChatStreamPayload{
		SessionID: "session-1",
		Seq:       1,
		Content:   "Hello!",
	})
	msg := protocol.Message{
		Type:    protocol.TypeChatStream,
		Payload: payload,
	}
	hub.SendToUser("user-send", msg)

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
	if receivedMsg.Type != protocol.TypeChatStream {
		t.Errorf("expected message type %q, got %q", protocol.TypeChatStream, receivedMsg.Type)
	}
}

func TestBroadcastToUser_MultipleTabs(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-broadcast")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect two tabs.
	ws1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect first WebSocket: %v", err)
	}
	defer ws1.Close()

	ws2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect second WebSocket: %v", err)
	}
	defer ws2.Close()

	time.Sleep(50 * time.Millisecond)

	// Broadcast a message to the user.
	payload, _ := json.Marshal(protocol.ChatDonePayload{
		SessionID: "session-1",
		MessageID: "msg-1",
		Content:   "Done!",
		ElapsedMs: 500,
	})
	msg := protocol.Message{
		Type:    protocol.TypeChatDone,
		Payload: payload,
	}
	hub.BroadcastToUser("user-broadcast", msg)

	// Both tabs should receive the message.
	ws1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, received1, err := ws1.ReadMessage()
	if err != nil {
		t.Fatalf("tab 1 failed to read message: %v", err)
	}

	ws2.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, received2, err := ws2.ReadMessage()
	if err != nil {
		t.Fatalf("tab 2 failed to read message: %v", err)
	}

	var msg1, msg2 protocol.Message
	json.Unmarshal(received1, &msg1)
	json.Unmarshal(received2, &msg2)

	if msg1.Type != protocol.TypeChatDone {
		t.Errorf("tab 1: expected type %q, got %q", protocol.TypeChatDone, msg1.Type)
	}
	if msg2.Type != protocol.TypeChatDone {
		t.Errorf("tab 2: expected type %q, got %q", protocol.TypeChatDone, msg2.Type)
	}
}

func TestBroadcastToUser_NoConnections(t *testing.T) {
	hub := NewHub()

	// BroadcastToUser should not panic when user has no connections.
	msg := protocol.Message{Type: protocol.TypeSessionUpdated}
	hub.BroadcastToUser("nonexistent", msg)
	// No error expected — message is silently dropped.
}

func TestClientDisconnect(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-disconnect")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if hub.ConnectionCount("user-disconnect") != 1 {
		t.Fatal("expected 1 connection before disconnect")
	}

	// Close the connection from the client side.
	ws.Close()

	// Give the hub time to detect the disconnect.
	time.Sleep(100 * time.Millisecond)

	if hub.ConnectionCount("user-disconnect") != 0 {
		t.Errorf("expected 0 connections after disconnect, got %d", hub.ConnectionCount("user-disconnect"))
	}
}

func TestClientDisconnect_OneOfMultipleTabs(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-partial-dc")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	ws1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect first WebSocket: %v", err)
	}
	defer ws1.Close()

	ws2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect second WebSocket: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if hub.ConnectionCount("user-partial-dc") != 2 {
		t.Fatalf("expected 2 connections, got %d", hub.ConnectionCount("user-partial-dc"))
	}

	// Close only the second connection.
	ws2.Close()

	time.Sleep(100 * time.Millisecond)

	if hub.ConnectionCount("user-partial-dc") != 1 {
		t.Errorf("expected 1 connection after partial disconnect, got %d", hub.ConnectionCount("user-partial-dc"))
	}

	// The remaining connection should still work.
	payload, _ := json.Marshal(protocol.ChatStreamPayload{
		SessionID: "s1",
		Seq:       1,
		Content:   "still alive",
	})
	msg := protocol.Message{
		Type:    protocol.TypeChatStream,
		Payload: payload,
	}
	hub.SendToUser("user-partial-dc", msg)

	ws1.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, received, err := ws1.ReadMessage()
	if err != nil {
		t.Fatalf("remaining tab failed to read message: %v", err)
	}

	var receivedMsg protocol.Message
	if err := json.Unmarshal(received, &receivedMsg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if receivedMsg.Type != protocol.TypeChatStream {
		t.Errorf("expected type %q, got %q", protocol.TypeChatStream, receivedMsg.Type)
	}
}

func TestMessageHandler(t *testing.T) {
	hub := NewHub()

	// Track messages received from clients.
	received := make(chan protocol.Message, 1)
	receivedUserID := make(chan string, 1)
	hub.SetMessageHandler(func(userID string, msg protocol.Message) {
		receivedUserID <- userID
		received <- msg
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-handler")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send a chat:send message from the client.
	chatPayload, _ := json.Marshal(map[string]string{
		"session_id": "session-1",
		"content":    "Hello, agent!",
	})
	clientMsg := protocol.Message{
		Type:    protocol.TypeChatSend,
		Payload: chatPayload,
	}
	data, _ := json.Marshal(clientMsg)
	if err := ws.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("failed to send message: %v", err)
	}

	// Verify the message handler was called.
	select {
	case uid := <-receivedUserID:
		if uid != "user-handler" {
			t.Errorf("expected userID %q, got %q", "user-handler", uid)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message handler not called within timeout")
	}

	select {
	case msg := <-received:
		if msg.Type != protocol.TypeChatSend {
			t.Errorf("expected message type %q, got %q", protocol.TypeChatSend, msg.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("message not received within timeout")
	}
}

func TestPingPongConstants(t *testing.T) {
	// Verify that the ping/pong constants match requirements 10.3 and 10.4:
	// - Ping sent every 30 seconds
	// - Pong expected within 10 seconds (pongWait = pingPeriod + 10s = 40s)
	if pingPeriod != 30*time.Second {
		t.Errorf("pingPeriod should be 30s, got %v", pingPeriod)
	}
	if pongWait != 40*time.Second {
		t.Errorf("pongWait should be 40s (pingPeriod + 10s timeout), got %v", pongWait)
	}
	if pingPeriod >= pongWait {
		t.Errorf("pingPeriod (%v) must be less than pongWait (%v)", pingPeriod, pongWait)
	}
}

func TestPingPong_ConnectionClosedOnTimeout(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-ping-timeout")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Use a custom dialer that does NOT respond to pings (disables the default pong handler).
	dialer := websocket.Dialer{}
	ws, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	// Disable the automatic pong response by setting a no-op ping handler.
	ws.SetPingHandler(func(appData string) error {
		// Intentionally do NOT send a pong back.
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	if hub.ConnectionCount("user-ping-timeout") != 1 {
		t.Fatalf("expected 1 connection, got %d", hub.ConnectionCount("user-ping-timeout"))
	}

	// The server sends a ping every 30s and expects a pong within 10s.
	// The read deadline on the server side is set to pongWait (40s) from the last pong.
	// Since we never send a pong, the server should close the connection after pongWait.
	//
	// For testing, we verify the mechanism works by reading from the connection
	// and confirming we receive a ping frame (the server's writePump sends it).
	// Then we wait for the server to close the connection due to pong timeout.

	// Start a goroutine to continuously read (required for the ping handler to fire).
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, _, err := ws.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	// Wait for the connection to be closed by the server.
	// The server's readPump has a read deadline of pongWait (40s) from connection start.
	// Since we never respond to pings, the read deadline will expire and the server
	// will close the connection.
	select {
	case <-done:
		// Connection was closed — expected behavior.
	case <-time.After(50 * time.Second):
		t.Fatal("connection was not closed within expected timeout (pongWait + buffer)")
	}

	// Give the hub time to process the unregister.
	time.Sleep(100 * time.Millisecond)

	if hub.ConnectionCount("user-ping-timeout") != 0 {
		t.Errorf("expected 0 connections after pong timeout, got %d", hub.ConnectionCount("user-ping-timeout"))
	}
}

func TestPingPong_ConnectionStaysAliveWithPong(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-ping-alive")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Default dialer automatically responds to pings with pongs.
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	if hub.ConnectionCount("user-ping-alive") != 1 {
		t.Fatalf("expected 1 connection, got %d", hub.ConnectionCount("user-ping-alive"))
	}

	// Wait past one ping period (30s) + some buffer. The connection should remain alive
	// because the default dialer responds to pings automatically.
	time.Sleep(35 * time.Second)

	if hub.ConnectionCount("user-ping-alive") != 1 {
		t.Errorf("expected connection to remain alive after ping/pong cycle, got %d connections", hub.ConnectionCount("user-ping-alive"))
	}
}

func TestMultipleUsers_Isolation(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use query param to determine user for this test.
		userID := r.URL.Query().Get("user")
		hub.HandleWebSocket(w, r, userID)
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect user-A.
	wsA, _, err := websocket.DefaultDialer.Dial(wsURL+"?user=user-A", nil)
	if err != nil {
		t.Fatalf("failed to connect user-A: %v", err)
	}
	defer wsA.Close()

	// Connect user-B.
	wsB, _, err := websocket.DefaultDialer.Dial(wsURL+"?user=user-B", nil)
	if err != nil {
		t.Fatalf("failed to connect user-B: %v", err)
	}
	defer wsB.Close()

	time.Sleep(50 * time.Millisecond)

	if hub.ConnectionCount("user-A") != 1 {
		t.Errorf("expected 1 connection for user-A, got %d", hub.ConnectionCount("user-A"))
	}
	if hub.ConnectionCount("user-B") != 1 {
		t.Errorf("expected 1 connection for user-B, got %d", hub.ConnectionCount("user-B"))
	}

	// Send a message only to user-A.
	msg := protocol.Message{
		Type:    protocol.TypeChatMsg,
		Payload: json.RawMessage(`{"content":"for A only"}`),
	}
	hub.SendToUser("user-A", msg)

	// user-A should receive the message.
	wsA.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, receivedA, err := wsA.ReadMessage()
	if err != nil {
		t.Fatalf("user-A failed to read message: %v", err)
	}

	var msgA protocol.Message
	json.Unmarshal(receivedA, &msgA)
	if msgA.Type != protocol.TypeChatMsg {
		t.Errorf("user-A: expected type %q, got %q", protocol.TypeChatMsg, msgA.Type)
	}

	// user-B should NOT receive any message (set a short deadline).
	wsB.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	_, _, err = wsB.ReadMessage()
	if err == nil {
		t.Error("user-B should not have received a message intended for user-A")
	}
}
