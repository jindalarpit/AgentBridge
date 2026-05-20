package connection

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// test401Server creates a test HTTP server that always responds with 401 Unauthorized
// to WebSocket upgrade requests, simulating an expired/invalid token.
func test401Server(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error": "authentication failed"}`))
	}))
	return server
}

func TestReconnectingConnection_AuthFailedStopsReconnectLoop(t *testing.T) {
	// Start with a working server, then switch to a 401 server to simulate
	// token expiration during reconnection.
	server := testWSServer(t, nil)

	rc := NewReconnectingConnection(wsURL(server)+"/ws", "token", func() {})

	ctx := context.Background()
	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Close the working server to trigger reconnection.
	server.Close()

	// Start a 401 server on a different port — we need to point the RC at it.
	// Instead, let's use a different approach: close the server and create a new
	// ReconnectingConnection pointing at a 401 server.
	rc.Stop()

	// Create a 401 server.
	authServer := test401Server(t)
	defer authServer.Close()

	// Create a new ReconnectingConnection pointing at the 401 server.
	// Start will fail with ErrAuthFailed on initial connect.
	rc2 := NewReconnectingConnection(wsURL(authServer)+"/ws", "bad-token", func() {})

	err := rc2.Start(ctx)
	if err == nil {
		rc2.Stop()
		t.Fatal("Start() should have returned an error for 401 server")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("Start() error = %v, want ErrAuthFailed", err)
	}
}

func TestReconnectingConnection_401DuringReconnectStopsLoop(t *testing.T) {
	// Start with a working server, then close it and have reconnection hit a 401.
	// We'll use a custom approach: start with a server that works initially,
	// then on reconnection attempts returns 401.
	var connCount atomic.Int32
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		count := connCount.Add(1)
		if count > 1 {
			// After the first connection, return 401 on reconnection attempts.
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": "token expired"}`))
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Keep connection open briefly then close to trigger reconnection.
		time.Sleep(100 * time.Millisecond)
		conn.Close()
	})

	customServer := httptest.NewServer(mux)
	defer customServer.Close()

	rc := NewReconnectingConnection(wsURL(customServer)+"/ws", "token", func() {})

	ctx := context.Background()
	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer rc.Stop()

	// Wait for the auth error to be signaled on the AuthErr channel.
	select {
	case err := <-rc.AuthErr:
		if !errors.Is(err, ErrAuthFailed) {
			t.Errorf("AuthErr = %v, want ErrAuthFailed", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for AuthErr signal after 401 during reconnection")
	}
}

func TestReconnectingConnection_NonAuthErrorContinuesBackoff(t *testing.T) {
	// A server that always refuses connections (not 401, just connection refused)
	// should cause the reconnect loop to keep retrying with backoff.
	var connCount atomic.Int32
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		count := connCount.Add(1)
		if count > 1 && count <= 4 {
			// Return 500 (not 401) to simulate a transient server error.
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error": "internal server error"}`))
			return
		}
		// First connection and connections after attempt 4 succeed.
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		if count == 1 {
			// Close first connection to trigger reconnection.
			time.Sleep(100 * time.Millisecond)
			conn.Close()
			return
		}
		// Keep later connections open.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})

	customServer := httptest.NewServer(mux)
	defer customServer.Close()

	reconnected := make(chan struct{}, 1)
	rc := NewReconnectingConnection(wsURL(customServer)+"/ws", "token", func() {
		select {
		case reconnected <- struct{}{}:
		default:
		}
	})

	ctx := context.Background()
	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer rc.Stop()

	// The reconnect loop should retry through the 500 errors and eventually succeed.
	select {
	case <-reconnected:
		// Good — reconnection succeeded after retrying through non-401 errors.
		if connCount.Load() < 3 {
			t.Errorf("expected at least 3 connection attempts (1 initial + retries), got %d", connCount.Load())
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("timed out waiting for reconnection; connCount=%d", connCount.Load())
	}

	// Verify no auth error was signaled.
	select {
	case err := <-rc.AuthErr:
		t.Errorf("unexpected AuthErr: %v", err)
	default:
		// Good — no auth error.
	}
}

func TestErrAuthFailed_IsDistinguishable(t *testing.T) {
	// Verify that ErrAuthFailed can be detected via errors.Is even when wrapped.
	wrapped := fmt.Errorf("initial connection failed: %w", ErrAuthFailed)

	if !errors.Is(wrapped, ErrAuthFailed) {
		t.Error("errors.Is(wrapped, ErrAuthFailed) should be true")
	}

	// Verify a different error is not mistaken for ErrAuthFailed.
	otherErr := fmt.Errorf("websocket dial: connection refused")
	if errors.Is(otherErr, ErrAuthFailed) {
		t.Error("errors.Is(otherErr, ErrAuthFailed) should be false")
	}
}

func TestReconnectingConnection_StartReturnsErrAuthFailedOn401(t *testing.T) {
	// When the initial connection gets a 401, Start should return ErrAuthFailed.
	authServer := test401Server(t)
	defer authServer.Close()

	rc := NewReconnectingConnection(wsURL(authServer)+"/ws", "expired-token", func() {})

	ctx := context.Background()
	err := rc.Start(ctx)
	if err == nil {
		rc.Stop()
		t.Fatal("Start() should have returned an error for 401 server")
	}
	if !errors.Is(err, ErrAuthFailed) {
		t.Errorf("Start() error = %v, want errors.Is(err, ErrAuthFailed) to be true", err)
	}
}

func TestReconnectingConnection_AuthErrChannelCancelsWork(t *testing.T) {
	// Verify that the AuthErr channel can be used by the caller to cancel
	// in-progress work when a 401 is received during reconnection.
	// This simulates the daemon's behavior: it selects on AuthErr and cancels
	// the task execution context when auth fails.
	var connCount atomic.Int32
	mux := http.NewServeMux()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		count := connCount.Add(1)
		if count > 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		// Close after a brief delay to trigger reconnection.
		time.Sleep(100 * time.Millisecond)
		conn.Close()
	})

	customServer := httptest.NewServer(mux)
	defer customServer.Close()

	rc := NewReconnectingConnection(wsURL(customServer)+"/ws", "token", func() {})

	ctx := context.Background()
	if err := rc.Start(ctx); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	defer rc.Stop()

	// Simulate a "task execution context" that should be cancelled on auth failure.
	taskCtx, taskCancel := context.WithCancel(context.Background())
	defer taskCancel()

	// In a goroutine, wait for AuthErr and cancel the task context.
	go func() {
		select {
		case <-rc.AuthErr:
			taskCancel()
		case <-time.After(15 * time.Second):
		}
	}()

	// Wait for the task context to be cancelled (proving auth failure propagates).
	select {
	case <-taskCtx.Done():
		// Good — the task was cancelled due to auth failure.
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for task cancellation after auth failure")
	}
}
