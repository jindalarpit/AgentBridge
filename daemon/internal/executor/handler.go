// Package executor handles agent CLI invocation and output streaming.
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

// ServerConnection defines the interface for the daemon's WebSocket connection
// to the server. This is a subset of the full connection.ServerConnection interface,
// used here to avoid circular imports.
type ServerConnection interface {
	Send(msg protocol.Message) error
	OnMessage(handler func(protocol.Message))
}

// SendFunc is a function type for sending protocol messages back to the server.
type SendFunc func(protocol.Message) error

// RuntimeResolver is a function type that resolves a runtime ID to its binary path.
// Returns the binary path and true if found, or empty string and false if not found.
type RuntimeResolver func(runtimeID string) (binaryPath string, ok bool)

// TaskHandler handles incoming chat:task messages from the server,
// invokes the AgentExecutor, and streams results back.
type TaskHandler struct {
	executor        *Executor
	sendFn          SendFunc
	runtimeResolver RuntimeResolver
}

// NewTaskHandler creates a new TaskHandler with the given executor and send function.
// If runtimeResolver is nil, the handler will return an error for tasks that require
// runtime resolution.
func NewTaskHandler(executor *Executor, sendFn SendFunc, runtimeResolver RuntimeResolver) *TaskHandler {
	return &TaskHandler{
		executor:        executor,
		sendFn:          sendFn,
		runtimeResolver: runtimeResolver,
	}
}

// HandleMessage processes an incoming protocol message. If the message type is
// chat:task, it unmarshals the payload and starts task execution. Other message
// types are ignored.
func (h *TaskHandler) HandleMessage(msg protocol.Message) {
	if msg.Type != protocol.TypeChatTask {
		return
	}

	var payload protocol.ChatTaskPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("handler: failed to unmarshal chat:task payload: %v", err)
		return
	}

	// Resolve the runtime ID to a binary path.
	binaryPath, err := h.resolveBinaryPath(payload.RuntimeID)
	if err != nil {
		h.sendError(payload.SessionID, payload.MessageID, err.Error(), protocol.ErrCodeAgentUnavailable)
		return
	}

	// Build the execution request.
	req := ExecutionRequest{
		SessionID:  payload.SessionID,
		RuntimeID:  payload.RuntimeID,
		BinaryPath: binaryPath,
		Content:    payload.Content,
		History:    payload.History,
	}

	// Execute the agent CLI.
	tokenCh, err := h.executor.Execute(context.Background(), req)
	if err != nil {
		h.sendError(payload.SessionID, payload.MessageID, err.Error(), protocol.ErrCodeAgentError)
		return
	}

	// Stream tokens back to the server in a goroutine.
	go h.streamTokens(payload.SessionID, payload.MessageID, tokenCh)
}

// resolveBinaryPath resolves a runtime ID to a binary path using the configured resolver.
func (h *TaskHandler) resolveBinaryPath(runtimeID string) (string, error) {
	if h.runtimeResolver == nil {
		return "", fmt.Errorf("no runtime resolver configured")
	}

	binaryPath, ok := h.runtimeResolver(runtimeID)
	if !ok {
		return "", fmt.Errorf("runtime %s not found or unavailable", runtimeID)
	}
	if binaryPath == "" {
		return "", fmt.Errorf("runtime %s has no binary path", runtimeID)
	}

	return binaryPath, nil
}

// streamTokens reads tokens from the channel and sends them as protocol messages.
// For regular tokens, it parses Claude stream-json format to extract text content.
// On Done, it sends chat:done. On Error, it sends chat:error.
func (h *TaskHandler) streamTokens(sessionID, messageID string, tokenCh <-chan StreamToken) {
	var contentParts []string

	for token := range tokenCh {
		if token.Error != nil {
			// Send error message to server.
			errCode := protocol.ErrCodeAgentError
			if strings.Contains(token.Error.Error(), "timeout") {
				errCode = protocol.ErrCodeAgentTimeout
			}
			h.sendError(sessionID, messageID, token.Error.Error(), errCode)
			return
		}

		if token.Done {
			// Send chat:done with the full concatenated content.
			fullContent := strings.Join(contentParts, "")
			h.sendDone(sessionID, messageID, fullContent, token.ElapsedMs)
			return
		}

		// Try to extract text from Claude's stream-json format.
		text := extractTextFromStreamJSON(token.Content)
		if text == "" {
			continue // Skip non-text lines (init, rate_limit, thinking, etc.)
		}

		// Accumulate content for the final done message.
		contentParts = append(contentParts, text)

		// Send streaming token to server.
		h.sendStream(sessionID, token.Seq, text)
	}
}

