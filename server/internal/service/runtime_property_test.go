package service

import (
	"context"
	"testing"

	"github.com/user/agentbridge/server/pkg/protocol"
	"pgregory.net/rapid"
)

// **Validates: Requirements 2.2**
// Property 2: Daemon Registration State Consistency
// For any valid DaemonRegister payload containing a daemon_id, user_id, and list of N runtimes,
// after the server processes the registration, the stored daemon record SHALL have the correct
// user_id, and the stored runtime list SHALL contain exactly N entries matching the registration
// payload, replacing any previously registered runtimes for that daemon.

func TestPropertyRegistration_RuntimeCountMatchesPayload(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryRuntimeService()
		ctx := context.Background()

		daemonID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "daemon_id")
		userID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "user_id")

		n := rapid.IntRange(0, 20).Draw(t, "num_runtimes")
		runtimes := generateRuntimes(t, n)

		reg := DaemonRegistration{
			DaemonID: daemonID,
			UserID:   userID,
			Runtimes: runtimes,
		}

		err := svc.RegisterDaemon(ctx, reg)
		if err != nil {
			t.Fatalf("RegisterDaemon failed: %v", err)
		}

		// After registration, GetUserRuntimes should return exactly N available runtimes.
		// Note: GetUserRuntimes only returns "available" runtimes for online daemons.
		// Count all runtimes for this daemon directly to verify the full set.
		storedRuntimes := getRuntimesForDaemon(svc, daemonID)
		if len(storedRuntimes) != n {
			t.Fatalf("stored %d runtimes, want %d", len(storedRuntimes), n)
		}
	})
}

func TestPropertyRegistration_ReplacesRuntimes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryRuntimeService()
		ctx := context.Background()

		daemonID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "daemon_id")
		userID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "user_id")

		// First registration with M runtimes.
		m := rapid.IntRange(1, 15).Draw(t, "first_num_runtimes")
		firstRuntimes := generateRuntimes(t, m)

		reg1 := DaemonRegistration{
			DaemonID: daemonID,
			UserID:   userID,
			Runtimes: firstRuntimes,
		}
		if err := svc.RegisterDaemon(ctx, reg1); err != nil {
			t.Fatalf("first RegisterDaemon failed: %v", err)
		}

		// Second registration with N runtimes (different count).
		n := rapid.IntRange(0, 15).Draw(t, "second_num_runtimes")
		secondRuntimes := generateRuntimes(t, n)

		reg2 := DaemonRegistration{
			DaemonID: daemonID,
			UserID:   userID,
			Runtimes: secondRuntimes,
		}
		if err := svc.RegisterDaemon(ctx, reg2); err != nil {
			t.Fatalf("second RegisterDaemon failed: %v", err)
		}

		// After the second registration, stored runtimes should be exactly N (the second set).
		storedRuntimes := getRuntimesForDaemon(svc, daemonID)
		if len(storedRuntimes) != n {
			t.Fatalf("after re-registration: stored %d runtimes, want %d", len(storedRuntimes), n)
		}
	})
}

func TestPropertyRegistration_CorrectUserID(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryRuntimeService()
		ctx := context.Background()

		daemonID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "daemon_id")
		userID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "user_id")
		otherUserID := rapid.StringMatching(`other[a-z0-9\-]{1,20}`).Draw(t, "other_user_id")

		n := rapid.IntRange(1, 10).Draw(t, "num_runtimes")
		runtimes := generateRuntimesAllAvailable(t, n)

		reg := DaemonRegistration{
			DaemonID: daemonID,
			UserID:   userID,
			Runtimes: runtimes,
		}

		if err := svc.RegisterDaemon(ctx, reg); err != nil {
			t.Fatalf("RegisterDaemon failed: %v", err)
		}

		// Verify the daemon has the correct user_id.
		daemon, exists := svc.daemons[daemonID]
		if !exists {
			t.Fatal("daemon record not found after registration")
		}
		if daemon.UserID != userID {
			t.Fatalf("daemon.UserID = %q, want %q", daemon.UserID, userID)
		}

		// GetUserRuntimes for the correct user should return runtimes.
		userRuntimes, err := svc.GetUserRuntimes(ctx, userID)
		if err != nil {
			t.Fatalf("GetUserRuntimes failed: %v", err)
		}
		if len(userRuntimes) != n {
			t.Fatalf("GetUserRuntimes returned %d runtimes for correct user, want %d", len(userRuntimes), n)
		}

		// GetUserRuntimes for a different user should return nothing.
		otherRuntimes, err := svc.GetUserRuntimes(ctx, otherUserID)
		if err != nil {
			t.Fatalf("GetUserRuntimes for other user failed: %v", err)
		}
		if len(otherRuntimes) != 0 {
			t.Fatalf("GetUserRuntimes returned %d runtimes for other user, want 0", len(otherRuntimes))
		}
	})
}

