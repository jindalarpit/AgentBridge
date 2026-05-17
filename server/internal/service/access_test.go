package service

import (
	"context"
	"testing"

	"github.com/user/agentbridge/server/pkg/protocol"
)

// setupAccessControlTest creates a test environment with two users, each with their own
// daemon, runtimes, and sessions.
func setupAccessControlTest(t *testing.T) (*AccessControl, string, string) {
	t.Helper()

	chatSvc := NewInMemoryChatService()
	runtimeSvc := NewInMemoryRuntimeService()
	ac := NewAccessControl(chatSvc, runtimeSvc)
	ctx := context.Background()

	// Register daemon for user-A.
	regA := DaemonRegistration{
		DaemonID: "daemon-A",
		UserID:   "user-A",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
	}
	if err := runtimeSvc.RegisterDaemon(ctx, regA); err != nil {
		t.Fatalf("RegisterDaemon for user-A failed: %v", err)
	}

	// Register daemon for user-B.
	regB := DaemonRegistration{
		DaemonID: "daemon-B",
		UserID:   "user-B",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "2.0.0", Status: "available"},
		},
	}
	if err := runtimeSvc.RegisterDaemon(ctx, regB); err != nil {
		t.Fatalf("RegisterDaemon for user-B failed: %v", err)
	}

	// Create a session for user-A.
	sessionA, err := chatSvc.CreateSession(ctx, "user-A")
	if err != nil {
		t.Fatalf("CreateSession for user-A failed: %v", err)
	}

	// Create a session for user-B.
	sessionB, err := chatSvc.CreateSession(ctx, "user-B")
	if err != nil {
		t.Fatalf("CreateSession for user-B failed: %v", err)
	}

	return ac, sessionA.ID, sessionB.ID
}

func TestVerifySessionOwnership_ValidUser(t *testing.T) {
	ac, sessionA, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// user-A should be able to access their own session.
	err := ac.VerifySessionOwnership(ctx, "user-A", sessionA)
	if err != nil {
		t.Errorf("VerifySessionOwnership for valid user returned error: %v", err)
	}
}

func TestVerifySessionOwnership_CrossUserAccess(t *testing.T) {
	ac, sessionA, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// user-B should NOT be able to access user-A's session.
	err := ac.VerifySessionOwnership(ctx, "user-B", sessionA)
	if err != ErrForbidden {
		t.Errorf("VerifySessionOwnership for cross-user access: got %v, want ErrForbidden", err)
	}
}

func TestVerifySessionOwnership_NonexistentSession(t *testing.T) {
	ac, _, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// Accessing a nonexistent session should return ErrForbidden (not ErrSessionNotFound)
	// to avoid leaking resource existence.
	err := ac.VerifySessionOwnership(ctx, "user-A", "nonexistent-session-id")
	if err != ErrForbidden {
		t.Errorf("VerifySessionOwnership for nonexistent session: got %v, want ErrForbidden", err)
	}
}

func TestVerifyDaemonOwnership_ValidUser(t *testing.T) {
	ac, _, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// user-A should be able to access their own daemon.
	err := ac.VerifyDaemonOwnership(ctx, "user-A", "daemon-A")
	if err != nil {
		t.Errorf("VerifyDaemonOwnership for valid user returned error: %v", err)
	}
}

func TestVerifyDaemonOwnership_CrossUserAccess(t *testing.T) {
	ac, _, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// user-B should NOT be able to access user-A's daemon.
	err := ac.VerifyDaemonOwnership(ctx, "user-B", "daemon-A")
	if err != ErrForbidden {
		t.Errorf("VerifyDaemonOwnership for cross-user access: got %v, want ErrForbidden", err)
	}
}

func TestVerifyDaemonOwnership_NonexistentDaemon(t *testing.T) {
	ac, _, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// Accessing a nonexistent daemon should return ErrForbidden (not ErrDaemonNotFound)
	// to avoid leaking resource existence.
	err := ac.VerifyDaemonOwnership(ctx, "user-A", "nonexistent-daemon")
	if err != ErrForbidden {
		t.Errorf("VerifyDaemonOwnership for nonexistent daemon: got %v, want ErrForbidden", err)
	}
}

func TestVerifyRuntimeOwnership_ValidUser(t *testing.T) {
	ac, _, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// Get user-A's runtime ID.
	runtimes, err := ac.Runtime.GetUserRuntimes(ctx, "user-A")
	if err != nil {
		t.Fatalf("GetUserRuntimes failed: %v", err)
	}
	if len(runtimes) == 0 {
		t.Fatal("no runtimes found for user-A")
	}
	runtimeID := runtimes[0].ID

	// user-A should be able to access their own runtime.
	err = ac.VerifyRuntimeOwnership(ctx, "user-A", runtimeID)
	if err != nil {
		t.Errorf("VerifyRuntimeOwnership for valid user returned error: %v", err)
	}
}

