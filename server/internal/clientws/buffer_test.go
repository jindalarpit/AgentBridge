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

func TestMessageBuffer_AddAndDrain(t *testing.T) {
	mb := NewMessageBuffer()
	defer mb.Stop()

	mb.Add("user-1", []byte(`{"type":"chat:message","payload":{}}`))
	mb.Add("user-1", []byte(`{"type":"chat:stream","payload":{}}`))
	mb.Add("user-1", []byte(`{"type":"chat:done","payload":{}}`))

	if mb.Count("user-1") != 3 {
		t.Errorf("expected 3 buffered messages, got %d", mb.Count("user-1"))
	}

	msgs := mb.Drain("user-1")
	if len(msgs) != 3 {
		t.Fatalf("expected 3 drained messages, got %d", len(msgs))
	}

	// Buffer should be empty after drain.
	if mb.Count("user-1") != 0 {
		t.Errorf("expected 0 buffered messages after drain, got %d", mb.Count("user-1"))
	}

	// Second drain should return nil.
	msgs = mb.Drain("user-1")
	if msgs != nil {
		t.Errorf("expected nil on second drain, got %d messages", len(msgs))
	}
}

func TestMessageBuffer_ChronologicalOrder(t *testing.T) {
	mb := NewMessageBuffer()
	defer mb.Stop()

	for i := 0; i < 5; i++ {
		mb.Add("user-order", []byte{byte('A' + i)})
	}

	msgs := mb.Drain("user-order")
	if len(msgs) != 5 {
		t.Fatalf("expected 5 messages, got %d", len(msgs))
	}

	for i, m := range msgs {
		expected := byte('A' + i)
		if m[0] != expected {
			t.Errorf("message %d: expected %c, got %c", i, expected, m[0])
		}
	}
}

func TestMessageBuffer_MaxCapacity(t *testing.T) {
	mb := NewMessageBuffer()
	defer mb.Stop()

	// Add more than maxBufferedMessages.
	for i := 0; i < maxBufferedMessages+20; i++ {
		mb.Add("user-cap", []byte{byte(i)})
	}

	if mb.Count("user-cap") != maxBufferedMessages {
		t.Errorf("expected %d buffered messages (max), got %d", maxBufferedMessages, mb.Count("user-cap"))
	}

	msgs := mb.Drain("user-cap")
	if len(msgs) != maxBufferedMessages {
		t.Fatalf("expected %d drained messages, got %d", maxBufferedMessages, len(msgs))
	}

	// The oldest 20 messages should have been dropped; first message should be byte(20).
	if msgs[0][0] != byte(20) {
		t.Errorf("expected first message to be byte(20), got byte(%d)", msgs[0][0])
	}
	// Last message should be byte(119) = maxBufferedMessages + 20 - 1.
	lastExpected := byte(maxBufferedMessages + 20 - 1)
	if msgs[len(msgs)-1][0] != lastExpected {
		t.Errorf("expected last message to be byte(%d), got byte(%d)", lastExpected, msgs[len(msgs)-1][0])
	}
}

func TestMessageBuffer_DrainNonexistentUser(t *testing.T) {
	mb := NewMessageBuffer()
	defer mb.Stop()

	msgs := mb.Drain("nonexistent")
	if msgs != nil {
		t.Errorf("expected nil for nonexistent user, got %d messages", len(msgs))
	}
}

func TestMessageBuffer_UserIsolation(t *testing.T) {
	mb := NewMessageBuffer()
	defer mb.Stop()

	mb.Add("user-A", []byte("msg-A"))
	mb.Add("user-B", []byte("msg-B"))

	msgsA := mb.Drain("user-A")
	if len(msgsA) != 1 || string(msgsA[0]) != "msg-A" {
		t.Errorf("user-A: unexpected messages: %v", msgsA)
	}

	msgsB := mb.Drain("user-B")
	if len(msgsB) != 1 || string(msgsB[0]) != "msg-B" {
		t.Errorf("user-B: unexpected messages: %v", msgsB)
	}
}

func TestMessageBuffer_ExpiredMessagesNotDelivered(t *testing.T) {
	// Use a controllable clock.
	now := time.Now()
	clock := func() time.Time { return now }
	mb := NewMessageBufferWithClock(clock)

	// Add a message "6 minutes ago" by setting clock back.
	now = time.Now().Add(-6 * time.Minute)
	mb.Add("user-expired", []byte("old"))

	// Add a fresh message at current time.
	now = time.Now()
	mb.Add("user-expired", []byte("fresh"))

	msgs := mb.Drain("user-expired")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 non-expired message, got %d", len(msgs))
	}
	if string(msgs[0]) != "fresh" {
		t.Errorf("expected 'fresh', got %q", string(msgs[0]))
	}
}

func TestMessageBuffer_AllExpiredReturnsNil(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	mb := NewMessageBufferWithClock(clock)

	// Add messages "in the past".
	now = time.Now().Add(-10 * time.Minute)
	mb.Add("user-all-expired", []byte("old1"))
	now = time.Now().Add(-7 * time.Minute)
	mb.Add("user-all-expired", []byte("old2"))

	// Drain at current time.
	now = time.Now()
	msgs := mb.Drain("user-all-expired")
	if msgs != nil {
		t.Errorf("expected nil when all messages expired, got %d messages", len(msgs))
	}
}

