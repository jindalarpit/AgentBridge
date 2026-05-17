package service

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/user/agentbridge/server/internal/clientws"
	"github.com/user/agentbridge/server/internal/daemonws"
	"github.com/user/agentbridge/server/pkg/protocol"
)

// mockClientHub implements clientws.ClientHub for testing.
type mockClientHub struct {
	mu        sync.Mutex
	sent      []sentMessage
	broadcast []sentMessage
}

type sentMessage struct {
	UserID  string
	Message protocol.Message
}

func (m *mockClientHub) HandleWebSocket(w http.ResponseWriter, r *http.Request, userID string) {}
func (m *mockClientHub) SendToUser(userID string, msg protocol.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, sentMessage{UserID: userID, Message: msg})
}
func (m *mockClientHub) BroadcastToUser(userID string, msg protocol.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcast = append(m.broadcast, sentMessage{UserID: userID, Message: msg})
}
func (m *mockClientHub) ConnectionCount(userID string) int { return 1 }

func (m *mockClientHub) getBroadcast() []sentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]sentMessage, len(m.broadcast))
	copy(result, m.broadcast)
	return result
}

// mockDaemonHub implements daemonws.DaemonHub for testing.
type mockDaemonHub struct {
	mu                sync.Mutex
	messageHandler    daemonws.MessageHandler
	disconnectHandler daemonws.DisconnectHandler
}

func (m *mockDaemonHub) HandleWebSocket(w http.ResponseWriter, r *http.Request, identity daemonws.DaemonIdentity) {
}
func (m *mockDaemonHub) SendToDaemon(daemonID string, msg protocol.Message) error { return nil }
func (m *mockDaemonHub) IsOnline(daemonID string) bool                            { return true }
func (m *mockDaemonHub) SetHeartbeatHandler(fn daemonws.HeartbeatHandler)          {}
func (m *mockDaemonHub) SetRegistrationHandler(fn daemonws.RegistrationHandler)    {}
func (m *mockDaemonHub) SetMessageHandler(fn daemonws.MessageHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messageHandler = fn
}
func (m *mockDaemonHub) SetDisconnectHandler(fn daemonws.DisconnectHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disconnectHandler = fn
}

func (m *mockDaemonHub) triggerMessage(identity daemonws.DaemonIdentity, msg protocol.Message) {
	m.mu.Lock()
	handler := m.messageHandler
	m.mu.Unlock()
	if handler != nil {
		handler(identity, msg)
	}
}

func (m *mockDaemonHub) triggerDisconnect(identity daemonws.DaemonIdentity) {
	m.mu.Lock()
	handler := m.disconnectHandler
	m.mu.Unlock()
	if handler != nil {
		handler(identity)
	}
}

// setupRelay creates a test relay with mock dependencies and a pre-configured session.
func setupRelay(t *testing.T) (*DaemonRelay, *mockDaemonHub, *mockClientHub, *InMemoryRuntimeService, *InMemoryChatService, string) {
	t.Helper()

	daemonHub := &mockDaemonHub{}
	clientHub := &mockClientHub{}
	runtimeSvc := NewInMemoryRuntimeService()
	chatSvc := NewInMemoryChatService()

	// Register a daemon.
	_ = runtimeSvc.RegisterDaemon(context.Background(), DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"},
		},
	})

	// Create a session for user-1.
	session, err := chatSvc.CreateSession(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	relay := NewDaemonRelay(daemonHub, clientHub, runtimeSvc, chatSvc)

	return relay, daemonHub, clientHub, runtimeSvc, chatSvc, session.ID
}

func TestDaemonRelay_ChatStream_ForwardsToClient(t *testing.T) {
	_, daemonHub, clientHub, _, _, sessionID := setupRelay(t)

	// Simulate a chat:stream message from the daemon.
	streamPayload, _ := json.Marshal(protocol.ChatStreamPayload{
		SessionID: sessionID,
		Seq:       1,
		Content:   "Hello",
	})
	msg := protocol.Message{
		Type:    protocol.TypeChatStream,
		Payload: streamPayload,
	}

	identity := daemonws.DaemonIdentity{DaemonID: "daemon-1", UserID: "user-1"}
	daemonHub.triggerMessage(identity, msg)

	// Verify the message was broadcast to user-1.
	broadcasts := clientHub.getBroadcast()
	if len(broadcasts) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(broadcasts))
	}
	if broadcasts[0].UserID != "user-1" {
		t.Errorf("expected broadcast to user-1, got %s", broadcasts[0].UserID)
	}
	if broadcasts[0].Message.Type != protocol.TypeChatStream {
		t.Errorf("expected message type %s, got %s", protocol.TypeChatStream, broadcasts[0].Message.Type)
	}
}

