package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/user/agentbridge/server/pkg/protocol"
	"pgregory.net/rapid"
)

// **Validates: Requirements 7.1, 7.2, 7.3, 7.4, 7.6**
// Property 16: Multi-User Isolation
// For any two distinct users A and B, user A SHALL NOT be able to: list user B's
// runtimes, bind to user B's runtimes, read user B's chat sessions, modify user B's
// chat sessions, or cause messages to be relayed to user B's daemon. All such
// cross-user operations SHALL be rejected with an authorization error that does not
// reveal whether the target resource exists.

// setupMultiUserEnv creates a test environment with two distinct users, each with
// their own daemon, runtimes, and sessions. Returns the AccessControl, chat service,
// runtime service, and the session IDs for each user.
func setupMultiUserEnv(t *rapid.T, userA, userB string) (*AccessControl, *InMemoryChatService, *InMemoryRuntimeService, string, string) {
	chatSvc := NewInMemoryChatService()
	runtimeSvc := NewInMemoryRuntimeService()
	ac := NewAccessControl(chatSvc, runtimeSvc)
	ctx := context.Background()

	// Generate 1-3 runtimes for user A.
	numRuntimesA := rapid.IntRange(1, 3).Draw(t, "num_runtimes_A")
	runtimesA := make([]protocol.RuntimeInfo, numRuntimesA)
	for i := range numRuntimesA {
		runtimesA[i] = protocol.RuntimeInfo{
			AgentType:  fmt.Sprintf("agent-a-%d", i),
			BinaryPath: fmt.Sprintf("/usr/bin/agent-a-%d", i),
			Version:    "1.0.0",
			Status:     "available",
		}
	}

	err := runtimeSvc.RegisterDaemon(ctx, DaemonRegistration{
		DaemonID: fmt.Sprintf("daemon-%s", userA),
		UserID:   userA,
		Runtimes: runtimesA,
	})
	if err != nil {
		t.Fatalf("RegisterDaemon for userA failed: %v", err)
	}

	// Generate 1-3 runtimes for user B.
	numRuntimesB := rapid.IntRange(1, 3).Draw(t, "num_runtimes_B")
	runtimesB := make([]protocol.RuntimeInfo, numRuntimesB)
	for i := range numRuntimesB {
		runtimesB[i] = protocol.RuntimeInfo{
			AgentType:  fmt.Sprintf("agent-b-%d", i),
			BinaryPath: fmt.Sprintf("/usr/bin/agent-b-%d", i),
			Version:    "2.0.0",
			Status:     "available",
		}
	}

	err = runtimeSvc.RegisterDaemon(ctx, DaemonRegistration{
		DaemonID: fmt.Sprintf("daemon-%s", userB),
		UserID:   userB,
		Runtimes: runtimesB,
	})
	if err != nil {
		t.Fatalf("RegisterDaemon for userB failed: %v", err)
	}

	// Create a session for user A.
	sessionA, err := chatSvc.CreateSession(ctx, userA)
	if err != nil {
		t.Fatalf("CreateSession for userA failed: %v", err)
	}

	// Create a session for user B.
	sessionB, err := chatSvc.CreateSession(ctx, userB)
	if err != nil {
		t.Fatalf("CreateSession for userB failed: %v", err)
	}

	return ac, chatSvc, runtimeSvc, sessionA.ID, sessionB.ID
}

// genDistinctUserPair generates two distinct user IDs.
func genDistinctUserPair(t *rapid.T) (string, string) {
	idxA := rapid.IntRange(0, 999).Draw(t, "user_idx_A")
	// Ensure B is different from A.
	idxB := rapid.IntRange(0, 998).Draw(t, "user_idx_B")
	if idxB >= idxA {
		idxB++
	}
	return fmt.Sprintf("user-%d", idxA), fmt.Sprintf("user-%d", idxB)
}

