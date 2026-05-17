package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"pgregory.net/rapid"

	"github.com/user/agentbridge/server/internal/clientws"
	"github.com/user/agentbridge/server/internal/service"
	"github.com/user/agentbridge/server/pkg/protocol"
)

// **Validates: Requirements 9.5**
// Property 19: Persist-Before-Relay Invariant
// For any user message, the server SHALL confirm successful persistence before
// relaying the message to the daemon. If persistence fails, the message SHALL
// NOT be relayed, and an error SHALL be returned to the client.

// trackingDaemonHub wraps mockDaemonHub to track relay attempts with ordering info.
type trackingDaemonHub struct {
	*mockDaemonHub
	mu          sync.Mutex
	relayCalls  []relayCall
	relayCount  int
}

type relayCall struct {
	daemonID string
	msg      protocol.Message
}

func newTrackingDaemonHub() *trackingDaemonHub {
	return &trackingDaemonHub{
		mockDaemonHub: newMockDaemonHub(),
	}
}

func (t *trackingDaemonHub) SendToDaemon(daemonID string, msg protocol.Message) error {
	t.mu.Lock()
	t.relayCount++
	t.relayCalls = append(t.relayCalls, relayCall{daemonID: daemonID, msg: msg})
	t.mu.Unlock()
	return t.mockDaemonHub.SendToDaemon(daemonID, msg)
}

func (t *trackingDaemonHub) getRelayCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.relayCount
}

func (t *trackingDaemonHub) getRelayCalls() []relayCall {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]relayCall, len(t.relayCalls))
	copy(result, t.relayCalls)
	return result
}

// TestProperty_PersistBeforeRelay_SuccessfulPersistThenRelay verifies that for any
// valid message content, after SendMessage succeeds (persistence), the message
// exists in the store AND the relay to the daemon happens.
func TestProperty_PersistBeforeRelay_SuccessfulPersistThenRelay(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Setup services.
		chatSvc := service.NewInMemoryChatService()
		runtimeSvc := service.NewInMemoryRuntimeService()
		daemonHub := newTrackingDaemonHub()
		clientHub := clientws.NewHub()

		ctx := context.Background()
		userID := "user-persist-relay"

		// Create a session.
		session, err := chatSvc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}

		// Register a daemon with a runtime.
		err = runtimeSvc.RegisterDaemon(ctx, service.DaemonRegistration{
			DaemonID: "daemon-persist",
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
		if len(runtimes) == 0 {
			t.Fatal("expected at least one runtime")
		}
		runtimeID := runtimes[0].ID

		err = chatSvc.BindSessionRuntime(ctx, userID, session.ID, runtimeID, runtimeSvc)
		if err != nil {
			t.Fatalf("BindSessionRuntime: %v", err)
		}

		// Mark daemon as online.
		daemonHub.setOnline("daemon-persist")

		// Create the router.
		router := NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, service.NewMessageQueue())

		// Generate a valid message content that will have at least 1 non-whitespace char after trimming.
		// Use a prefix of at least one alphanumeric char followed by optional mixed content.
		prefix := rapid.StringMatching(`[a-zA-Z0-9]{1,10}`).Draw(t, "prefix")
		suffix := rapid.StringMatching(`[a-zA-Z0-9 .,!?]{0,500}`).Draw(t, "suffix")
		content := prefix + suffix

		// Simulate a chat:send message.
		sendPayload := protocol.ChatSendPayload{
			SessionID: session.ID,
			Content:   content,
		}
		payloadData, _ := json.Marshal(sendPayload)
		msg := protocol.Message{
			Type:    protocol.TypeChatSend,
			Payload: payloadData,
		}

		// Invoke the handler.
		router.handleClientMessage(userID, msg)

		// PROPERTY: After successful handling, the message MUST be persisted in the store.
		msgs, err := chatSvc.GetMessages(ctx, session.ID)
		if err != nil {
			t.Fatalf("GetMessages: %v", err)
		}
		if len(msgs) == 0 {
			t.Fatal("message was not persisted in the store")
		}

		// Find the message with matching content.
		found := false
		for _, m := range msgs {
			if m.Content == strings.TrimSpace(content) && m.Role == "user" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("persisted message with content %q not found in store", strings.TrimSpace(content))
		}

		// PROPERTY: The relay to the daemon MUST have happened (exactly once).
		relayCount := daemonHub.getRelayCount()
		if relayCount != 1 {
			t.Fatalf("expected exactly 1 relay to daemon, got %d", relayCount)
		}

		// Verify the relayed message is a chat:task with the correct content.
		calls := daemonHub.getRelayCalls()
		if calls[0].msg.Type != protocol.TypeChatTask {
			t.Fatalf("expected relay message type %q, got %q", protocol.TypeChatTask, calls[0].msg.Type)
		}

		var task protocol.ChatTaskPayload
		if err := json.Unmarshal(calls[0].msg.Payload, &task); err != nil {
			t.Fatalf("unmarshal task payload: %v", err)
		}
		if task.Content != strings.TrimSpace(content) {
			t.Fatalf("task content = %q, want %q", task.Content, strings.TrimSpace(content))
		}
		if task.SessionID != session.ID {
			t.Fatalf("task session_id = %q, want %q", task.SessionID, session.ID)
		}
	})
}

