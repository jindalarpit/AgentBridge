package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"

	"github.com/user/agentbridge/server/internal/clientws"
	"github.com/user/agentbridge/server/internal/daemonws"
	"github.com/user/agentbridge/server/internal/service"
	"github.com/user/agentbridge/server/pkg/protocol"
)

// mockDaemonHub implements daemonws.DaemonHub for testing.
type mockDaemonHub struct {
	mu       sync.Mutex
	messages map[string][]protocol.Message // keyed by daemonID
	online   map[string]bool
}

func newMockDaemonHub() *mockDaemonHub {
	return &mockDaemonHub{
		messages: make(map[string][]protocol.Message),
		online:   make(map[string]bool),
	}
}

func (m *mockDaemonHub) HandleWebSocket(_ http.ResponseWriter, _ *http.Request, _ daemonws.DaemonIdentity) {
}

func (m *mockDaemonHub) SendToDaemon(daemonID string, msg protocol.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.online[daemonID] {
		return &daemonOfflineError{daemonID: daemonID}
	}
	m.messages[daemonID] = append(m.messages[daemonID], msg)
	return nil
}

func (m *mockDaemonHub) IsOnline(daemonID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.online[daemonID]
}

func (m *mockDaemonHub) SetHeartbeatHandler(_ daemonws.HeartbeatHandler) {}

func (m *mockDaemonHub) SetRegistrationHandler(_ daemonws.RegistrationHandler) {}

func (m *mockDaemonHub) SetMessageHandler(_ daemonws.MessageHandler) {}

func (m *mockDaemonHub) SetDisconnectHandler(_ daemonws.DisconnectHandler) {}

func (m *mockDaemonHub) setOnline(daemonID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.online[daemonID] = true
}

func (m *mockDaemonHub) getMessages(daemonID string) []protocol.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages[daemonID]
}

type daemonOfflineError struct {
	daemonID string
}

func (e *daemonOfflineError) Error() string {
	return "daemon not connected: " + e.daemonID
}

// captureHub wraps clientws.Hub to capture messages sent to users.
type captureHub struct {
	*clientws.Hub
	mu       sync.Mutex
	sent     map[string][]protocol.Message // keyed by userID
	broadcast map[string][]protocol.Message // keyed by userID
}

func newCaptureHub() *captureHub {
	return &captureHub{
		Hub:       clientws.NewHub(),
		sent:      make(map[string][]protocol.Message),
		broadcast: make(map[string][]protocol.Message),
	}
}

func (c *captureHub) getSent(userID string) []protocol.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sent[userID]
}

func (c *captureHub) getBroadcast(userID string) []protocol.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.broadcast[userID]
}

