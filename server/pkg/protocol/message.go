// Package protocol defines shared WebSocket message types and payload structures
// used for communication between the server, daemon, and client.
package protocol

import "encoding/json"

// Message is the typed envelope for all WebSocket messages.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Message type constants for daemon communication.
const (
	TypeDaemonRegister     = "daemon:register"
	TypeDaemonRegisterAck  = "daemon:register_ack"
	TypeDaemonRegisterErr  = "daemon:register_error"
	TypeDaemonHeartbeat    = "daemon:heartbeat"
	TypeDaemonHeartbeatAck = "daemon:heartbeat_ack"
)

// Message type constants for chat communication.
const (
	TypeChatSend   = "chat:send"
	TypeChatMsg    = "chat:message"
	TypeChatStream = "chat:stream"
	TypeChatDone   = "chat:done"
	TypeChatError  = "chat:error"
	TypeChatTask   = "chat:task"
	TypeChatCancel = "chat:cancel"
)

// Message type constants for session events.
const (
	TypeSessionCreated = "session:created"
	TypeSessionDeleted = "session:deleted"
	TypeSessionUpdated = "session:updated"
)

// Message type constants for runtime and connection events.
const (
	TypeRuntimeStatus  = "runtime:status"
	TypeConnectionPing = "connection:ping"
	TypeConnectionPong = "connection:pong"
)

// Error code constants for ChatErrorPayload.
const (
	ErrCodeValidation      = "validation_error"
	ErrCodeAuthentication  = "authentication_error"
	ErrCodeAuthorization   = "authorization_error"
	ErrCodeNotFound        = "not_found"
	ErrCodeAgentTimeout    = "agent_timeout"
	ErrCodeAgentError      = "agent_error"
	ErrCodeAgentUnavailable = "agent_unavailable"
	ErrCodePersistFailed   = "persist_failed"
	ErrCodeRateLimit       = "rate_limit"
	ErrCodeInternal        = "internal_error"
)

// DaemonRegisterPayload is sent by the daemon to register with the server.
type DaemonRegisterPayload struct {
	DaemonID string        `json:"daemon_id"`
	UserID   string        `json:"user_id"`
	Runtimes []RuntimeInfo `json:"runtimes"`
}

// RuntimeInfo describes a detected agent CLI on the daemon's machine.
type RuntimeInfo struct {
	AgentType  string `json:"agent_type"`
	BinaryPath string `json:"binary_path"`
	Version    string `json:"version"`
	Status     string `json:"status"` // "available" or "unavailable"
}

// ChatTaskPayload is sent from the server to the daemon to execute a chat task.
type ChatTaskPayload struct {
	SessionID string        `json:"session_id"`
	MessageID string        `json:"message_id"`
	Content   string        `json:"content"`
	History   []HistoryItem `json:"history"`
	RuntimeID string        `json:"runtime_id"`
}

// HistoryItem represents a single message in the conversation history.
type HistoryItem struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatStreamPayload is sent from the daemon to the server (and forwarded to the client)
// for each streaming token during agent response generation.
type ChatStreamPayload struct {
	SessionID string `json:"session_id"`
	Seq       int    `json:"seq"`
	Content   string `json:"content"`
}

// ChatDonePayload is sent from the daemon to the server when the agent completes its response.
type ChatDonePayload struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
	ElapsedMs int64  `json:"elapsed_ms"`
}

// ChatErrorPayload is sent when an error occurs during message processing.
type ChatErrorPayload struct {
	SessionID string `json:"session_id"`
	MessageID string `json:"message_id,omitempty"`
	Error     string `json:"error"`
	Code      string `json:"code"`
}

// ChatSendPayload is sent from the client to the server when a user sends a chat message.
type ChatSendPayload struct {
	SessionID string `json:"session_id"`
	Content   string `json:"content"`
}

// ChatCancelPayload is sent from the client to the server to cancel an in-progress response.
type ChatCancelPayload struct {
	SessionID string `json:"session_id"`
}

// SessionEventPayload is sent from the server to the client for session lifecycle events.
type SessionEventPayload struct {
	SessionID string `json:"session_id"`
	Title     string `json:"title,omitempty"`
	Status    string `json:"status,omitempty"`
}

// RuntimeStatusPayload is sent from the server to the client when a runtime's status changes.
type RuntimeStatusPayload struct {
	RuntimeID string `json:"runtime_id"`
	DaemonID  string `json:"daemon_id"`
	AgentType string `json:"agent_type"`
	Status    string `json:"status"`
}