func TestMessageBuffer_Cleanup(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	mb := NewMessageBufferWithClock(clock)

	// Add expired messages.
	now = time.Now().Add(-10 * time.Minute)
	mb.Add("user-cleanup", []byte("expired1"))
	now = time.Now().Add(-6 * time.Minute)
	mb.Add("user-cleanup", []byte("expired2"))

	// Add a fresh message.
	now = time.Now()
	mb.Add("user-cleanup", []byte("fresh1"))

	// Add only expired messages for another user.
	now = time.Now().Add(-10 * time.Minute)
	mb.Add("user-all-gone", []byte("gone"))

	// Run cleanup at current time.
	now = time.Now()
	mb.Cleanup()

	// user-cleanup should only have the fresh message.
	if mb.Count("user-cleanup") != 1 {
		t.Errorf("expected 1 message for user-cleanup after cleanup, got %d", mb.Count("user-cleanup"))
	}

	// user-all-gone should be removed entirely.
	if mb.Count("user-all-gone") != 0 {
		t.Errorf("expected 0 messages for user-all-gone after cleanup, got %d", mb.Count("user-all-gone"))
	}

	// Verify the entry was deleted from the map.
	mb.mu.Lock()
	_, exists := mb.entries["user-all-gone"]
	mb.mu.Unlock()
	if exists {
		t.Error("expected user-all-gone entry to be removed from map")
	}
}

// Integration test: verify that messages sent while a user is disconnected
// are buffered and delivered on reconnection.
func TestHub_BufferedMessageDeliveryOnReconnect(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-buffer")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// Connect and then disconnect.
	ws1, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	if hub.ConnectionCount("user-buffer") != 1 {
		t.Fatalf("expected 1 connection, got %d", hub.ConnectionCount("user-buffer"))
	}

	// Disconnect the client.
	ws1.Close()
	time.Sleep(100 * time.Millisecond)

	if hub.ConnectionCount("user-buffer") != 0 {
		t.Fatalf("expected 0 connections after disconnect, got %d", hub.ConnectionCount("user-buffer"))
	}

	// Send messages while disconnected — these should be buffered.
	for i := 0; i < 3; i++ {
		payload, _ := json.Marshal(protocol.ChatStreamPayload{
			SessionID: "session-1",
			Seq:       i + 1,
			Content:   "token",
		})
		msg := protocol.Message{
			Type:    protocol.TypeChatStream,
			Payload: payload,
		}
		hub.SendToUser("user-buffer", msg)
	}

	// Verify messages were buffered.
	if hub.buffer.Count("user-buffer") != 3 {
		t.Fatalf("expected 3 buffered messages, got %d", hub.buffer.Count("user-buffer"))
	}

	// Reconnect — buffered messages should be delivered.
	ws2, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to reconnect: %v", err)
	}
	defer ws2.Close()

	time.Sleep(50 * time.Millisecond)

	// Read the buffered messages. The writePump may batch multiple messages
	// into a single WebSocket frame separated by newlines, so we collect all
	// frames and split by newline to get individual messages.
	var allMessages []protocol.Message
	for len(allMessages) < 3 {
		ws2.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, data, err := ws2.ReadMessage()
		if err != nil {
			t.Fatalf("failed to read buffered messages (got %d so far): %v", len(allMessages), err)
		}

		// Split by newline since writePump batches messages.
		parts := strings.Split(string(data), "\n")
		for _, part := range parts {
			if part == "" {
				continue
			}
			var msg protocol.Message
			if err := json.Unmarshal([]byte(part), &msg); err != nil {
				t.Fatalf("failed to unmarshal message: %v (raw: %q)", err, part)
			}
			allMessages = append(allMessages, msg)
		}
	}

	if len(allMessages) != 3 {
		t.Fatalf("expected 3 buffered messages, got %d", len(allMessages))
	}

	for i, msg := range allMessages {
		if msg.Type != protocol.TypeChatStream {
			t.Errorf("message %d: expected type %q, got %q", i+1, protocol.TypeChatStream, msg.Type)
		}

		var payload protocol.ChatStreamPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("failed to unmarshal payload %d: %v", i+1, err)
		}
		if payload.Seq != i+1 {
			t.Errorf("message %d: expected seq %d, got %d", i+1, i+1, payload.Seq)
		}
	}

	// Buffer should be empty now.
	if hub.buffer.Count("user-buffer") != 0 {
		t.Errorf("expected buffer to be empty after delivery, got %d", hub.buffer.Count("user-buffer"))
	}
}

// Integration test: verify that BroadcastToUser also buffers when disconnected.
func TestHub_BroadcastBufferedOnDisconnect(t *testing.T) {
	hub := NewHub()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.HandleWebSocket(w, r, "user-bcast-buf")
	}))
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

	// User has no connections — broadcast should buffer.
	payload, _ := json.Marshal(protocol.ChatDonePayload{
		SessionID: "s1",
		MessageID: "m1",
		Content:   "done",
		ElapsedMs: 100,
	})
	msg := protocol.Message{
		Type:    protocol.TypeChatDone,
		Payload: payload,
	}
	hub.BroadcastToUser("user-bcast-buf", msg)

	if hub.buffer.Count("user-bcast-buf") != 1 {
		t.Fatalf("expected 1 buffered message, got %d", hub.buffer.Count("user-bcast-buf"))
	}

	// Connect — should receive the buffered message.
	ws, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer ws.Close()

	time.Sleep(50 * time.Millisecond)

	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, data, err := ws.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read buffered broadcast: %v", err)
	}

	var received protocol.Message
	if err := json.Unmarshal(data, &received); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if received.Type != protocol.TypeChatDone {
		t.Errorf("expected type %q, got %q", protocol.TypeChatDone, received.Type)
	}
}