func TestVerifyRuntimeOwnership_CrossUserAccess(t *testing.T) {
	ac, _, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// Get user-A's runtime ID.
	runtimes, err := ac.Runtime.GetUserRuntimes(ctx, "user-A")
	if err != nil {
		t.Fatalf("GetUserRuntimes failed: %v", err)
	}
	if len(runtimes) == 0 {
		t.Fatal("no runtimes found for user-A")
	}
	runtimeID := runtimes[0].ID

	// user-B should NOT be able to access user-A's runtime.
	err = ac.VerifyRuntimeOwnership(ctx, "user-B", runtimeID)
	if err != ErrForbidden {
		t.Errorf("VerifyRuntimeOwnership for cross-user access: got %v, want ErrForbidden", err)
	}
}

func TestVerifyRuntimeOwnership_NonexistentRuntime(t *testing.T) {
	ac, _, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// Accessing a nonexistent runtime should return ErrForbidden (not ErrRuntimeNotFound)
	// to avoid leaking resource existence.
	err := ac.VerifyRuntimeOwnership(ctx, "user-A", "nonexistent-runtime")
	if err != ErrForbidden {
		t.Errorf("VerifyRuntimeOwnership for nonexistent runtime: got %v, want ErrForbidden", err)
	}
}

func TestVerifyRelayAuthorization_ValidRelay(t *testing.T) {
	ac, sessionA, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// user-A relaying to their own daemon for their own session should succeed.
	err := ac.VerifyRelayAuthorization(ctx, "user-A", sessionA, "daemon-A")
	if err != nil {
		t.Errorf("VerifyRelayAuthorization for valid relay returned error: %v", err)
	}
}

func TestVerifyRelayAuthorization_CrossUserDaemon(t *testing.T) {
	ac, sessionA, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// user-A trying to relay to user-B's daemon should fail.
	err := ac.VerifyRelayAuthorization(ctx, "user-A", sessionA, "daemon-B")
	if err != ErrForbidden {
		t.Errorf("VerifyRelayAuthorization for cross-user daemon: got %v, want ErrForbidden", err)
	}
}

func TestVerifyRelayAuthorization_CrossUserSession(t *testing.T) {
	ac, _, sessionB := setupAccessControlTest(t)
	ctx := context.Background()

	// user-A trying to relay for user-B's session should fail.
	err := ac.VerifyRelayAuthorization(ctx, "user-A", sessionB, "daemon-A")
	if err != ErrForbidden {
		t.Errorf("VerifyRelayAuthorization for cross-user session: got %v, want ErrForbidden", err)
	}
}

func TestVerifyRelayAuthorization_NonexistentResources(t *testing.T) {
	ac, _, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// Nonexistent session should return ErrForbidden.
	err := ac.VerifyRelayAuthorization(ctx, "user-A", "nonexistent-session", "daemon-A")
	if err != ErrForbidden {
		t.Errorf("VerifyRelayAuthorization for nonexistent session: got %v, want ErrForbidden", err)
	}

	// Nonexistent daemon should return ErrForbidden.
	chatSvc := ac.Chat
	session, _ := chatSvc.CreateSession(ctx, "user-A")
	err = ac.VerifyRelayAuthorization(ctx, "user-A", session.ID, "nonexistent-daemon")
	if err != ErrForbidden {
		t.Errorf("VerifyRelayAuthorization for nonexistent daemon: got %v, want ErrForbidden", err)
	}
}

func TestAccessControl_ForbiddenDoesNotRevealExistence(t *testing.T) {
	ac, _, _ := setupAccessControlTest(t)
	ctx := context.Background()

	// All cross-user and nonexistent resource errors should be the same ErrForbidden.
	// This ensures we don't leak information about whether a resource exists.

	// Cross-user session access.
	err1 := ac.VerifySessionOwnership(ctx, "user-B", "nonexistent-session")
	err2 := ac.VerifySessionOwnership(ctx, "user-A", "nonexistent-session")

	if err1 != ErrForbidden || err2 != ErrForbidden {
		t.Error("expected both nonexistent session accesses to return ErrForbidden")
	}

	// Cross-user daemon access vs nonexistent daemon.
	err3 := ac.VerifyDaemonOwnership(ctx, "user-B", "daemon-A") // exists but wrong user
	err4 := ac.VerifyDaemonOwnership(ctx, "user-A", "nonexistent-daemon")

	if err3 != ErrForbidden || err4 != ErrForbidden {
		t.Error("expected both daemon access errors to return the same ErrForbidden")
	}

	// Verify the error messages are identical (no information leakage).
	if err3.Error() != err4.Error() {
		t.Errorf("error messages differ: %q vs %q — this leaks resource existence", err3.Error(), err4.Error())
	}
}