func TestPropertyRegistration_RuntimeFieldsMatch(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryRuntimeService()
		ctx := context.Background()

		daemonID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "daemon_id")
		userID := rapid.StringMatching(`[a-z][a-z0-9\-]{1,30}`).Draw(t, "user_id")

		n := rapid.IntRange(1, 15).Draw(t, "num_runtimes")
		runtimes := generateRuntimes(t, n)

		reg := DaemonRegistration{
			DaemonID: daemonID,
			UserID:   userID,
			Runtimes: runtimes,
		}

		if err := svc.RegisterDaemon(ctx, reg); err != nil {
			t.Fatalf("RegisterDaemon failed: %v", err)
		}

		// Verify each stored runtime's fields match the registration payload.
		storedRuntimes := getRuntimesForDaemon(svc, daemonID)
		if len(storedRuntimes) != n {
			t.Fatalf("stored %d runtimes, want %d", len(storedRuntimes), n)
		}

		// Build a lookup from the registration payload for comparison.
		// Since runtimes are stored in insertion order (map iteration is random),
		// we match by checking that for each stored runtime, there exists a matching
		// entry in the payload.
		type runtimeKey struct {
			AgentType  string
			BinaryPath string
			Version    string
		}

		payloadCounts := make(map[runtimeKey]int)
		for _, ri := range runtimes {
			key := runtimeKey{AgentType: ri.AgentType, BinaryPath: ri.BinaryPath, Version: ri.Version}
			payloadCounts[key]++
		}

		storedCounts := make(map[runtimeKey]int)
		for _, rt := range storedRuntimes {
			key := runtimeKey{AgentType: rt.AgentType, BinaryPath: rt.BinaryPath, Version: rt.Version}
			storedCounts[key]++
		}

		for key, count := range payloadCounts {
			if storedCounts[key] != count {
				t.Fatalf("runtime mismatch for %+v: stored %d, payload has %d", key, storedCounts[key], count)
			}
		}
	})
}

// --- Generators ---

// generateRuntimes creates N random RuntimeInfo entries with mixed statuses.
func generateRuntimes(t *rapid.T, n int) []protocol.RuntimeInfo {
	agentTypes := []string{"claude", "kiro-cli", "gemini", "codex", "copilot", "opencode", "hermes", "pi", "cursor-agent", "kimi"}
	runtimes := make([]protocol.RuntimeInfo, n)
	for i := 0; i < n; i++ {
		agentType := rapid.SampledFrom(agentTypes).Draw(t, "agent_type")
		binaryPath := rapid.StringMatching(`/usr/local/bin/[a-z\-]{2,15}`).Draw(t, "binary_path")
		version := rapid.StringMatching(`[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}`).Draw(t, "version")
		status := rapid.SampledFrom([]string{"available", "unavailable"}).Draw(t, "status")

		runtimes[i] = protocol.RuntimeInfo{
			AgentType:  agentType,
			BinaryPath: binaryPath,
			Version:    version,
			Status:     status,
		}
	}
	return runtimes
}

// generateRuntimesAllAvailable creates N random RuntimeInfo entries all with "available" status.
func generateRuntimesAllAvailable(t *rapid.T, n int) []protocol.RuntimeInfo {
	agentTypes := []string{"claude", "kiro-cli", "gemini", "codex", "copilot", "opencode", "hermes", "pi", "cursor-agent", "kimi"}
	runtimes := make([]protocol.RuntimeInfo, n)
	for i := 0; i < n; i++ {
		agentType := rapid.SampledFrom(agentTypes).Draw(t, "agent_type")
		binaryPath := rapid.StringMatching(`/usr/local/bin/[a-z\-]{2,15}`).Draw(t, "binary_path")
		version := rapid.StringMatching(`[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}`).Draw(t, "version")

		runtimes[i] = protocol.RuntimeInfo{
			AgentType:  agentType,
			BinaryPath: binaryPath,
			Version:    version,
			Status:     "available",
		}
	}
	return runtimes
}
