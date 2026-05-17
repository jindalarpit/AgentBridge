package executor

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

// mockSender collects messages sent by the handler for test assertions.
type mockSender struct {
	mu       sync.Mutex
	messages []protocol.Message
}

func (m *mockSender) send(msg protocol.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockSender) getMessages() []protocol.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]protocol.Message, len(m.messages))
	copy(result, m.messages)
	return result
}

func TestNewTaskHandler(t *testing.T) {
	executor := NewExecutor()
	sender := &mockSender{}
	resolver := func(runtimeID string) (string, bool) {
		return "/bin/echo", true
	}

	handler := NewTaskHandler(executor, sender.send, resolver)
	if handler == nil {
		t.Fatal("NewTaskHandler returned nil")
	}
	if handler.executor != executor {
		t.Error("executor not set correctly")
	}
	if handler.sendFn == nil {
		t.Error("sendFn not set")
	}
	if handler.runtimeResolver == nil {
		t.Error("runtimeResolver not set")
	}
}

func TestHandleMessage_IgnoresNonTaskMessages(t *testing.T) {
	executor := NewExecutor()
	sender := &mockSender{}
	resolver := func(runtimeID string) (string, bool) {
		return "/bin/echo", true
	}
	handler := NewTaskHandler(executor, sender.send, resolver)

	// Send a heartbeat message — should be ignored.
	msg := protocol.Message{
		Type:    protocol.TypeDaemonHeartbeat,
		Payload: json.RawMessage(`{}`),
	}
	handler.HandleMessage(msg)

	messages := sender.getMessages()
	if len(messages) != 0 {
		t.Errorf("expected no messages sent for non-task message, got %d", len(messages))
	}
}

func TestHandleMessage_InvalidPayload(t *testing.T) {
	executor := NewExecutor()
	sender := &mockSender{}
	resolver := func(runtimeID string) (string, bool) {
		return "/bin/echo", true
	}
	handler := NewTaskHandler(executor, sender.send, resolver)

	// Send a chat:task with invalid JSON payload.
	msg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: json.RawMessage(`{invalid json`),
	}
	handler.HandleMessage(msg)

	// Should not crash; no messages sent since unmarshal fails silently (logged).
	messages := sender.getMessages()
	if len(messages) != 0 {
		t.Errorf("expected no messages for invalid payload, got %d", len(messages))
	}
}

func TestHandleMessage_RuntimeNotFound(t *testing.T) {
	executor := NewExecutor()
	sender := &mockSender{}
	resolver := func(runtimeID string) (string, bool) {
		return "", false // runtime not found
	}
	handler := NewTaskHandler(executor, sender.send, resolver)

	payload := protocol.ChatTaskPayload{
		SessionID: "session-1",
		MessageID: "msg-1",
		Content:   "hello",
		RuntimeID: "unknown-runtime",
	}
	payloadData, _ := json.Marshal(payload)

	msg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: payloadData,
	}
	handler.HandleMessage(msg)

	// Should send a chat:error message.
	messages := sender.getMessages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 error message, got %d", len(messages))
	}
	if messages[0].Type != protocol.TypeChatError {
		t.Errorf("expected message type %s, got %s", protocol.TypeChatError, messages[0].Type)
	}

	var errPayload protocol.ChatErrorPayload
	if err := json.Unmarshal(messages[0].Payload, &errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if errPayload.SessionID != "session-1" {
		t.Errorf("expected session_id 'session-1', got '%s'", errPayload.SessionID)
	}
	if errPayload.Code != protocol.ErrCodeAgentUnavailable {
		t.Errorf("expected error code %s, got %s", protocol.ErrCodeAgentUnavailable, errPayload.Code)
	}
}

func TestHandleMessage_NilRuntimeResolver(t *testing.T) {
	executor := NewExecutor()
	sender := &mockSender{}
	handler := NewTaskHandler(executor, sender.send, nil)

	payload := protocol.ChatTaskPayload{
		SessionID: "session-1",
		MessageID: "msg-1",
		Content:   "hello",
		RuntimeID: "some-runtime",
	}
	payloadData, _ := json.Marshal(payload)

	msg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: payloadData,
	}
	handler.HandleMessage(msg)

	// Should send a chat:error because no resolver is configured.
	messages := sender.getMessages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 error message, got %d", len(messages))
	}
	if messages[0].Type != protocol.TypeChatError {
		t.Errorf("expected message type %s, got %s", protocol.TypeChatError, messages[0].Type)
	}
}

