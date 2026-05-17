// Package handler provides HTTP route handlers and WebSocket message routing
// for the AgentBridge server.
package handler

import (
	"context"
	"encoding/json"
	"log"

	"github.com/user/agentbridge/server/internal/clientws"
	"github.com/user/agentbridge/server/internal/daemonws"
	"github.com/user/agentbridge/server/internal/service"
	"github.com/user/agentbridge/server/pkg/protocol"
)

// WSRouter routes incoming WebSocket messages from clients to the appropriate
// service methods and broadcasts server-side events to connected users.
type WSRouter struct {
	clientHub    *clientws.Hub
	daemonHub    daemonws.DaemonHub
	chatSvc      *service.InMemoryChatService
	runtimeSvc   *service.InMemoryRuntimeService
	messageQueue *service.MessageQueue
}

// NewWSRouter creates a new WSRouter and registers itself as the ClientHub's
// message handler. It wires incoming client messages to the appropriate service
// methods.
func NewWSRouter(
	clientHub *clientws.Hub,
	daemonHub daemonws.DaemonHub,
	chatSvc *service.InMemoryChatService,
	runtimeSvc *service.InMemoryRuntimeService,
	messageQueue *service.MessageQueue,
) *WSRouter {
	r := &WSRouter{
		clientHub:    clientHub,
		daemonHub:    daemonHub,
		chatSvc:      chatSvc,
		runtimeSvc:   runtimeSvc,
		messageQueue: messageQueue,
	}

	// Register as the ClientHub's message handler.
	clientHub.SetMessageHandler(r.handleClientMessage)

	return r
}

// MessageQueue returns the message queue used by this router.
// This is exposed so the DaemonRelay can drain queued messages on chat:done/chat:error.
func (r *WSRouter) MessageQueue() *service.MessageQueue {
	return r.messageQueue
}

// HandleClientMessageForTest is an exported version of handleClientMessage
// for use in integration tests where a mock ClientHub needs to invoke the
// router's message handling logic directly.
func (r *WSRouter) HandleClientMessageForTest(userID string, msg protocol.Message) {
	r.handleClientMessage(userID, msg)
}

// handleClientMessage is the callback invoked by ClientHub for every incoming
// message from a connected client. It dispatches based on message type.
func (r *WSRouter) handleClientMessage(userID string, msg protocol.Message) {
	switch msg.Type {
	case protocol.TypeChatSend:
		r.handleChatSend(userID, msg.Payload)

	case protocol.TypeChatCancel:
		r.handleChatCancel(userID, msg.Payload)

	default:
		log.Printf("ws_router: unhandled client message type %q from user %s", msg.Type, userID)
	}
}