// sendStream sends a chat:stream message with the given token content.
func (h *TaskHandler) sendStream(sessionID string, seq int, content string) {
	payload := protocol.ChatStreamPayload{
		SessionID: sessionID,
		Seq:       seq,
		Content:   content,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("handler: failed to marshal stream payload: %v", err)
		return
	}

	msg := protocol.Message{
		Type:    protocol.TypeChatStream,
		Payload: data,
	}

	if err := h.sendFn(msg); err != nil {
		log.Printf("handler: failed to send stream message: %v", err)
	}
}

// sendDone sends a chat:done message indicating the agent response is complete.
func (h *TaskHandler) sendDone(sessionID, messageID, content string, elapsedMs int64) {
	payload := protocol.ChatDonePayload{
		SessionID: sessionID,
		MessageID: messageID,
		Content:   content,
		ElapsedMs: elapsedMs,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("handler: failed to marshal done payload: %v", err)
		return
	}

	msg := protocol.Message{
		Type:    protocol.TypeChatDone,
		Payload: data,
	}

	if err := h.sendFn(msg); err != nil {
		log.Printf("handler: failed to send done message: %v", err)
	}
}

// sendError sends a chat:error message indicating a failure during execution.
func (h *TaskHandler) sendError(sessionID, messageID, errMsg, code string) {
	payload := protocol.ChatErrorPayload{
		SessionID: sessionID,
		MessageID: messageID,
		Error:     errMsg,
		Code:      code,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("handler: failed to marshal error payload: %v", err)
		return
	}

	msg := protocol.Message{
		Type:    protocol.TypeChatError,
		Payload: data,
	}

	if err := h.sendFn(msg); err != nil {
		log.Printf("handler: failed to send error message: %v", err)
	}
}

// RegisterWithConnection wires the TaskHandler to listen for incoming messages
// on the given ServerConnection. It registers HandleMessage as the connection's
// message callback and uses the connection's Send method for outgoing messages.
//
// This is the primary integration point between the daemon's WebSocket connection
// and the task execution pipeline. Once registered, any chat:task message received
// from the server will be automatically dispatched to the handler for execution.
func RegisterWithConnection(conn ServerConnection, executor *Executor, runtimeResolver RuntimeResolver) *TaskHandler {
	handler := NewTaskHandler(executor, conn.Send, runtimeResolver)
	conn.OnMessage(handler.HandleMessage)
	return handler
}

// extractTextFromStreamJSON parses a line of Claude's stream-json output and
// extracts the text content. Returns empty string for non-text events (init,
// rate_limit, thinking, system messages).
//
// Claude stream-json format produces lines like:
//   {"type":"result","result":"Hello!","stop_reason":"end_turn",...}
//   {"type":"assistant","message":{"content":[{"type":"text","text":"Hello!"}],...}}
//
// We extract text from:
// 1. "result" type events → use the "result" field
// 2. "assistant" type events with text content → use content[].text where type=="text"
func extractTextFromStreamJSON(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}

	// Try to parse as JSON.
	var event map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &event); err != nil {
		// Not JSON — return as-is (might be plain text from non-Claude agents).
		return line
	}

	// Get the event type.
	var eventType string
	if raw, ok := event["type"]; ok {
		json.Unmarshal(raw, &eventType)
	}

	switch eventType {
	case "result":
		// Extract the "result" field which contains the final text.
		var result string
		if raw, ok := event["result"]; ok {
			json.Unmarshal(raw, &result)
		}
		return result

	case "assistant":
		// Extract text from message.content[].text where content type is "text".
		// Only extract if stop_reason is present (final message, not intermediate).
		var stopReason json.RawMessage
		if msgRaw, ok := event["message"]; ok {
			var msgObj map[string]json.RawMessage
			if json.Unmarshal(msgRaw, &msgObj) == nil {
				stopReason = msgObj["stop_reason"]
			}
		}
		// Skip intermediate assistant messages (stop_reason is null)
		if stopReason == nil || string(stopReason) == "null" {
			return ""
		}
		var msg struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if raw, ok := event["message"]; ok {
			json.Unmarshal(raw, &msg)
		}
		var texts []string
		for _, c := range msg.Content {
			if c.Type == "text" && c.Text != "" {
				texts = append(texts, c.Text)
			}
		}
		return strings.Join(texts, "")

	case "system", "rate_limit_event":
		// Skip system/rate-limit events.
		return ""

	default:
		// Unknown type — skip.
		return ""
	}
}