func TestHandleMessage_SuccessfulExecution(t *testing.T) {
	executor := NewExecutorWithTimeout(5 * time.Second)
	sender := &mockSender{}
	resolver := func(runtimeID string) (string, bool) {
		if runtimeID == "echo-runtime" {
			return "/bin/echo", true
		}
		return "", false
	}
	handler := NewTaskHandler(executor, sender.send, resolver)

	payload := protocol.ChatTaskPayload{
		SessionID: "session-1",
		MessageID: "msg-1",
		Content:   "hello world",
		RuntimeID: "echo-runtime",
		History:   []protocol.HistoryItem{},
	}
	payloadData, _ := json.Marshal(payload)

	msg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: payloadData,
	}
	handler.HandleMessage(msg)

	// Wait for the goroutine to complete streaming.
	time.Sleep(500 * time.Millisecond)

	messages := sender.getMessages()
	if len(messages) == 0 {
		t.Fatal("expected at least one message")
	}

	// Should have at least one stream message and one done message.
	var streamCount int
	var doneCount int
	for _, m := range messages {
		switch m.Type {
		case protocol.TypeChatStream:
			streamCount++
		case protocol.TypeChatDone:
			doneCount++
		}
	}

	if streamCount == 0 {
		t.Error("expected at least one chat:stream message")
	}
	if doneCount != 1 {
		t.Errorf("expected exactly 1 chat:done message, got %d", doneCount)
	}

	// Verify the done message content.
	for _, m := range messages {
		if m.Type == protocol.TypeChatDone {
			var donePayload protocol.ChatDonePayload
			if err := json.Unmarshal(m.Payload, &donePayload); err != nil {
				t.Fatalf("failed to unmarshal done payload: %v", err)
			}
			if donePayload.SessionID != "session-1" {
				t.Errorf("expected session_id 'session-1', got '%s'", donePayload.SessionID)
			}
			if donePayload.MessageID != "msg-1" {
				t.Errorf("expected message_id 'msg-1', got '%s'", donePayload.MessageID)
			}
			if donePayload.Content != "hello world" {
				t.Errorf("expected content 'hello world', got '%s'", donePayload.Content)
			}
			if donePayload.ElapsedMs <= 0 {
				t.Errorf("expected positive elapsed_ms, got %d", donePayload.ElapsedMs)
			}
		}
	}
}

func TestHandleMessage_StreamSequenceNumbers(t *testing.T) {
	executor := NewExecutorWithTimeout(5 * time.Second)
	sender := &mockSender{}
	resolver := func(runtimeID string) (string, bool) {
		if runtimeID == "printf-runtime" {
			return "/usr/bin/printf", true
		}
		return "", false
	}
	handler := NewTaskHandler(executor, sender.send, resolver)

	payload := protocol.ChatTaskPayload{
		SessionID: "session-seq",
		MessageID: "msg-seq",
		Content:   "line1\nline2\nline3\n",
		RuntimeID: "printf-runtime",
		History:   []protocol.HistoryItem{},
	}
	payloadData, _ := json.Marshal(payload)

	msg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: payloadData,
	}
	handler.HandleMessage(msg)

	// Wait for streaming to complete.
	time.Sleep(500 * time.Millisecond)

	messages := sender.getMessages()

	// Verify stream messages have monotonically increasing sequence numbers.
	prevSeq := 0
	for _, m := range messages {
		if m.Type == protocol.TypeChatStream {
			var streamPayload protocol.ChatStreamPayload
			if err := json.Unmarshal(m.Payload, &streamPayload); err != nil {
				t.Fatalf("failed to unmarshal stream payload: %v", err)
			}
			if streamPayload.Seq <= prevSeq {
				t.Errorf("sequence not monotonically increasing: prev=%d, current=%d", prevSeq, streamPayload.Seq)
			}
			prevSeq = streamPayload.Seq
		}
	}

	if prevSeq == 0 {
		t.Error("expected at least one stream message with a sequence number")
	}
}

func TestHandleMessage_ExecutionTimeout(t *testing.T) {
	// Use a very short timeout to trigger timeout behavior.
	executor := NewExecutorWithTimeout(100 * time.Millisecond)
	sender := &mockSender{}
	resolver := func(runtimeID string) (string, bool) {
		if runtimeID == "sleep-runtime" {
			return "/bin/sleep", true
		}
		return "", false
	}
	handler := NewTaskHandler(executor, sender.send, resolver)

	payload := protocol.ChatTaskPayload{
		SessionID: "session-timeout",
		MessageID: "msg-timeout",
		Content:   "10",
		RuntimeID: "sleep-runtime",
		History:   []protocol.HistoryItem{},
	}
	payloadData, _ := json.Marshal(payload)

	msg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: payloadData,
	}
	handler.HandleMessage(msg)

	// Wait for timeout to trigger.
	time.Sleep(500 * time.Millisecond)

	messages := sender.getMessages()
	if len(messages) == 0 {
		t.Fatal("expected at least one message (error)")
	}

	// Should have a chat:error message with timeout code.
	lastMsg := messages[len(messages)-1]
	if lastMsg.Type != protocol.TypeChatError {
		t.Errorf("expected last message type %s, got %s", protocol.TypeChatError, lastMsg.Type)
	}

	var errPayload protocol.ChatErrorPayload
	if err := json.Unmarshal(lastMsg.Payload, &errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if errPayload.Code != protocol.ErrCodeAgentTimeout {
		t.Errorf("expected error code %s, got %s", protocol.ErrCodeAgentTimeout, errPayload.Code)
	}
}