// TestProperty_PersistBeforeRelay_FailedPersistNoRelay verifies that if persistence
// fails (e.g., session not found, forbidden, invalid message), no relay to the
// daemon occurs.
func TestProperty_PersistBeforeRelay_FailedPersistNoRelay(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Setup services.
		chatSvc := service.NewInMemoryChatService()
		runtimeSvc := service.NewInMemoryRuntimeService()
		daemonHub := newTrackingDaemonHub()
		clientHub := clientws.NewHub()

		ctx := context.Background()
		userID := "user-persist-fail"

		// Register a daemon with a runtime (so relay infrastructure exists).
		err := runtimeSvc.RegisterDaemon(ctx, service.DaemonRegistration{
			DaemonID: "daemon-fail",
			UserID:   userID,
			Runtimes: []protocol.RuntimeInfo{
				{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"},
			},
		})
		if err != nil {
			t.Fatalf("RegisterDaemon: %v", err)
		}

		// Mark daemon as online.
		daemonHub.setOnline("daemon-fail")

		// Create the router.
		router := NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, service.NewMessageQueue())

		// Choose a failure scenario.
		failureType := rapid.IntRange(0, 2).Draw(t, "failure_type")

		var sessionID string
		var content string

		switch failureType {
		case 0:
			// Scenario: session does not exist (ErrSessionNotFound).
			sessionID = "nonexistent-session-" + fmt.Sprintf("%d", rapid.IntRange(1, 10000).Draw(t, "session_suffix"))
			content = rapid.StringMatching(`[a-zA-Z0-9 ]{1,100}`).Draw(t, "content")

		case 1:
			// Scenario: user does not own the session (ErrForbidden).
			otherUser := "other-user"
			session, err := chatSvc.CreateSession(ctx, otherUser)
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			sessionID = session.ID
			content = rapid.StringMatching(`[a-zA-Z0-9 ]{1,100}`).Draw(t, "content")

		case 2:
			// Scenario: invalid message content (empty or too long).
			session, err := chatSvc.CreateSession(ctx, userID)
			if err != nil {
				t.Fatalf("CreateSession: %v", err)
			}
			sessionID = session.ID

			// Generate invalid content: either empty/whitespace-only or exceeding 32000 chars.
			invalidType := rapid.IntRange(0, 1).Draw(t, "invalid_type")
			if invalidType == 0 {
				// Empty or whitespace-only content.
				numSpaces := rapid.IntRange(0, 10).Draw(t, "num_spaces")
				content = strings.Repeat(" ", numSpaces)
			} else {
				// Content exceeding 32000 characters.
				length := rapid.IntRange(32001, 33000).Draw(t, "long_length")
				content = strings.Repeat("x", length)
			}
		}

		// Simulate a chat:send message.
		sendPayload := protocol.ChatSendPayload{
			SessionID: sessionID,
			Content:   content,
		}
		payloadData, _ := json.Marshal(sendPayload)
		msg := protocol.Message{
			Type:    protocol.TypeChatSend,
			Payload: payloadData,
		}

		// Invoke the handler.
		router.handleClientMessage(userID, msg)

		// PROPERTY: No relay to the daemon SHALL occur when persistence fails.
		relayCount := daemonHub.getRelayCount()
		if relayCount != 0 {
			t.Fatalf("expected 0 relays to daemon when persistence fails (failure_type=%d), got %d", failureType, relayCount)
		}
	})
}

