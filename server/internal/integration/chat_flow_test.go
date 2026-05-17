// Package integration provides end-to-end integration tests that wire together
// the in-memory services, WebSocket hubs, and handlers to verify full system flows.
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/user/agentbridge/server/internal/clientws"
	"github.com/user/agentbridge/server/internal/daemonws"
	"github.com/user/agentbridge/server/internal/handler"
	"github.com/user/agentbridge/server/internal/service"
	"github.com/user/agentbridge/server/pkg/protocol"
)

// --- Mock implementations for capturing messages ---

// mockClientHub captures messages sent/broadcast to users without real WebSocket connections.
type mockClientHub struct {
	mu             sync.Mutex
	sent           []userMessage
	broadcast      []userMessage
	messageHandler clientws.MessageHandler
}

type userMessage struct {
	UserID  string
	Message protocol.Message
}

func (m *mockClientHub) HandleWebSocket(_ http.ResponseWriter, _ *http.Request, _ string) {}
func (m *mockClientHub) SendToUser(userID string, msg protocol.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, userMessage{UserID: userID, Message: msg})
}
func (m *mockClientHub) BroadcastToUser(userID string, msg protocol.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.broadcast = append(m.broadcast, userMessage{UserID: userID, Message: msg})
}
func (m *mockClientHub) ConnectionCount(_ string) int { return 1 }
func (m *mockClientHub) SetMessageHandler(fn clientws.MessageHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messageHandler = fn
}

func (m *mockClientHub) getSent() []userMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]userMessage, len(m.sent))
	copy(result, m.sent)
	return result
}

func (m *mockClientHub) getBroadcast() []userMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]userMessage, len(m.broadcast))
	copy(result, m.broadcast)
	return result
}

func (m *mockClientHub) simulateClientMessage(userID string, msg protocol.Message) {
	m.mu.Lock()
	handler := m.messageHandler
	m.mu.Unlock()
	if handler != nil {
		handler(userID, msg)
	}
}

// mockDaemonHub captures messages sent to daemons and allows simulating daemon messages/disconnects.
type mockDaemonHub struct {
	mu                  sync.Mutex
	messages            map[string][]protocol.Message // keyed by daemonID
	online              map[string]bool
	heartbeatHandler    daemonws.HeartbeatHandler
	registrationHandler daemonws.RegistrationHandler
	messageHandler      daemonws.MessageHandler
	disconnectHandler   daemonws.DisconnectHandler
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
		return &daemonOfflineErr{daemonID}
	}
	m.messages[daemonID] = append(m.messages[daemonID], msg)
	return nil
}
func (m *mockDaemonHub) IsOnline(daemonID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.online[daemonID]
}
func (m *mockDaemonHub) SetHeartbeatHandler(fn daemonws.HeartbeatHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeatHandler = fn
}
func (m *mockDaemonHub) SetRegistrationHandler(fn daemonws.RegistrationHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.registrationHandler = fn
}
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

func (m *mockDaemonHub) setOnline(daemonID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.online[daemonID] = true
}

func (m *mockDaemonHub) getMessages(daemonID string) []protocol.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]protocol.Message, len(m.messages[daemonID]))
	copy(result, m.messages[daemonID])
	return result
}

func (m *mockDaemonHub) simulateDaemonMessage(identity daemonws.DaemonIdentity, msg protocol.Message) {
	m.mu.Lock()
	handler := m.messageHandler
	m.mu.Unlock()
	if handler != nil {
		handler(identity, msg)
	}
}

func (m *mockDaemonHub) simulateDisconnect(identity daemonws.DaemonIdentity) {
	m.mu.Lock()
	handler := m.disconnectHandler
	m.mu.Unlock()
	if handler != nil {
		handler(identity)
	}
}

type daemonOfflineErr struct {
	daemonID string
}

func (e *daemonOfflineErr) Error() string {
	return "daemon not connected: " + e.daemonID
}

// --- Test harness ---

// testEnv wires together all components for integration testing.
type testEnv struct {
	chatSvc          *service.InMemoryChatService
	runtimeSvc       *service.InMemoryRuntimeService
	messageQueue     *service.MessageQueue
	clientHub        *mockClientHub
	daemonHub        *mockDaemonHub
	heartbeatChecker *daemonws.HeartbeatChecker
	daemonRelay      *service.DaemonRelay
	wsRouter         *handler.WSRouter
}