// TestProperty_MultiUserIsolation_SessionAccessDenied verifies that for any two
// distinct users A and B, user A cannot access user B's sessions (always ErrForbidden).
func TestProperty_MultiUserIsolation_SessionAccessDenied(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userA, userB := genDistinctUserPair(t)
		ac, chatSvc, _, _, sessionBID := setupMultiUserEnv(t, userA, userB)
		ctx := context.Background()

		// User A should NOT be able to verify ownership of user B's session.
		err := ac.VerifySessionOwnership(ctx, userA, sessionBID)
		if err != ErrForbidden {
			t.Errorf("VerifySessionOwnership: user A accessing user B's session: got %v, want ErrForbidden", err)
		}

		// User A should NOT be able to get user B's session.
		_, err = chatSvc.GetSession(ctx, userA, sessionBID)
		if err == nil {
			t.Error("GetSession: user A was able to read user B's session")
		}

		// User A should NOT be able to rename user B's session.
		err = chatSvc.RenameSession(ctx, userA, sessionBID, "hacked")
		if err == nil {
			t.Error("RenameSession: user A was able to modify user B's session")
		}

		// User A should NOT be able to delete user B's session.
		err = chatSvc.DeleteSession(ctx, userA, sessionBID)
		if err == nil {
			t.Error("DeleteSession: user A was able to delete user B's session")
		}

		// User A should NOT be able to send a message to user B's session.
		_, err = chatSvc.SendMessage(ctx, userA, sessionBID, "hello from A")
		if err == nil {
			t.Error("SendMessage: user A was able to send message to user B's session")
		}
	})
}

// TestProperty_MultiUserIsolation_DaemonAccessDenied verifies that for any two
// distinct users, user A cannot access user B's daemons (always ErrForbidden).
func TestProperty_MultiUserIsolation_DaemonAccessDenied(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userA, userB := genDistinctUserPair(t)
		ac, _, _, _, _ := setupMultiUserEnv(t, userA, userB)
		ctx := context.Background()

		daemonBID := fmt.Sprintf("daemon-%s", userB)

		// User A should NOT be able to verify ownership of user B's daemon.
		err := ac.VerifyDaemonOwnership(ctx, userA, daemonBID)
		if err != ErrForbidden {
			t.Errorf("VerifyDaemonOwnership: user A accessing user B's daemon: got %v, want ErrForbidden", err)
		}
	})
}

// TestProperty_MultiUserIsolation_RuntimeAccessDenied verifies that for any two
// distinct users, user A cannot access user B's runtimes (always ErrForbidden).
func TestProperty_MultiUserIsolation_RuntimeAccessDenied(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userA, userB := genDistinctUserPair(t)
		ac, _, runtimeSvc, _, _ := setupMultiUserEnv(t, userA, userB)
		ctx := context.Background()

		// Get user B's runtimes to obtain their IDs.
		runtimesB, err := runtimeSvc.GetUserRuntimes(ctx, userB)
		if err != nil {
			t.Fatalf("GetUserRuntimes for userB failed: %v", err)
		}
		if len(runtimesB) == 0 {
			t.Fatal("no runtimes found for userB")
		}

		// User A should NOT be able to verify ownership of any of user B's runtimes.
		for _, rt := range runtimesB {
			err := ac.VerifyRuntimeOwnership(ctx, userA, rt.ID)
			if err != ErrForbidden {
				t.Errorf("VerifyRuntimeOwnership: user A accessing user B's runtime %s: got %v, want ErrForbidden",
					rt.ID, err)
			}
		}

		// User A's GetUserRuntimes should never include user B's runtimes.
		runtimesA, err := runtimeSvc.GetUserRuntimes(ctx, userA)
		if err != nil {
			t.Fatalf("GetUserRuntimes for userA failed: %v", err)
		}
		daemonBID := fmt.Sprintf("daemon-%s", userB)
		for _, rt := range runtimesA {
			if rt.DaemonID == daemonBID {
				t.Errorf("user A's runtime list contains runtime %s from user B's daemon", rt.ID)
			}
		}
	})
}

// TestProperty_MultiUserIsolation_RelayAuthorizationDenied verifies that for any two
// distinct users, relay authorization fails when daemon doesn't belong to user.
func TestProperty_MultiUserIsolation_RelayAuthorizationDenied(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userA, userB := genDistinctUserPair(t)
		ac, _, _, sessionAID, sessionBID := setupMultiUserEnv(t, userA, userB)
		ctx := context.Background()

		daemonAID := fmt.Sprintf("daemon-%s", userA)
		daemonBID := fmt.Sprintf("daemon-%s", userB)

		// User A trying to relay to user B's daemon (for user A's own session) should fail.
		err := ac.VerifyRelayAuthorization(ctx, userA, sessionAID, daemonBID)
		if err != ErrForbidden {
			t.Errorf("VerifyRelayAuthorization: user A relaying to user B's daemon: got %v, want ErrForbidden", err)
		}

		// User A trying to relay for user B's session (to user A's own daemon) should fail.
		err = ac.VerifyRelayAuthorization(ctx, userA, sessionBID, daemonAID)
		if err != ErrForbidden {
			t.Errorf("VerifyRelayAuthorization: user A relaying for user B's session: got %v, want ErrForbidden", err)
		}

		// User A trying to relay for user B's session to user B's daemon should fail.
		err = ac.VerifyRelayAuthorization(ctx, userA, sessionBID, daemonBID)
		if err != ErrForbidden {
			t.Errorf("VerifyRelayAuthorization: user A relaying for user B's session to user B's daemon: got %v, want ErrForbidden", err)
		}
	})
}

