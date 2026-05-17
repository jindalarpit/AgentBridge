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

func TestMalformedRateLimiter_AllowsUpToMaxCount(t *testing.T) {
	rl := newMalformedRateLimiter(60*time.Second, 10)
	now := time.Now()

	// Record 10 malformed messages — all should be tolerated.
	for i := 0; i < 10; i++ {
		shouldClose := rl.Record(now.Add(time.Duration(i) * time.Second))
		if shouldClose {
			t.Fatalf("expected connection to remain open after %d malformed messages", i+1)
		}
	}
}

func TestMalformedRateLimiter_ClosesOnExceedingMaxCount(t *testing.T) {
	rl := newMalformedRateLimiter(60*time.Second, 10)
	now := time.Now()

	// Record 10 malformed messages — all tolerated.
	for i := 0; i < 10; i++ {
		rl.Record(now.Add(time.Duration(i) * time.Second))
	}

	// The 11th should trigger close.
	shouldClose := rl.Record(now.Add(10 * time.Second))
	if !shouldClose {
		t.Fatal("expected connection to be closed after 11th malformed message within window")
	}
}

func TestMalformedRateLimiter_SlidingWindowEviction(t *testing.T) {
	rl := newMalformedRateLimiter(60*time.Second, 10)
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Record 10 malformed messages at t=0s through t=9s.
	for i := 0; i < 10; i++ {
		rl.Record(baseTime.Add(time.Duration(i) * time.Second))
	}

	// At t=61s, the first message (t=0s) should have expired from the window.
	// So we should have 9 messages in the window, and the 11th overall (but 10th in window)
	// should NOT trigger close.
	shouldClose := rl.Record(baseTime.Add(61 * time.Second))
	if shouldClose {
		t.Fatal("expected connection to remain open after window eviction")
	}
}

func TestMalformedRateLimiter_AllExpired(t *testing.T) {
	rl := newMalformedRateLimiter(60*time.Second, 10)
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Record 10 malformed messages at t=0s.
	for i := 0; i < 10; i++ {
		rl.Record(baseTime)
	}

	// At t=61s, all messages should have expired. A new message should be tolerated.
	shouldClose := rl.Record(baseTime.Add(61 * time.Second))
	if shouldClose {
		t.Fatal("expected connection to remain open after all messages expired")
	}

	// Verify count is 1.
	count := rl.Count(baseTime.Add(61 * time.Second))
	if count != 1 {
		t.Errorf("expected count 1 after expiration, got %d", count)
	}
}

func TestMalformedRateLimiter_Count(t *testing.T) {
	rl := newMalformedRateLimiter(60*time.Second, 10)
	now := time.Now()

	if rl.Count(now) != 0 {
		t.Errorf("expected initial count 0, got %d", rl.Count(now))
	}

	rl.Record(now)
	rl.Record(now.Add(1 * time.Second))
	rl.Record(now.Add(2 * time.Second))

	if rl.Count(now.Add(3*time.Second)) != 3 {
		t.Errorf("expected count 3, got %d", rl.Count(now.Add(3*time.Second)))
	}
}

func TestMalformedRateLimiter_ExactBoundary(t *testing.T) {
	rl := newMalformedRateLimiter(60*time.Second, 10)
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Record a message at exactly t=0.
	rl.Record(baseTime)

	// At exactly t=60s (the boundary), the message at t=0 should be evicted
	// because the window is (now-60s, now], i.e., messages at exactly the cutoff are evicted.
	count := rl.Count(baseTime.Add(60 * time.Second))
	if count != 0 {
		t.Errorf("expected count 0 at exact boundary, got %d", count)
	}
}

func TestMalformedRateLimiter_JustBeforeBoundary(t *testing.T) {
	rl := newMalformedRateLimiter(60*time.Second, 10)
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Record a message at t=1ns (just after base).
	rl.Record(baseTime.Add(1 * time.Nanosecond))

	// At t=60s, the cutoff is t=0s. The message at t=1ns is after the cutoff, so it stays.
	count := rl.Count(baseTime.Add(60 * time.Second))
	if count != 1 {
		t.Errorf("expected count 1 just before boundary, got %d", count)
	}
}

func TestMalformedRateLimit_Integration_ConnectionClosedAfterExceedingLimit(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-malformed")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	if hub.ConnectionCount("user-malformed") != 1 {
		t.Fatalf("expected 1 connection, got %d", hub.ConnectionCount("user-malformed"))
	}

	// Send 11 malformed messages (not valid JSON).
	for i := 0; i < 11; i++ {
		err := ws.WriteMessage(websocket.TextMessage, []byte("not valid json {{{"))
		if err != nil {
			// Connection may have been closed by the server after the 11th message.
			if i >= 10 {
				break
			}
			t.Fatalf("failed to send malformed message %d: %v", i+1, err)
		}
		// Small delay to ensure messages are processed sequentially.
		time.Sleep(10 * time.Millisecond)
	}

	// Give the server time to process and close the connection.
	time.Sleep(100 * time.Millisecond)

	// The connection should be closed.
	if hub.ConnectionCount("user-malformed") != 0 {
		t.Errorf("expected 0 connections after rate limit exceeded, got %d", hub.ConnectionCount("user-malformed"))
	}
}

func TestMalformedRateLimit_Integration_ConnectionSurvives10Malformed(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-survives")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send exactly 10 malformed messages — connection should survive.
	for i := 0; i < 10; i++ {
		err := ws.WriteMessage(websocket.TextMessage, []byte("bad json"))
		if err != nil {
			t.Fatalf("failed to send malformed message %d: %v", i+1, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	// Connection should still be alive.
	if hub.ConnectionCount("user-survives") != 1 {
		t.Errorf("expected 1 connection (should survive 10 malformed), got %d", hub.ConnectionCount("user-survives"))
	}

	// Verify we can still send a valid message.
	validMsg := map[string]interface{}{
		"type":    "connection:pong",
		"payload": json.RawMessage("null"),
	}
	data, _ := json.Marshal(validMsg)
	err = ws.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		t.Fatalf("failed to send valid message after 10 malformed: %v", err)
	}
}

func TestMalformedRateLimit_Integration_MixedValidAndMalformed(t *testing.T) {
	hub := NewHub()

	received := make(chan struct{}, 20)
	hub.SetMessageHandler(func(userID string, msg protocol.Message) {
		received <- struct{}{}
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-mixed")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect WebSocket: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	// Send alternating valid and malformed messages.
	// 5 malformed + 5 valid = connection should survive.
	for i := 0; i < 5; i++ {
		// Malformed
		ws.WriteMessage(websocket.TextMessage, []byte("invalid"))
		time.Sleep(5 * time.Millisecond)

		// Valid
		validMsg := map[string]interface{}{
			"type":    "chat:send",
			"payload": json.RawMessage(`{"session_id":"s1","content":"hello"}`),
		}
		data, _ := json.Marshal(validMsg)
		ws.WriteMessage(websocket.TextMessage, data)
		time.Sleep(5 * time.Millisecond)
	}

	time.Sleep(100 * time.Millisecond)

	// Connection should still be alive (only 5 malformed, well under limit of 10).
	if hub.ConnectionCount("user-mixed") != 1 {
		t.Errorf("expected 1 connection with mixed messages, got %d", hub.ConnectionCount("user-mixed"))
	}
}