func TestWSRouter_ChatSend_Success(t *testing.T) {
	// Setup services.
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	daemonHub := newMockDaemonHub()
	clientHub := clientws.NewHub()

	ctx := context.Background()
	userID := "user-1"

	// Create a session.
	session, err := chatSvc.CreateSession(ctx, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Register a daemon with a runtime.
	err = runtimeSvc.RegisterDaemon(ctx, service.DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   userID,
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterDaemon: %v", err)
	}

	// Get the runtime ID.
	runtimes, err := runtimeSvc.GetUserRuntimes(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserRuntimes: %v", err)
	}
	if len(runtimes) == 0 {
		t.Fatal("expected at least one runtime")
	}
	runtimeID := runtimes[0].ID

	// Bind the runtime to the session.
	err = chatSvc.BindSessionRuntime(ctx, userID, session.ID, runtimeID, runtimeSvc)
	if err != nil {
		t.Fatalf("BindSessionRuntime: %v", err)
	}

	// Mark daemon as online in mock.
	daemonHub.setOnline("daemon-1")

	// Create the router.
	router := NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, service.NewMessageQueue())
	_ = router

	// Simulate a chat:send message.
	sendPayload := protocol.ChatSendPayload{
		SessionID: session.ID,
		Content:   "Hello, agent!",
	}
	payloadData, _ := json.Marshal(sendPayload)
	msg := protocol.Message{
		Type:    protocol.TypeChatSend,
		Payload: payloadData,
	}

	// Invoke the handler directly.
	router.handleClientMessage(userID, msg)

	// Verify the message was relayed to the daemon.
	daemonMsgs := daemonHub.getMessages("daemon-1")
	if len(daemonMsgs) == 0 {
		t.Fatal("expected message to be relayed to daemon")
	}

	if daemonMsgs[0].Type != protocol.TypeChatTask {
		t.Errorf("expected message type %q, got %q", protocol.TypeChatTask, daemonMsgs[0].Type)
	}

	// Verify the task payload.
	var task protocol.ChatTaskPayload
	if err := json.Unmarshal(daemonMsgs[0].Payload, &task); err != nil {
		t.Fatalf("unmarshal task payload: %v", err)
	}

	if task.SessionID != session.ID {
		t.Errorf("task.SessionID = %q, want %q", task.SessionID, session.ID)
	}
	if task.Content != "Hello, agent!" {
		t.Errorf("task.Content = %q, want %q", task.Content, "Hello, agent!")
	}
	// The router overrides RuntimeID with the agent_type so the daemon can
	// resolve it to a binary path (daemon maps by agent_type, not server IDs).
	expectedAgentType := "claude"
	if task.RuntimeID != expectedAgentType {
		t.Errorf("task.RuntimeID = %q, want %q (agent_type)", task.RuntimeID, expectedAgentType)
	}
}

func TestWSRouter_ChatSend_NoBinding(t *testing.T) {
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	daemonHub := newMockDaemonHub()
	clientHub := clientws.NewHub()

	ctx := context.Background()
	userID := "user-1"

	// Create a session without binding a runtime.
	session, err := chatSvc.CreateSession(ctx, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	router := NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, service.NewMessageQueue())

	// Simulate a chat:send message.
	sendPayload := protocol.ChatSendPayload{
		SessionID: session.ID,
		Content:   "Hello!",
	}
	payloadData, _ := json.Marshal(sendPayload)
	msg := protocol.Message{
		Type:    protocol.TypeChatSend,
		Payload: payloadData,
	}

	// This should not panic — it should send an error to the user.
	router.handleClientMessage(userID, msg)

	// Verify no message was sent to any daemon.
	if len(daemonHub.messages) > 0 {
		t.Error("expected no messages to daemon when no binding exists")
	}
}

func TestWSRouter_ChatSend_InvalidPayload(t *testing.T) {
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	daemonHub := newMockDaemonHub()
	clientHub := clientws.NewHub()

	router := NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, service.NewMessageQueue())

	// Send an invalid payload.
	msg := protocol.Message{
		Type:    protocol.TypeChatSend,
		Payload: json.RawMessage(`{invalid json`),
	}

	// Should not panic.
	router.handleClientMessage("user-1", msg)

	// Verify no messages sent to daemon.
	if len(daemonHub.messages) > 0 {
		t.Error("expected no messages to daemon on invalid payload")
	}
}