func TestHandleMessage_InvalidBinaryPath(t *testing.T) {
	executor := NewExecutorWithTimeout(5 * time.Second)
	sender := &mockSender{}
	resolver := func(runtimeID string) (string, bool) {
		return "/nonexistent/binary", true
	}
	handler := NewTaskHandler(executor, sender.send, resolver)

	payload := protocol.ChatTaskPayload{
		SessionID: "session-invalid-bin",
		MessageID: "msg-invalid-bin",
		Content:   "hello",
		RuntimeID: "bad-runtime",
		History:   []protocol.HistoryItem{},
	}
	payloadData, _ := json.Marshal(payload)

	msg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: payloadData,
	}
	handler.HandleMessage(msg)

	// The executor should fail to start the process and send an error.
	// Give a moment for the synchronous error path.
	time.Sleep(100 * time.Millisecond)

	messages := sender.getMessages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 error message, got %d", len(messages))
	}
	if messages[0].Type != protocol.TypeChatError {
		t.Errorf("expected message type %s, got %s", protocol.TypeChatError, messages[0].Type)
	}

	var errPayload protocol.ChatErrorPayload
	if err := json.Unmarshal(messages[0].Payload, &errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if errPayload.Code != protocol.ErrCodeAgentError {
		t.Errorf("expected error code %s, got %s", protocol.ErrCodeAgentError, errPayload.Code)
	}
}

func TestHandleMessage_WithHistory(t *testing.T) {
	executor := NewExecutorWithTimeout(5 * time.Second)
	sender := &mockSender{}
	resolver := func(runtimeID string) (string, bool) {
		if runtimeID == "echo-runtime" {
			return "/bin/echo", true
		}
		return "", false
	}
	handler := NewTaskHandler(executor, sender.send, resolver)

	payload := protocol.ChatTaskPayload{
		SessionID: "session-history",
		MessageID: "msg-history",
		Content:   "continue",
		RuntimeID: "echo-runtime",
		History: []protocol.HistoryItem{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
			{Role: "user", Content: "how are you?"},
		},
	}
	payloadData, _ := json.Marshal(payload)

	msg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: payloadData,
	}
	handler.HandleMessage(msg)

	// Wait for streaming to complete.
	time.Sleep(500 * time.Millisecond)

	messages := sender.getMessages()

	// Should complete successfully with a done message.
	var doneCount int
	for _, m := range messages {
		if m.Type == protocol.TypeChatDone {
			doneCount++
		}
	}
	if doneCount != 1 {
		t.Errorf("expected 1 chat:done message, got %d", doneCount)
	}
}

// mockServerConnection implements the ServerConnection interface for testing
// the RegisterWithConnection integration.
type mockServerConnection struct {
	mu       sync.Mutex
	messages []protocol.Message
	handler  func(protocol.Message)
}

func (c *mockServerConnection) Send(msg protocol.Message) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.messages = append(c.messages, msg)
	return nil
}

func (c *mockServerConnection) OnMessage(handler func(protocol.Message)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
}

func (c *mockServerConnection) getMessages() []protocol.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := make([]protocol.Message, len(c.messages))
	copy(result, c.messages)
	return result
}

func (c *mockServerConnection) dispatchMessage(msg protocol.Message) {
	c.mu.Lock()
	h := c.handler
	c.mu.Unlock()
	if h != nil {
		h(msg)
	}
}

func TestRegisterWithConnection(t *testing.T) {
	conn := &mockServerConnection{}
	executor := NewExecutorWithTimeout(5 * time.Second)
	resolver := func(runtimeID string) (string, bool) {
		if runtimeID == "echo-runtime" {
			return "/bin/echo", true
		}
		return "", false
	}

	handler := RegisterWithConnection(conn, executor, resolver)
	if handler == nil {
		t.Fatal("RegisterWithConnection returned nil")
	}

	// Verify the handler was registered as the message callback.
	conn.mu.Lock()
	hasHandler := conn.handler != nil
	conn.mu.Unlock()
	if !hasHandler {
		t.Fatal("expected OnMessage handler to be registered")
	}
}