func TestDaemonRelay_ChatDone_PersistsAndForwards(t *testing.T) {
	_, daemonHub, clientHub, _, chatSvc, sessionID := setupRelay(t)

	// Simulate a chat:done message from the daemon.
	donePayload, _ := json.Marshal(protocol.ChatDonePayload{
		SessionID: sessionID,
		MessageID: "msg-assistant-1",
		Content:   "I can help with that!",
		ElapsedMs: 1500,
	})
	msg := protocol.Message{
		Type:    protocol.TypeChatDone,
		Payload: donePayload,
	}

	identity := daemonws.DaemonIdentity{DaemonID: "daemon-1", UserID: "user-1"}
	daemonHub.triggerMessage(identity, msg)

	// Verify the assistant message was persisted.
	messages, err := chatSvc.GetMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Role != "assistant" {
		t.Errorf("expected role 'assistant', got %s", messages[0].Role)
	}
	if messages[0].Content != "I can help with that!" {
		t.Errorf("expected content 'I can help with that!', got %s", messages[0].Content)
	}
	if messages[0].Status != "complete" {
		t.Errorf("expected status 'complete', got %s", messages[0].Status)
	}
	if messages[0].ElapsedMs == nil || *messages[0].ElapsedMs != 1500 {
		t.Errorf("expected elapsed_ms 1500, got %v", messages[0].ElapsedMs)
	}

	// Verify the message was broadcast to user-1.
	broadcasts := clientHub.getBroadcast()
	if len(broadcasts) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(broadcasts))
	}
	if broadcasts[0].UserID != "user-1" {
		t.Errorf("expected broadcast to user-1, got %s", broadcasts[0].UserID)
	}
	if broadcasts[0].Message.Type != protocol.TypeChatDone {
		t.Errorf("expected message type %s, got %s", protocol.TypeChatDone, broadcasts[0].Message.Type)
	}
}

func TestDaemonRelay_ChatError_MarksAndForwards(t *testing.T) {
	_, daemonHub, clientHub, _, chatSvc, sessionID := setupRelay(t)

	// Simulate a chat:error message from the daemon.
	errPayload, _ := json.Marshal(protocol.ChatErrorPayload{
		SessionID: sessionID,
		MessageID: "msg-err-1",
		Error:     "agent timed out",
		Code:      protocol.ErrCodeAgentTimeout,
	})
	msg := protocol.Message{
		Type:    protocol.TypeChatError,
		Payload: errPayload,
	}

	identity := daemonws.DaemonIdentity{DaemonID: "daemon-1", UserID: "user-1"}
	daemonHub.triggerMessage(identity, msg)

	// Verify the error was recorded.
	messages, err := chatSvc.GetMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("failed to get messages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].Status != "error" {
		t.Errorf("expected status 'error', got %s", messages[0].Status)
	}
	if messages[0].FailureReason == nil {
		t.Fatal("expected failure_reason to be set")
	}
	if *messages[0].FailureReason != "agent_timeout: agent timed out" {
		t.Errorf("unexpected failure_reason: %s", *messages[0].FailureReason)
	}

	// Verify the error was broadcast to user-1.
	broadcasts := clientHub.getBroadcast()
	if len(broadcasts) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(broadcasts))
	}
	if broadcasts[0].Message.Type != protocol.TypeChatError {
		t.Errorf("expected message type %s, got %s", protocol.TypeChatError, broadcasts[0].Message.Type)
	}
}