// TestProperty_PersistBeforeRelay_PersistenceConfirmedBeforeRelay verifies that
// the message is confirmed in the store at the point the relay happens. This tests
// the ordering invariant: persist THEN relay, never relay without persistence.
func TestProperty_PersistBeforeRelay_PersistenceConfirmedBeforeRelay(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Setup services.
		chatSvc := service.NewInMemoryChatService()
		runtimeSvc := service.NewInMemoryRuntimeService()
		clientHub := clientws.NewHub()

		ctx := context.Background()
		userID := "user-order-check"

		// Create a session.
		session, err := chatSvc.CreateSession(ctx, userID)
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}

		// Register a daemon with a runtime.
		err = runtimeSvc.RegisterDaemon(ctx, service.DaemonRegistration{
			DaemonID: "daemon-order",
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

		// Create a daemon hub that checks persistence at relay time.
		var persistedAtRelay bool
		var relayOccurred bool
		checkingHub := &orderCheckDaemonHub{
			mockDaemonHub: newMockDaemonHub(),
			onRelay: func(daemonID string, msg protocol.Message) {
				relayOccurred = true
				// At the moment of relay, verify the message is already persisted.
				msgs, err := chatSvc.GetMessages(ctx, session.ID)
				if err == nil && len(msgs) > 0 {
					persistedAtRelay = true
				}
			},
		}
		checkingHub.setOnline("daemon-order")

		// Create the router with the checking hub.
		router := NewWSRouter(clientHub, checkingHub, chatSvc, runtimeSvc, service.NewMessageQueue())

		// Generate valid message content.
		content := rapid.StringMatching(`[a-zA-Z0-9]{1,500}`).Draw(t, "content")

		// Simulate a chat:send message.
		sendPayload := protocol.ChatSendPayload{
			SessionID: session.ID,
			Content:   content,
		}
		payloadData, _ := json.Marshal(sendPayload)
		msg := protocol.Message{
			Type:    protocol.TypeChatSend,
			Payload: payloadData,
		}

		// Invoke the handler.
		router.handleClientMessage(userID, msg)

		// PROPERTY: The relay must have occurred.
		if !relayOccurred {
			t.Fatal("relay did not occur for valid message")
		}

		// PROPERTY: At the moment of relay, the message was already persisted.
		if !persistedAtRelay {
			t.Fatal("message was NOT persisted at the time of relay — violates persist-before-relay invariant")
		}
	})
}

// orderCheckDaemonHub is a mock DaemonHub that invokes a callback at relay time
// to verify ordering invariants.
type orderCheckDaemonHub struct {
	*mockDaemonHub
	onRelay func(daemonID string, msg protocol.Message)
}

func (h *orderCheckDaemonHub) SendToDaemon(daemonID string, msg protocol.Message) error {
	if h.onRelay != nil {
		h.onRelay(daemonID, msg)
	}
	return h.mockDaemonHub.SendToDaemon(daemonID, msg)
}
