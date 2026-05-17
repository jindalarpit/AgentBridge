package service

import (
	"context"
)

// AccessControl provides user-scoped ownership verification for all resources.
// It wraps ChatService and RuntimeService to enforce that users can only access
// their own resources. All methods return ErrForbidden on cross-user access
// without revealing whether the target resource exists.
type AccessControl struct {
	Chat    *InMemoryChatService
	Runtime *InMemoryRuntimeService
}

// NewAccessControl creates a new AccessControl instance wrapping the given services.
func NewAccessControl(chat *InMemoryChatService, runtime *InMemoryRuntimeService) *AccessControl {
	return &AccessControl{
		Chat:    chat,
		Runtime: runtime,
	}
}

// VerifySessionOwnership checks that the given session belongs to the specified user.
// Returns ErrForbidden without revealing resource existence on cross-user access.
func (ac *AccessControl) VerifySessionOwnership(ctx context.Context, userID, sessionID string) error {
	_, err := ac.Chat.GetSession(ctx, userID, sessionID)
	if err != nil {
		// Map all errors (not found or forbidden) to a generic forbidden error
		// to avoid leaking resource existence information.
		return ErrForbidden
	}
	return nil
}

// VerifyDaemonOwnership checks that the given daemon belongs to the specified user.
// Returns ErrForbidden without revealing resource existence on cross-user access.
func (ac *AccessControl) VerifyDaemonOwnership(ctx context.Context, userID, daemonID string) error {
	daemon, err := ac.Runtime.GetDaemonByID(ctx, daemonID)
	if err != nil {
		// Daemon not found — return generic forbidden to avoid leaking existence.
		return ErrForbidden
	}

	if daemon.UserID != userID {
		return ErrForbidden
	}

	return nil
}

// VerifyRuntimeOwnership checks that the given runtime's daemon belongs to the specified user.
// Returns ErrForbidden without revealing resource existence on cross-user access.
func (ac *AccessControl) VerifyRuntimeOwnership(ctx context.Context, userID, runtimeID string) error {
	rt, err := ac.Runtime.GetRuntimeByID(ctx, runtimeID)
	if err != nil {
		// Runtime not found — return generic forbidden to avoid leaking existence.
		return ErrForbidden
	}

	// Verify the runtime's daemon belongs to the user.
	return ac.VerifyDaemonOwnership(ctx, userID, rt.DaemonID)
}

// VerifyRelayAuthorization verifies that a message can be relayed to a daemon
// by checking that the daemon belongs to the authenticated user who owns the session.
// This implements Requirement 7.4: verify daemon belongs to user before relay.
// Returns ErrForbidden without revealing resource existence on cross-user access.
func (ac *AccessControl) VerifyRelayAuthorization(ctx context.Context, userID, sessionID, daemonID string) error {
	// First verify the session belongs to the user.
	if err := ac.VerifySessionOwnership(ctx, userID, sessionID); err != nil {
		return ErrForbidden
	}

	// Then verify the daemon belongs to the user.
	if err := ac.VerifyDaemonOwnership(ctx, userID, daemonID); err != nil {
		return ErrForbidden
	}

	return nil
}