// setupTestEnv creates a fully wired test environment with a registered daemon,
// a session, and a bound runtime. Returns the env and the session ID.
func setupTestEnv(t *testing.T) (*testEnv, string) {
	t.Helper()

	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	messageQueue := service.NewMessageQueue()
	clientHub := &mockClientHub{}
	daemonHub := newMockDaemonHub()

	ctx := context.Background()
	userID := "user-1"
	daemonID := "daemon-1"

	// Register a daemon with a runtime.
	err := runtimeSvc.RegisterDaemon(ctx, service.DaemonRegistration{
		DaemonID: daemonID,
		UserID:   userID,
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"},
		},
	})
	if err != nil {
		t.Fatalf("RegisterDaemon: %v", err)
	}

	// Get the runtime ID and bind it to a session.
	runtimes, err := runtimeSvc.GetUserRuntimes(ctx, userID)
	if err != nil || len(runtimes) == 0 {
		t.Fatalf("GetUserRuntimes: %v (count: %d)", err, len(runtimes))
	}
	runtimeID := runtimes[0].ID

	session, err := chatSvc.CreateSession(ctx, userID)
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	err = chatSvc.BindSessionRuntime(ctx, userID, session.ID, runtimeID, runtimeSvc)
	if err != nil {
		t.Fatalf("BindSessionRuntime: %v", err)
	}

	// Mark daemon as online.
	daemonHub.setOnline(daemonID)

	// Heartbeat checker with a short interval for testing.
	heartbeatChecker := daemonws.NewHeartbeatChecker(100 * time.Millisecond)

	// Wire heartbeat handler.
	daemonHub.SetHeartbeatHandler(func(dID string) {
		heartbeatChecker.RecordHeartbeat(dID)
		_ = runtimeSvc.UpdateHeartbeat(ctx, dID)
	})

	// Wire registration handler.
	daemonHub.SetRegistrationHandler(func(identity daemonws.DaemonIdentity, payload protocol.DaemonRegisterPayload) error {
		reg := service.DaemonRegistration{
			DaemonID: payload.DaemonID,
			UserID:   identity.UserID,
			Runtimes: payload.Runtimes,
		}
		return runtimeSvc.RegisterDaemon(ctx, reg)
	})

	// Create the daemon relay (wires message + disconnect handlers on daemonHub).
	daemonRelay := service.NewDaemonRelay(daemonHub, clientHub, runtimeSvc, chatSvc)

	// Create the WS router (wires message handler on clientHub).
	wsRouter := handler.NewWSRouter(clientws.NewHub(), daemonHub, chatSvc, runtimeSvc, messageQueue)
	// Override the client hub's message handler to use our mock.
	clientHub.SetMessageHandler(wsRouter.HandleClientMessageForTest)

	// Wire drain queue callback.
	daemonRelay.SetOnResponseComplete(wsRouter.DrainQueue)

	// Record initial heartbeat so the daemon is tracked.
	heartbeatChecker.RecordHeartbeat(daemonID)

	env := &testEnv{
		chatSvc:          chatSvc,
		runtimeSvc:       runtimeSvc,
		messageQueue:     messageQueue,
		clientHub:        clientHub,
		daemonHub:        daemonHub,
		heartbeatChecker: heartbeatChecker,
		daemonRelay:      daemonRelay,
		wsRouter:         wsRouter,
	}

	return env, session.ID
}

// Compile-time interface checks.
var _ clientws.ClientHub = (*mockClientHub)(nil)
var _ daemonws.DaemonHub = (*mockDaemonHub)(nil)

// --- Integration Test 1: Full Chat Flow ---
// Validates: Requirements 6.1, 6.3, 6.5, 6.6
//
// Flow: user sends message → server persists → relays to daemon →
//       daemon streams back → client receives tokens → done persisted

