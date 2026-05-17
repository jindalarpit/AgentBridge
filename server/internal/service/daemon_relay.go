package service

import (
	"context"
	"encoding/json"
	"log"

	"github.com/user/agentbridge/server/internal/clientws"
	"github.com/user/agentbridge/server/internal/daemonws"
	"github.com/user/agentbridge/server/pkg/protocol"
)

// DaemonRelay routes incoming daemon WebSocket messages to the appropriate
// services and forwards chat events to connected clients. It also handles
// daemon disconnect events by marking daemons offline and notifying affected users.
type DaemonRelay struct {
	daemonHub      daemonws.DaemonHub
	clientHub      clientws.ClientHub
	runtimeService *InMemoryRuntimeService
	chatService    *InMemoryChatService
	// onResponseComplete is called when a session's response completes (chat:done or chat:error).
	// This allows the message queue to drain and deliver the next queued message.
	onResponseComplete func(sessionID string)
}

// NewDaemonRelay creates a new DaemonRelay and wires it into the DaemonHub's
// message handler and disconnect handler.
func NewDaemonRelay(
	daemonHub daemonws.DaemonHub,
	clientHub clientws.ClientHub,
	runtimeService *InMemoryRuntimeService,
	chatService *InMemoryChatService,
) *DaemonRelay {
	relay := &DaemonRelay{
		daemonHub:      daemonHub,
		clientHub:      clientHub,
		runtimeService: runtimeService,
		chatService:    chatService,
	}

	// Wire the message handler for chat messages from daemons.
	daemonHub.SetMessageHandler(relay.handleDaemonMessage)

	// Wire the disconnect handler for daemon disconnections.
	daemonHub.SetDisconnectHandler(relay.handleDaemonDisconnect)

	return relay
}

// SetOnResponseComplete sets the callback invoked when a session's response
// completes (chat:done or chat:error). This is used by the message queue to
// drain and deliver the next queued message.
func (r *DaemonRelay) SetOnResponseComplete(fn func(sessionID string)) {
	r.onResponseComplete = fn
}

// handleDaemonMessage routes incoming messages from a daemon based on type.
func (r *DaemonRelay) handleDaemonMessage(identity daemonws.DaemonIdentity, msg protocol.Message) {
	switch msg.Type {
	case protocol.TypeChatStream:
		r.handleChatStream(identity, msg)

	case protocol.TypeChatDone:
		r.handleChatDone(identity, msg)

	case protocol.TypeChatError:
		r.handleChatError(identity, msg)

	default:
		log.Printf("daemon_relay: unhandled message type %q from daemon %s", msg.Type, identity.DaemonID)
	}
}

// handleChatStream forwards a streaming token from the daemon to the owning user's client.
func (r *DaemonRelay) handleChatStream(identity daemonws.DaemonIdentity, msg protocol.Message) {
	var payload protocol.ChatStreamPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("daemon_relay: invalid chat:stream payload from daemon %s: %v", identity.DaemonID, err)
		return
	}

	// Look up the user who owns this session.
	userID, err := r.chatService.GetSessionOwner(context.Background(), payload.SessionID)
	if err != nil {
		log.Printf("daemon_relay: session %s not found for chat:stream from daemon %s: %v",
			payload.SessionID, identity.DaemonID, err)
		return
	}

	// Forward the stream message to the user's client connections.
	r.clientHub.BroadcastToUser(userID, msg)
}

// handleChatDone persists the assistant message and forwards the done event to the client.
func (r *DaemonRelay) handleChatDone(identity daemonws.DaemonIdentity, msg protocol.Message) {
	var payload protocol.ChatDonePayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("daemon_relay: invalid chat:done payload from daemon %s: %v", identity.DaemonID, err)
		return
	}

	// Look up the user who owns this session.
	userID, err := r.chatService.GetSessionOwner(context.Background(), payload.SessionID)
	if err != nil {
		log.Printf("daemon_relay: session %s not found for chat:done from daemon %s: %v",
			payload.SessionID, identity.DaemonID, err)
		return
	}

	// Persist the assistant message.
	_, err = r.chatService.PersistAssistantMessage(
		context.Background(),
		payload.SessionID,
		payload.MessageID,
		payload.Content,
		payload.ElapsedMs,
	)
	if err != nil {
		log.Printf("daemon_relay: failed to persist assistant message for session %s: %v",
			payload.SessionID, err)
		// Still forward the done event to the client so they see the response,
		// even if persistence failed. Log the error for investigation.
	}

	// Forward the done message to the user's client connections.
	r.clientHub.BroadcastToUser(userID, msg)

	// Notify the message queue that the response is complete so it can drain.
	if r.onResponseComplete != nil {
		r.onResponseComplete(payload.SessionID)
	}
}

// handleChatError updates the message status and forwards the error to the client.
func (r *DaemonRelay) handleChatError(identity daemonws.DaemonIdentity, msg protocol.Message) {
	var payload protocol.ChatErrorPayload
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		log.Printf("daemon_relay: invalid chat:error payload from daemon %s: %v", identity.DaemonID, err)
		return
	}

	// Look up the user who owns this session.
	userID, err := r.chatService.GetSessionOwner(context.Background(), payload.SessionID)
	if err != nil {
		log.Printf("daemon_relay: session %s not found for chat:error from daemon %s: %v",
			payload.SessionID, identity.DaemonID, err)
		return
	}

	// Record the error in the message store.
	if markErr := r.chatService.MarkMessageError(
		context.Background(),
		payload.SessionID,
		payload.MessageID,
		payload.Error,
		payload.Code,
	); markErr != nil {
		log.Printf("daemon_relay: failed to mark message error for session %s: %v",
			payload.SessionID, markErr)
	}

	// Forward the error message to the user's client connections.
	r.clientHub.BroadcastToUser(userID, msg)

	// Notify the message queue that the response is complete (failed) so it can drain.
	if r.onResponseComplete != nil {
		r.onResponseComplete(payload.SessionID)
	}
}

// handleDaemonDisconnect marks the daemon offline and notifies affected users.
func (r *DaemonRelay) handleDaemonDisconnect(identity daemonws.DaemonIdentity) {
	ctx := context.Background()

	// Mark the daemon and its runtimes as offline.
	if err := r.runtimeService.MarkOffline(ctx, identity.DaemonID); err != nil {
		log.Printf("daemon_relay: failed to mark daemon %s offline: %v", identity.DaemonID, err)
		// Continue to notify the user even if the state update fails.
	}

	// Notify the user that their daemon's runtimes are now offline.
	// Send a runtime:status message to the user's client connections.
	statusPayload, err := json.Marshal(struct {
		DaemonID string `json:"daemon_id"`
		Status   string `json:"status"`
	}{
		DaemonID: identity.DaemonID,
		Status:   "offline",
	})
	if err != nil {
		log.Printf("daemon_relay: failed to marshal runtime status for daemon %s: %v", identity.DaemonID, err)
		return
	}

	statusMsg := protocol.Message{
		Type:    protocol.TypeRuntimeStatus,
		Payload: statusPayload,
	}

	r.clientHub.BroadcastToUser(identity.UserID, statusMsg)

	log.Printf("daemon_relay: daemon %s disconnected, marked offline and notified user %s",
		identity.DaemonID, identity.UserID)
}