func TestRegisterWithConnection_DispatchesTaskMessages(t *testing.T) {
	conn := &mockServerConnection{}
	executor := NewExecutorWithTimeout(5 * time.Second)
	resolver := func(runtimeID string) (string, bool) {
		if runtimeID == "echo-runtime" {
			return "/bin/echo", true
		}
		return "", false
	}

	RegisterWithConnection(conn, executor, resolver)

	// Simulate receiving a chat:task message from the server.
	payload := protocol.ChatTaskPayload{
		SessionID: "session-reg",
		MessageID: "msg-reg",
		Content:   "hello from connection",
		RuntimeID: "echo-runtime",
		History:   []protocol.HistoryItem{},
	}
	payloadData, _ := json.Marshal(payload)

	msg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: payloadData,
	}
	conn.dispatchMessage(msg)

	// Wait for the streaming goroutine to complete.
	time.Sleep(500 * time.Millisecond)

	messages := conn.getMessages()
	if len(messages) == 0 {
		t.Fatal("expected at least one message sent back through connection")
	}

	// Should have stream + done messages sent via the connection's Send method.
	var streamCount, doneCount int
	for _, m := range messages {
		switch m.Type {
		case protocol.TypeChatStream:
			streamCount++
		case protocol.TypeChatDone:
			doneCount++
		}
	}

	if streamCount == 0 {
		t.Error("expected at least one chat:stream message")
	}
	if doneCount != 1 {
		t.Errorf("expected exactly 1 chat:done message, got %d", doneCount)
	}

	// Verify the done message has correct content.
	for _, m := range messages {
		if m.Type == protocol.TypeChatDone {
			var donePayload protocol.ChatDonePayload
			if err := json.Unmarshal(m.Payload, &donePayload); err != nil {
				t.Fatalf("failed to unmarshal done payload: %v", err)
			}
			if donePayload.SessionID != "session-reg" {
				t.Errorf("expected session_id 'session-reg', got '%s'", donePayload.SessionID)
			}
			if donePayload.MessageID != "msg-reg" {
				t.Errorf("expected message_id 'msg-reg', got '%s'", donePayload.MessageID)
			}
			if donePayload.Content != "hello from connection" {
				t.Errorf("expected content 'hello from connection', got '%s'", donePayload.Content)
			}
			if donePayload.ElapsedMs <= 0 {
				t.Errorf("expected positive elapsed_ms, got %d", donePayload.ElapsedMs)
			}
		}
	}
}

func TestRegisterWithConnection_IgnoresNonTaskMessages(t *testing.T) {
	conn := &mockServerConnection{}
	executor := NewExecutorWithTimeout(5 * time.Second)
	resolver := func(runtimeID string) (string, bool) {
		return "/bin/echo", true
	}

	RegisterWithConnection(conn, executor, resolver)

	// Dispatch a non-task message.
	msg := protocol.Message{
		Type:    protocol.TypeDaemonHeartbeat,
		Payload: json.RawMessage(`{}`),
	}
	conn.dispatchMessage(msg)

	// No messages should be sent back.
	time.Sleep(100 * time.Millisecond)
	messages := conn.getMessages()
	if len(messages) != 0 {
		t.Errorf("expected no messages for non-task dispatch, got %d", len(messages))
	}
}

func TestRegisterWithConnection_ErrorOnUnknownRuntime(t *testing.T) {
	conn := &mockServerConnection{}
	executor := NewExecutorWithTimeout(5 * time.Second)
	resolver := func(runtimeID string) (string, bool) {
		return "", false // always not found
	}

	RegisterWithConnection(conn, executor, resolver)

	// Dispatch a task with an unknown runtime.
	payload := protocol.ChatTaskPayload{
		SessionID: "session-err",
		MessageID: "msg-err",
		Content:   "hello",
		RuntimeID: "unknown-runtime",
		History:   []protocol.HistoryItem{},
	}
	payloadData, _ := json.Marshal(payload)

	msg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: payloadData,
	}
	conn.dispatchMessage(msg)

	// Should send a chat:error back through the connection.
	time.Sleep(100 * time.Millisecond)
	messages := conn.getMessages()
	if len(messages) != 1 {
		t.Fatalf("expected 1 error message, got %d", len(messages))
	}
	if messages[0].Type != protocol.TypeChatError {
		t.Errorf("expected message type %s, got %s", protocol.TypeChatError, messages[0].Type)
	}

	var errPayload protocol.ChatErrorPayload
	if err := json.Unmarshal(messages[0].Payload, &errPayload); err != nil {
		t.Fatalf("failed to unmarshal error payload: %v", err)
	}
	if errPayload.Code != protocol.ErrCodeAgentUnavailable {
		t.Errorf("expected error code %s, got %s", protocol.ErrCodeAgentUnavailable, errPayload.Code)
	}
}