func TestIntegration_FullChatFlow(t *testing.T) {
	env, sessionID := setupTestEnv(t)
	userID := "user-1"
	daemonID := "daemon-1"

	// Step 1: User sends a chat message via the client WebSocket.
	sendPayload, _ := json.Marshal(protocol.ChatSendPayload{
		SessionID: sessionID,
		Content:   "What is Go?",
	})
	clientMsg := protocol.Message{
		Type:    protocol.TypeChatSend,
		Payload: sendPayload,
	}
	env.clientHub.simulateClientMessage(userID, clientMsg)

	// Step 2: Verify the user message was persisted.
	messages, err := env.chatSvc.GetMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetMessages: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 persisted message, got %d", len(messages))
	}
	if messages[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", messages[0].Role)
	}
	if messages[0].Content != "What is Go?" {
		t.Errorf("expected content 'What is Go?', got %q", messages[0].Content)
	}
	if messages[0].Seq != 1 {
		t.Errorf("expected seq 1, got %d", messages[0].Seq)
	}

	// Step 3: Verify the message was relayed to the daemon as a chat:task.
	daemonMsgs := env.daemonHub.getMessages(daemonID)
	if len(daemonMsgs) != 1 {
		t.Fatalf("expected 1 message relayed to daemon, got %d", len(daemonMsgs))
	}
	if daemonMsgs[0].Type != protocol.TypeChatTask {
		t.Fatalf("expected chat:task, got %q", daemonMsgs[0].Type)
	}

	var task protocol.ChatTaskPayload
	if err := json.Unmarshal(daemonMsgs[0].Payload, &task); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	if task.SessionID != sessionID {
		t.Errorf("task.SessionID = %q, want %q", task.SessionID, sessionID)
	}
	if task.Content != "What is Go?" {
		t.Errorf("task.Content = %q, want %q", task.Content, "What is Go?")
	}

	// Step 4: Daemon streams tokens back.
	identity := daemonws.DaemonIdentity{DaemonID: daemonID, UserID: userID}
	tokens := []string{"Go ", "is ", "a ", "programming ", "language."}
	for i, token := range tokens {
		streamPayload, _ := json.Marshal(protocol.ChatStreamPayload{
			SessionID: sessionID,
			Seq:       i + 1,
			Content:   token,
		})
		streamMsg := protocol.Message{
			Type:    protocol.TypeChatStream,
			Payload: streamPayload,
		}
		env.daemonHub.simulateDaemonMessage(identity, streamMsg)
	}

	// Step 5: Verify client received all stream tokens.
	broadcasts := env.clientHub.getBroadcast()
	// First broadcast is the chat:message (persist confirmation), then 5 stream tokens.
	streamCount := 0
	for _, b := range broadcasts {
		if b.UserID == userID && b.Message.Type == protocol.TypeChatStream {
			streamCount++
		}
	}
	if streamCount != len(tokens) {
		t.Errorf("expected %d stream broadcasts, got %d", len(tokens), streamCount)
	}

	// Step 6: Daemon sends chat:done with the full response.
	fullContent := "Go is a programming language."
	donePayload, _ := json.Marshal(protocol.ChatDonePayload{
		SessionID: sessionID,
		MessageID: "assistant-msg-1",
		Content:   fullContent,
		ElapsedMs: 2500,
	})
	doneMsg := protocol.Message{
		Type:    protocol.TypeChatDone,
		Payload: donePayload,
	}
	env.daemonHub.simulateDaemonMessage(identity, doneMsg)

	// Step 7: Verify the assistant message was persisted.
	messages, err = env.chatSvc.GetMessages(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetMessages after done: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages (user + assistant), got %d", len(messages))
	}

	assistantMsg := messages[1]
	if assistantMsg.Role != "assistant" {
		t.Errorf("expected role 'assistant', got %q", assistantMsg.Role)
	}
	if assistantMsg.Content != fullContent {
		t.Errorf("expected content %q, got %q", fullContent, assistantMsg.Content)
	}
	if assistantMsg.Status != "complete" {
		t.Errorf("expected status 'complete', got %q", assistantMsg.Status)
	}
	if assistantMsg.ElapsedMs == nil || *assistantMsg.ElapsedMs != 2500 {
		t.Errorf("expected elapsed_ms 2500, got %v", assistantMsg.ElapsedMs)
	}
	if assistantMsg.Seq != 2 {
		t.Errorf("expected seq 2, got %d", assistantMsg.Seq)
	}

	// Step 8: Verify client received the chat:done broadcast.
	broadcasts = env.clientHub.getBroadcast()
	doneCount := 0
	for _, b := range broadcasts {
		if b.UserID == userID && b.Message.Type == protocol.TypeChatDone {
			doneCount++
		}
	}
	if doneCount != 1 {
		t.Errorf("expected 1 chat:done broadcast, got %d", doneCount)
	}
}