// handleChatSend processes a chat:send message from a client.
// It extracts session_id and content, calls ChatService.SendMessage to persist
// the message, then builds and relays a chat:task payload to the daemon.
// If a response is already in progress for the session, the message is queued
// and delivered after the current response completes or fails (FIFO order).
func (r *WSRouter) handleChatSend(userID string, payload json.RawMessage) {
	var send protocol.ChatSendPayload
	if err := json.Unmarshal(payload, &send); err != nil {
		r.sendErrorToUser(userID, "", "", "invalid chat:send payload: "+err.Error(), protocol.ErrCodeValidation)
		return
	}

	ctx := context.Background()

	// Persist the user message via ChatService.
	msg, err := r.chatSvc.SendMessage(ctx, userID, send.SessionID, send.Content)
	if err != nil {
		code := protocol.ErrCodeInternal
		switch err {
		case service.ErrSessionNotFound:
			code = protocol.ErrCodeNotFound
		case service.ErrForbidden:
			code = protocol.ErrCodeAuthorization
		case service.ErrInvalidMessage:
			code = protocol.ErrCodeValidation
		}
		r.sendErrorToUser(userID, send.SessionID, "", err.Error(), code)
		return
	}

	// Notify the user that the message was persisted.
	r.sendMessageEvent(userID, msg)

	// Build the task payload for relay to the daemon.
	taskPayload, err := r.chatSvc.BuildTaskPayload(ctx, send.SessionID, msg)
	if err != nil {
		r.sendErrorToUser(userID, send.SessionID, msg.ID, "failed to build task payload: "+err.Error(), protocol.ErrCodeInternal)
		return
	}

	// Look up the daemon for this session's bound runtime.
	runtimeID, err := r.runtimeSvc.GetSessionBinding(ctx, send.SessionID)
	if err != nil {
		r.sendErrorToUser(userID, send.SessionID, msg.ID, "no agent bound to session", protocol.ErrCodeAgentUnavailable)
		return
	}

	runtime, err := r.runtimeSvc.GetRuntimeByID(ctx, runtimeID)
	if err != nil {
		r.sendErrorToUser(userID, send.SessionID, msg.ID, "bound runtime not found", protocol.ErrCodeAgentUnavailable)
		return
	}

	// Override the RuntimeID in the task payload with the agent_type so the daemon
	// can resolve it to a binary path. The daemon maps by agent_type, not by
	// server-generated runtime IDs.
	taskPayload.RuntimeID = runtime.AgentType

	// Try to enqueue the message. If a response is in progress, it gets queued.
	queued := &service.QueuedMessage{
		UserID:    userID,
		SessionID: send.SessionID,
		Message:   msg,
		Task:      taskPayload,
		RuntimeID: runtimeID,
		DaemonID:  runtime.DaemonID,
	}

	if r.messageQueue.TryEnqueue(queued) {
		// Message was queued — it will be delivered when the current response completes.
		log.Printf("ws_router: queued message %s for session %s (response in progress)", msg.ID, send.SessionID)
		return
	}

	// No response in progress — send immediately.
	r.relayTaskToDaemon(queued)
}

// relayTaskToDaemon sends a chat:task message to the daemon for a queued message.
func (r *WSRouter) relayTaskToDaemon(queued *service.QueuedMessage) {
	taskData, err := json.Marshal(queued.Task)
	if err != nil {
		r.sendErrorToUser(queued.UserID, queued.SessionID, queued.Message.ID, "failed to marshal task payload", protocol.ErrCodeInternal)
		return
	}

	taskMsg := protocol.Message{
		Type:    protocol.TypeChatTask,
		Payload: taskData,
	}

	if err := r.daemonHub.SendToDaemon(queued.DaemonID, taskMsg); err != nil {
		r.sendErrorToUser(queued.UserID, queued.SessionID, queued.Message.ID, "agent is not connected", protocol.ErrCodeAgentUnavailable)
		// Mark session as idle since we couldn't deliver.
		r.messageQueue.Dequeue(queued.SessionID)
	}
}

// DrainQueue is called when a response completes (chat:done or chat:error) to
// deliver the next queued message for the session, if any.
func (r *WSRouter) DrainQueue(sessionID string) {
	next := r.messageQueue.Dequeue(sessionID)
	if next == nil {
		return
	}

	// Deliver the next queued message to the daemon.
	log.Printf("ws_router: draining queue for session %s, delivering message %s", sessionID, next.Message.ID)
	r.relayTaskToDaemon(next)
}

// handleChatCancel processes a chat:cancel message from a client.
// It extracts the session_id, looks up the daemon for that session's bound
// runtime, and forwards the cancel message to the daemon via DaemonHub.
func (r *WSRouter) handleChatCancel(userID string, payload json.RawMessage) {
	var cancel protocol.ChatCancelPayload
	if err := json.Unmarshal(payload, &cancel); err != nil {
		r.sendErrorToUser(userID, "", "", "invalid chat:cancel payload: "+err.Error(), protocol.ErrCodeValidation)
		return
	}

	ctx := context.Background()

	// Verify the user owns this session.
	_, err := r.chatSvc.GetSession(ctx, userID, cancel.SessionID)
	if err != nil {
		code := protocol.ErrCodeNotFound
		if err == service.ErrForbidden {
			code = protocol.ErrCodeAuthorization
		}
		r.sendErrorToUser(userID, cancel.SessionID, "", err.Error(), code)
		return
	}

	// Look up the daemon for this session's bound runtime.
	runtimeID, err := r.runtimeSvc.GetSessionBinding(ctx, cancel.SessionID)
	if err != nil {
		// No binding — nothing to cancel.
		return
	}

	runtime, err := r.runtimeSvc.GetRuntimeByID(ctx, runtimeID)
	if err != nil {
		return
	}

	// Forward the cancel to the daemon.
	cancelData, err := json.Marshal(cancel)
	if err != nil {
		return
	}

	cancelMsg := protocol.Message{
		Type:    protocol.TypeChatCancel,
		Payload: cancelData,
	}

	if err := r.daemonHub.SendToDaemon(runtime.DaemonID, cancelMsg); err != nil {
		log.Printf("ws_router: failed to send cancel to daemon %s: %v", runtime.DaemonID, err)
	}
}