func TestDaemonRelay_Disconnect_MarksOfflineAndNotifies(t *testing.T) {
	_, daemonHub, clientHub, runtimeSvc, _, _ := setupRelay(t)

	identity := daemonws.DaemonIdentity{DaemonID: "daemon-1", UserID: "user-1"}
	daemonHub.triggerDisconnect(identity)

	// Verify the daemon was marked offline.
	daemon, err := runtimeSvc.GetDaemonByID(context.Background(), "daemon-1")
	if err != nil {
		t.Fatalf("failed to get daemon: %v", err)
	}
	if daemon.Status != "offline" {
		t.Errorf("expected daemon status 'offline', got %s", daemon.Status)
	}

	// Verify a runtime:status message was broadcast to user-1.
	broadcasts := clientHub.getBroadcast()
	if len(broadcasts) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(broadcasts))
	}
	if broadcasts[0].UserID != "user-1" {
		t.Errorf("expected broadcast to user-1, got %s", broadcasts[0].UserID)
	}
	if broadcasts[0].Message.Type != protocol.TypeRuntimeStatus {
		t.Errorf("expected message type %s, got %s", protocol.TypeRuntimeStatus, broadcasts[0].Message.Type)
	}

	// Verify the payload contains the daemon ID and offline status.
	var statusPayload struct {
		DaemonID string `json:"daemon_id"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(broadcasts[0].Message.Payload, &statusPayload); err != nil {
		t.Fatalf("failed to unmarshal status payload: %v", err)
	}
	if statusPayload.DaemonID != "daemon-1" {
		t.Errorf("expected daemon_id 'daemon-1', got %s", statusPayload.DaemonID)
	}
	if statusPayload.Status != "offline" {
		t.Errorf("expected status 'offline', got %s", statusPayload.Status)
	}
}

func TestDaemonRelay_ChatStream_InvalidSession(t *testing.T) {
	_, daemonHub, clientHub, _, _, _ := setupRelay(t)

	// Simulate a chat:stream for a non-existent session.
	streamPayload, _ := json.Marshal(protocol.ChatStreamPayload{
		SessionID: "non-existent-session",
		Seq:       1,
		Content:   "Hello",
	})
	msg := protocol.Message{
		Type:    protocol.TypeChatStream,
		Payload: streamPayload,
	}

	identity := daemonws.DaemonIdentity{DaemonID: "daemon-1", UserID: "user-1"}
	daemonHub.triggerMessage(identity, msg)

	// Verify no message was broadcast (session not found).
	broadcasts := clientHub.getBroadcast()
	if len(broadcasts) != 0 {
		t.Errorf("expected 0 broadcasts for invalid session, got %d", len(broadcasts))
	}
}

func TestDaemonRelay_ChatDone_InvalidSession(t *testing.T) {
	_, daemonHub, clientHub, _, _, _ := setupRelay(t)

	// Simulate a chat:done for a non-existent session.
	donePayload, _ := json.Marshal(protocol.ChatDonePayload{
		SessionID: "non-existent-session",
		MessageID: "msg-1",
		Content:   "response",
		ElapsedMs: 100,
	})
	msg := protocol.Message{
		Type:    protocol.TypeChatDone,
		Payload: donePayload,
	}

	identity := daemonws.DaemonIdentity{DaemonID: "daemon-1", UserID: "user-1"}
	daemonHub.triggerMessage(identity, msg)

	// Verify no message was broadcast.
	broadcasts := clientHub.getBroadcast()
	if len(broadcasts) != 0 {
		t.Errorf("expected 0 broadcasts for invalid session, got %d", len(broadcasts))
	}
}

func TestDaemonRelay_Disconnect_UnknownDaemon(t *testing.T) {
	_, daemonHub, clientHub, _, _, _ := setupRelay(t)

	// Disconnect an unknown daemon — should not panic.
	identity := daemonws.DaemonIdentity{DaemonID: "unknown-daemon", UserID: "user-1"}
	daemonHub.triggerDisconnect(identity)

	// Should still broadcast the status notification to the user.
	broadcasts := clientHub.getBroadcast()
	if len(broadcasts) != 1 {
		t.Fatalf("expected 1 broadcast even for unknown daemon, got %d", len(broadcasts))
	}
	if broadcasts[0].Message.Type != protocol.TypeRuntimeStatus {
		t.Errorf("expected runtime:status message, got %s", broadcasts[0].Message.Type)
	}
}

// Verify interface compliance at compile time.
var _ clientws.ClientHub = (*mockClientHub)(nil)
var _ daemonws.DaemonHub = (*mockDaemonHub)(nil)
