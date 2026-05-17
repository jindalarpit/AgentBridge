package connection

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/user/agentbridge/daemon/pkg/protocol"
)

// testWSServer creates a test WebSocket server that accepts connections and
// optionally echoes messages back.
func testWSServer(t *testing.T, handler func(conn *websocket.Conn)) *httptest.Server {
	t.Helper()
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		if handler != nil {
			handler(conn)
		} else {
			// Default: keep connection open until closed
			for {
				_, _, err := conn.ReadMessage()
				if err != nil {
					return
				}
			}
		}
	}))
	return server
}

func wsURL(server *httptest.Server) string {
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func TestNewReconnectingConnection(t *testing.T) {
	called := false
	rc := NewReconnectingConnection("ws://localhost:8080/ws", "test-token", func() {
		called = true
	})

	if rc.serverURL != "ws://localhost:8080/ws" {
		t.Errorf("serverURL = %q, want %q", rc.serverURL, "ws://localhost:8080/ws")
	}
	if rc.token != "test-token" {
		t.Errorf("token = %q, want %q", rc.token, "test-token")
	}
	if rc.onReconnect == nil {
		t.Error("onReconnect should not be nil")
	}
	// Verify callback is stored correctly
	rc.onReconnect()
	if !called {
		t.Error("onReconnect callback was not invoked")
	}
}

func TestReconnectingConnection_StartAndStop(t *testing.T) {
	server := testWSServer(t, nil)
	defer server.Close()

	rc := NewReconnectingConnection(wsURL(server)+"/ws", "token", func() {})

	ctx := context.Background()
	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if !rc.IsConnected() {
		t.Error("expected IsConnected() to be true after Start()")
	}

	rc.Stop()

	// After stop, IsConnected should be false
	if rc.IsConnected() {
		t.Error("expected IsConnected() to be false after Stop()")
	}
}

func TestReconnectingConnection_StartFailsOnBadURL(t *testing.T) {
	rc := NewReconnectingConnection("ws://localhost:1/nonexistent", "token", func() {})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := rc.Start(ctx)
	if err == nil {
		rc.Stop()
		t.Fatal("Start() should have returned an error for unreachable server")
	}
}

func TestReconnectingConnection_Send(t *testing.T) {
	var received atomic.Value

	server := testWSServer(t, func(conn *websocket.Conn) {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		received.Store(string(data))
		// Keep connection open
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	rc := NewReconnectingConnection(wsURL(server)+"/ws", "token", func() {})

	ctx := context.Background()
	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer rc.Stop()

	msg := protocol.Message{
		Type:    protocol.TypeDaemonHeartbeat,
		Payload: json.RawMessage(`{}`),
	}

	if err := rc.Send(msg); err != nil {
		t.Fatalf("Send() error: %v", err)
	}

	// Wait for message to be received
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v := received.Load(); v != nil {
			// Verify it's valid JSON with the right type
			var got protocol.Message
			if err := json.Unmarshal([]byte(v.(string)), &got); err != nil {
				t.Fatalf("received invalid JSON: %v", err)
			}
			if got.Type != protocol.TypeDaemonHeartbeat {
				t.Errorf("received type = %q, want %q", got.Type, protocol.TypeDaemonHeartbeat)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("timed out waiting for message to be received by server")
}

func TestReconnectingConnection_SendFailsWhenNotConnected(t *testing.T) {
	rc := NewReconnectingConnection("ws://localhost:1/ws", "token", func() {})

	msg := protocol.Message{
		Type:    protocol.TypeDaemonHeartbeat,
		Payload: json.RawMessage(`{}`),
	}

	err := rc.Send(msg)
	if err == nil {
		t.Error("Send() should fail when not connected")
	}
}

func TestReconnectingConnection_OnMessage(t *testing.T) {
	var receivedMsg atomic.Value

	server := testWSServer(t, func(conn *websocket.Conn) {
		// Send a message to the client
		msg := protocol.Message{
			Type:    protocol.TypeDaemonRegisterAck,
			Payload: json.RawMessage(`{}`),
		}
		data, _ := json.Marshal(msg)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		// Keep connection open
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	rc := NewReconnectingConnection(wsURL(server)+"/ws", "token", func() {})

	ctx := context.Background()
	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer rc.Stop()

	rc.OnMessage(func(msg protocol.Message) {
		receivedMsg.Store(msg.Type)
	})

	// Wait for message to be received
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if v := receivedMsg.Load(); v != nil {
			if v.(string) != protocol.TypeDaemonRegisterAck {
				t.Errorf("received type = %q, want %q", v.(string), protocol.TypeDaemonRegisterAck)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("timed out waiting for message from server")
}

func TestReconnectingConnection_ReconnectsOnDisconnect(t *testing.T) {
	var (
		connCount   atomic.Int32
		reconnected atomic.Bool
		serverMu    sync.Mutex
		serverConns []*websocket.Conn
	)

	server := testWSServer(t, func(conn *websocket.Conn) {
		connCount.Add(1)
		serverMu.Lock()
		serverConns = append(serverConns, conn)
		serverMu.Unlock()
		// Keep connection open until closed
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})
	defer server.Close()

	rc := NewReconnectingConnection(wsURL(server)+"/ws", "token", func() {
		reconnected.Store(true)
	})

	ctx := context.Background()
	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer rc.Stop()

	// Verify initial connection
	if connCount.Load() != 1 {
		t.Fatalf("expected 1 connection, got %d", connCount.Load())
	}

	// Close the server-side connection to simulate disconnect
	serverMu.Lock()
	if len(serverConns) > 0 {
		_ = serverConns[0].Close()
	}
	serverMu.Unlock()

	// Wait for reconnection (backoff starts at 1s, plus monitoring interval)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if reconnected.Load() && connCount.Load() >= 2 {
			// Verify we're connected again
			if !rc.IsConnected() {
				t.Error("expected IsConnected() to be true after reconnection")
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("timed out waiting for reconnection: connCount=%d, reconnected=%v",
		connCount.Load(), reconnected.Load())
}

func TestReconnectingConnection_StopCancelsReconnect(t *testing.T) {
	// Start with a server, then close it and stop the reconnecting connection
	server := testWSServer(t, nil)

	rc := NewReconnectingConnection(wsURL(server)+"/ws", "token", func() {})

	ctx := context.Background()
	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Close the server to trigger reconnection attempts
	server.Close()

	// Give it a moment to detect disconnection
	time.Sleep(600 * time.Millisecond)

	// Stop should complete without hanging
	done := make(chan struct{})
	go func() {
		rc.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Good - Stop completed
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not complete within timeout - likely hanging")
	}
}

func TestReconnectingConnection_IsConnectedFalseWhenNoConn(t *testing.T) {
	rc := NewReconnectingConnection("ws://localhost:1/ws", "token", func() {})
	if rc.IsConnected() {
		t.Error("expected IsConnected() to be false before Start()")
	}
}

func TestReconnectingConnection_MultipleStopCalls(t *testing.T) {
	server := testWSServer(t, nil)
	defer server.Close()

	rc := NewReconnectingConnection(wsURL(server)+"/ws", "token", func() {})

	ctx := context.Background()
	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Multiple Stop calls should not panic
	rc.Stop()
	rc.Stop()
}