func TestWSRouter_ChatCancel_Success(t *testing.T) {
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	daemonHub := newMockDaemonHub()
	clientHub := clientws.NewHub()

	ctx := context.Background()
	userID := "user-1"

	// Create a session.
	session, err := chatSvc.CreateSession(ctx, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Register a daemon with a runtime.
	err = runtimeSvc.RegisterDaemon(ctx, service.DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   userID,
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterDaemon: %v", err)
	}

	// Get the runtime ID and bind it.
	runtimes, err := runtimeSvc.GetUserRuntimes(ctx, userID)
	if err != nil {
		t.Fatalf("GetUserRuntimes: %v", err)
	}
	runtimeID := runtimes[0].ID

	err = chatSvc.BindSessionRuntime(ctx, userID, session.ID, runtimeID, runtimeSvc)
	if err != nil {
		t.Fatalf("BindSessionRuntime: %v", err)
	}

	daemonHub.setOnline("daemon-1")

	router := NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, service.NewMessageQueue())

	// Simulate a chat:cancel message.
	cancelPayload := protocol.ChatCancelPayload{
		SessionID: session.ID,
	}
	payloadData, _ := json.Marshal(cancelPayload)
	msg := protocol.Message{
		Type:    protocol.TypeChatCancel,
		Payload: payloadData,
	}

	router.handleClientMessage(userID, msg)

	// Verify the cancel was forwarded to the daemon.
	daemonMsgs := daemonHub.getMessages("daemon-1")
	if len(daemonMsgs) == 0 {
		t.Fatal("expected cancel message to be relayed to daemon")
	}

	if daemonMsgs[0].Type != protocol.TypeChatCancel {
		t.Errorf("expected message type %q, got %q", protocol.TypeChatCancel, daemonMsgs[0].Type)
	}

	var cancelResult protocol.ChatCancelPayload
	if err := json.Unmarshal(daemonMsgs[0].Payload, &cancelResult); err != nil {
		t.Fatalf("unmarshal cancel payload: %v", err)
	}
	if cancelResult.SessionID != session.ID {
		t.Errorf("cancel.SessionID = %q, want %q", cancelResult.SessionID, session.ID)
	}
}

func TestWSRouter_ChatCancel_WrongUser(t *testing.T) {
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	daemonHub := newMockDaemonHub()
	clientHub := clientws.NewHub()

	ctx := context.Background()

	// Create a session for user-1.
	session, err := chatSvc.CreateSession(ctx, "user-1")
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	router := NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, service.NewMessageQueue())

	// user-2 tries to cancel user-1's session.
	cancelPayload := protocol.ChatCancelPayload{
		SessionID: session.ID,
	}
	payloadData, _ := json.Marshal(cancelPayload)
	msg := protocol.Message{
		Type:    protocol.TypeChatCancel,
		Payload: payloadData,
	}

	router.handleClientMessage("user-2", msg)

	// Verify no cancel was sent to any daemon.
	if len(daemonHub.messages) > 0 {
		t.Error("expected no messages to daemon when user doesn't own session")
	}
}

func TestWSRouter_BroadcastSessionCreated(t *testing.T) {
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	daemonHub := newMockDaemonHub()
	clientHub := clientws.NewHub()

	router := NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, service.NewMessageQueue())

	session := &service.ChatSession{
		ID:     "session-123",
		UserID: "user-1",
		Title:  "New Chat",
		Status: "active",
	}

	// This should not panic even without connected clients.
	router.BroadcastSessionCreated("user-1", session)
}

func TestWSRouter_BroadcastSessionDeleted(t *testing.T) {
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	daemonHub := newMockDaemonHub()
	clientHub := clientws.NewHub()

	router := NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, service.NewMessageQueue())

	// Should not panic.
	router.BroadcastSessionDeleted("user-1", "session-123")
}

func TestWSRouter_BroadcastSessionUpdated(t *testing.T) {
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	daemonHub := newMockDaemonHub()
	clientHub := clientws.NewHub()

	router := NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, service.NewMessageQueue())

	session := &service.ChatSession{
		ID:     "session-123",
		UserID: "user-1",
		Title:  "Renamed Chat",
		Status: "active",
	}

	// Should not panic.
	router.BroadcastSessionUpdated("user-1", session)
}

func TestWSRouter_BroadcastRuntimeStatus(t *testing.T) {
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	daemonHub := newMockDaemonHub()
	clientHub := clientws.NewHub()

	router := NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, service.NewMessageQueue())

	runtime := &service.Runtime{
		ID:        "rt-1",
		DaemonID:  "daemon-1",
		AgentType: "claude",
		Status:    "offline",
	}

	// Should not panic.
	router.BroadcastRuntimeStatus("user-1", runtime, "offline")
}