// --- Integration Test 2: Daemon Disconnect → Heartbeat Timeout → Offline ---
// Validates: Requirements 2.5, 6.6
//
// Flow: daemon disconnects → heartbeat timeout → runtimes marked offline → client notified

func TestIntegration_DaemonDisconnect_HeartbeatTimeout(t *testing.T) {
	env, _ := setupTestEnv(t)
	userID := "user-1"
	daemonID := "daemon-1"

	// Start the heartbeat checker with a short interval (100ms).
	// Timeout threshold = 3 × 100ms = 300ms.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	offlineCh := make(chan string, 1)
	env.heartbeatChecker.Start(ctx, func(dID string) {
		_ = env.runtimeSvc.MarkOffline(context.Background(), dID)
		offlineCh <- dID
	})
	defer env.heartbeatChecker.Stop()

	// Verify daemon is initially online.
	daemon, err := env.runtimeSvc.GetDaemonByID(context.Background(), daemonID)
	if err != nil {
		t.Fatalf("GetDaemonByID: %v", err)
	}
	if daemon.Status != "online" {
		t.Fatalf("expected daemon status 'online', got %q", daemon.Status)
	}

	// Verify runtimes are initially available.
	runtimes, err := env.runtimeSvc.GetUserRuntimes(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetUserRuntimes: %v", err)
	}
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 available runtime, got %d", len(runtimes))
	}

	// Simulate daemon disconnect via the disconnect handler.
	identity := daemonws.DaemonIdentity{DaemonID: daemonID, UserID: userID}
	env.daemonHub.simulateDisconnect(identity)

	// Verify daemon was immediately marked offline by the disconnect handler.
	daemon, err = env.runtimeSvc.GetDaemonByID(context.Background(), daemonID)
	if err != nil {
		t.Fatalf("GetDaemonByID after disconnect: %v", err)
	}
	if daemon.Status != "offline" {
		t.Errorf("expected daemon status 'offline' after disconnect, got %q", daemon.Status)
	}

	// Verify runtimes are now offline (no available runtimes for user).
	runtimes, err = env.runtimeSvc.GetUserRuntimes(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetUserRuntimes after disconnect: %v", err)
	}
	if len(runtimes) != 0 {
		t.Errorf("expected 0 available runtimes after disconnect, got %d", len(runtimes))
	}

	// Verify client was notified of the runtime going offline (runtime:status broadcast).
	broadcasts := env.clientHub.getBroadcast()
	runtimeStatusCount := 0
	for _, b := range broadcasts {
		if b.UserID == userID && b.Message.Type == protocol.TypeRuntimeStatus {
			runtimeStatusCount++
			// Verify the payload contains the correct daemon_id and status.
			var statusPayload struct {
				DaemonID string `json:"daemon_id"`
				Status   string `json:"status"`
			}
			if err := json.Unmarshal(b.Message.Payload, &statusPayload); err != nil {
				t.Errorf("failed to unmarshal runtime:status payload: %v", err)
				continue
			}
			if statusPayload.DaemonID != daemonID {
				t.Errorf("expected daemon_id %q in runtime:status, got %q", daemonID, statusPayload.DaemonID)
			}
			if statusPayload.Status != "offline" {
				t.Errorf("expected status 'offline' in runtime:status, got %q", statusPayload.Status)
			}
		}
	}
	if runtimeStatusCount == 0 {
		t.Error("expected at least 1 runtime:status broadcast to client, got 0")
	}

	// Also verify via heartbeat timeout path: wait for the heartbeat checker to fire.
	// Since we already marked offline via disconnect handler, the heartbeat checker
	// should also detect the timeout (daemon hasn't sent heartbeats).
	select {
	case offlineID := <-offlineCh:
		if offlineID != daemonID {
			t.Errorf("heartbeat timeout for unexpected daemon: %q (want %q)", offlineID, daemonID)
		}
	case <-time.After(500 * time.Millisecond):
		// The heartbeat checker should fire within 3 × 100ms = 300ms.
		t.Error("heartbeat timeout did not fire within expected window")
	}
}