// BroadcastSessionCreated notifies all of a user's connected clients that a
// new session was created.
func (r *WSRouter) BroadcastSessionCreated(userID string, session *service.ChatSession) {
	payload := protocol.SessionEventPayload{
		SessionID: session.ID,
		Title:     session.Title,
		Status:    session.Status,
	}
	r.broadcastEvent(userID, protocol.TypeSessionCreated, payload)
}

// BroadcastSessionDeleted notifies all of a user's connected clients that a
// session was deleted.
func (r *WSRouter) BroadcastSessionDeleted(userID, sessionID string) {
	payload := protocol.SessionEventPayload{
		SessionID: sessionID,
	}
	r.broadcastEvent(userID, protocol.TypeSessionDeleted, payload)
}

// BroadcastSessionUpdated notifies all of a user's connected clients that a
// session was updated (e.g., renamed or binding changed).
func (r *WSRouter) BroadcastSessionUpdated(userID string, session *service.ChatSession) {
	payload := protocol.SessionEventPayload{
		SessionID: session.ID,
		Title:     session.Title,
		Status:    session.Status,
	}
	r.broadcastEvent(userID, protocol.TypeSessionUpdated, payload)
}

// BroadcastRuntimeStatus notifies the affected user that a runtime's status
// has changed (e.g., went offline or came back online).
func (r *WSRouter) BroadcastRuntimeStatus(userID string, runtime *service.Runtime, status string) {
	payload := protocol.RuntimeStatusPayload{
		RuntimeID: runtime.ID,
		DaemonID:  runtime.DaemonID,
		AgentType: runtime.AgentType,
		Status:    status,
	}
	r.broadcastEvent(userID, protocol.TypeRuntimeStatus, payload)
}

// broadcastEvent marshals a payload and broadcasts it to all of a user's connections.
func (r *WSRouter) broadcastEvent(userID, msgType string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("ws_router: failed to marshal %s payload: %v", msgType, err)
		return
	}

	msg := protocol.Message{
		Type:    msgType,
		Payload: data,
	}

	r.clientHub.BroadcastToUser(userID, msg)
}

// sendErrorToUser sends a chat:error message to the user.
func (r *WSRouter) sendErrorToUser(userID, sessionID, messageID, errMsg, code string) {
	payload := protocol.ChatErrorPayload{
		SessionID: sessionID,
		MessageID: messageID,
		Error:     errMsg,
		Code:      code,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("ws_router: failed to marshal error payload: %v", err)
		return
	}

	msg := protocol.Message{
		Type:    protocol.TypeChatError,
		Payload: data,
	}

	r.clientHub.SendToUser(userID, msg)
}

// sendMessageEvent sends a chat:message event to the user confirming message persistence.
func (r *WSRouter) sendMessageEvent(userID string, chatMsg *service.ChatMessage) {
	data, err := json.Marshal(chatMsg)
	if err != nil {
		log.Printf("ws_router: failed to marshal chat message: %v", err)
		return
	}

	msg := protocol.Message{
		Type:    protocol.TypeChatMsg,
		Payload: data,
	}

	r.clientHub.BroadcastToUser(userID, msg)
}