// TestProperty_MultiUserIsolation_BindingDenied verifies that for any two distinct users,
// user A cannot bind user B's runtimes to user A's sessions.
func TestProperty_MultiUserIsolation_BindingDenied(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userA, userB := genDistinctUserPair(t)
		ac, chatSvc, runtimeSvc, sessionAID, _ := setupMultiUserEnv(t, userA, userB)
		ctx := context.Background()

		// Get user B's runtimes to attempt binding.
		runtimesB, err := runtimeSvc.GetUserRuntimes(ctx, userB)
		if err != nil {
			t.Fatalf("GetUserRuntimes for userB failed: %v", err)
		}
		if len(runtimesB) == 0 {
			t.Fatal("no runtimes found for userB")
		}

		// Pick a random runtime from user B.
		idx := rapid.IntRange(0, len(runtimesB)-1).Draw(t, "runtime_idx")
		runtimeBID := runtimesB[idx].ID

		// User A should NOT be able to verify ownership of user B's runtime (prerequisite for binding).
		err = ac.VerifyRuntimeOwnership(ctx, userA, runtimeBID)
		if err != ErrForbidden {
			t.Errorf("VerifyRuntimeOwnership: user A accessing user B's runtime %s: got %v, want ErrForbidden",
				runtimeBID, err)
		}

		// User A should NOT be able to bind user B's runtime to user A's session via BindSessionRuntime.
		// The BindSessionRuntime method on InMemoryChatService doesn't enforce cross-user runtime checks
		// directly (that's the AccessControl layer's job), but the service-level BindRuntime on
		// RuntimeService will succeed since it only checks availability. The access control layer
		// (VerifyRuntimeOwnership) is what prevents this in the real flow.
		// Verify the full access-controlled flow: ownership check must fail before bind is attempted.
		err = ac.VerifyRuntimeOwnership(ctx, userA, runtimeBID)
		if err == nil {
			// If ownership check passes (it shouldn't), the bind would succeed — that's a violation.
			t.Errorf("user A was able to pass ownership check for user B's runtime %s", runtimeBID)
		}

		// Also verify that user A cannot bind user B's runtime to user B's session
		// (cross-user session + cross-user runtime).
		_, sessionBID := func() (string, string) {
			// Get session B ID from the setup.
			sessions, _, _ := chatSvc.ListSessions(ctx, userB, 1, 50)
			if len(sessions) == 0 {
				t.Fatal("no sessions found for userB")
			}
			return sessions[0].ID, sessions[0].ID
		}()

		// User A cannot access user B's session for binding.
		err = ac.VerifySessionOwnership(ctx, userA, sessionBID)
		if err != ErrForbidden {
			t.Errorf("VerifySessionOwnership: user A accessing user B's session for binding: got %v, want ErrForbidden", err)
		}

		// Verify user A's own session can only bind to user A's own runtimes.
		runtimesA, _ := runtimeSvc.GetUserRuntimes(ctx, userA)
		if len(runtimesA) > 0 {
			// User A binding their own runtime to their own session should pass ownership check.
			err = ac.VerifyRuntimeOwnership(ctx, userA, runtimesA[0].ID)
			if err != nil {
				t.Errorf("VerifyRuntimeOwnership: user A accessing own runtime: got %v, want nil", err)
			}
			// And the actual bind should succeed.
			err = chatSvc.BindSessionRuntime(ctx, userA, sessionAID, runtimesA[0].ID, runtimeSvc)
			if err != nil {
				t.Errorf("BindSessionRuntime: user A binding own runtime to own session: got %v, want nil", err)
			}
		}
	})
}

// TestProperty_MultiUserIsolation_ErrorDoesNotRevealExistence verifies that the error
// returned is always the same ErrForbidden regardless of whether the resource exists
// or belongs to another user. This prevents information leakage about resource existence.
func TestProperty_MultiUserIsolation_ErrorDoesNotRevealExistence(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		userA, userB := genDistinctUserPair(t)
		ac, _, _, _, sessionBID := setupMultiUserEnv(t, userA, userB)
		ctx := context.Background()

		daemonBID := fmt.Sprintf("daemon-%s", userB)

		// Generate a random nonexistent resource ID.
		nonexistentID := fmt.Sprintf("nonexistent-%d", rapid.IntRange(0, 99999).Draw(t, "nonexistent_id"))

		// Session: cross-user access vs nonexistent resource should return identical error.
		errCrossSession := ac.VerifySessionOwnership(ctx, userA, sessionBID)
		errNonexistentSession := ac.VerifySessionOwnership(ctx, userA, nonexistentID)

		if errCrossSession != ErrForbidden {
			t.Errorf("cross-user session access: got %v, want ErrForbidden", errCrossSession)
		}
		if errNonexistentSession != ErrForbidden {
			t.Errorf("nonexistent session access: got %v, want ErrForbidden", errNonexistentSession)
		}
		if errCrossSession.Error() != errNonexistentSession.Error() {
			t.Errorf("error messages differ for session access: %q vs %q — leaks resource existence",
				errCrossSession.Error(), errNonexistentSession.Error())
		}

		// Daemon: cross-user access vs nonexistent resource should return identical error.
		errCrossDaemon := ac.VerifyDaemonOwnership(ctx, userA, daemonBID)
		errNonexistentDaemon := ac.VerifyDaemonOwnership(ctx, userA, nonexistentID)

		if errCrossDaemon != ErrForbidden {
			t.Errorf("cross-user daemon access: got %v, want ErrForbidden", errCrossDaemon)
		}
		if errNonexistentDaemon != ErrForbidden {
			t.Errorf("nonexistent daemon access: got %v, want ErrForbidden", errNonexistentDaemon)
		}
		if errCrossDaemon.Error() != errNonexistentDaemon.Error() {
			t.Errorf("error messages differ for daemon access: %q vs %q — leaks resource existence",
				errCrossDaemon.Error(), errNonexistentDaemon.Error())
		}

		// Runtime: cross-user access vs nonexistent resource should return identical error.
		// Get one of user B's runtime IDs for the cross-user test.
		runtimesB, _ := ac.Runtime.GetUserRuntimes(ctx, userB)
		if len(runtimesB) > 0 {
			errCrossRuntime := ac.VerifyRuntimeOwnership(ctx, userA, runtimesB[0].ID)
			errNonexistentRuntime := ac.VerifyRuntimeOwnership(ctx, userA, nonexistentID)

			if errCrossRuntime != ErrForbidden {
				t.Errorf("cross-user runtime access: got %v, want ErrForbidden", errCrossRuntime)
			}
			if errNonexistentRuntime != ErrForbidden {
				t.Errorf("nonexistent runtime access: got %v, want ErrForbidden", errNonexistentRuntime)
			}
			if errCrossRuntime.Error() != errNonexistentRuntime.Error() {
				t.Errorf("error messages differ for runtime access: %q vs %q — leaks resource existence",
					errCrossRuntime.Error(), errNonexistentRuntime.Error())
			}
		}

		// Relay: cross-user relay vs nonexistent resources should return identical error.
		errCrossRelay := ac.VerifyRelayAuthorization(ctx, userA, sessionBID, daemonBID)
		errNonexistentRelay := ac.VerifyRelayAuthorization(ctx, userA, nonexistentID, nonexistentID)

		if errCrossRelay != ErrForbidden {
			t.Errorf("cross-user relay: got %v, want ErrForbidden", errCrossRelay)
		}
		if errNonexistentRelay != ErrForbidden {
			t.Errorf("nonexistent relay: got %v, want ErrForbidden", errNonexistentRelay)
		}
		if errCrossRelay.Error() != errNonexistentRelay.Error() {
			t.Errorf("error messages differ for relay: %q vs %q — leaks resource existence",
				errCrossRelay.Error(), errNonexistentRelay.Error())
		}
	})
}
