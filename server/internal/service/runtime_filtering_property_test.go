package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/user/agentbridge/server/pkg/protocol"
	"pgregory.net/rapid"
)

// **Validates: Requirements 5.1**
// Property 10: Runtime Filtering
// For any set of runtimes across multiple users and daemons with mixed statuses
// (available, unavailable, offline), querying available runtimes for user U SHALL
// return only runtimes that belong to user U's daemon AND have status "available",
// and SHALL never include runtimes belonging to other users.

// genRuntimeStatus generates a random runtime status from the valid set.
func genRuntimeStatus(t *rapid.T) string {
	return rapid.SampledFrom([]string{"available", "unavailable"}).Draw(t, "status")
}

// genRuntimeInfo generates a random RuntimeInfo with a given index for uniqueness.
func genRuntimeInfo(t *rapid.T, idx int) protocol.RuntimeInfo {
	agentTypes := []string{"claude", "kiro-cli", "gemini", "codex", "copilot", "opencode", "hermes"}
	agentType := rapid.SampledFrom(agentTypes).Draw(t, fmt.Sprintf("agent_type_%d", idx))
	version := fmt.Sprintf("%d.%d.%d",
		rapid.IntRange(0, 9).Draw(t, fmt.Sprintf("major_%d", idx)),
		rapid.IntRange(0, 9).Draw(t, fmt.Sprintf("minor_%d", idx)),
		rapid.IntRange(0, 9).Draw(t, fmt.Sprintf("patch_%d", idx)),
	)
	status := genRuntimeStatus(t)

	return protocol.RuntimeInfo{
		AgentType:  agentType,
		BinaryPath: fmt.Sprintf("/usr/bin/%s-%d", agentType, idx),
		Version:    version,
		Status:     status,
	}
}

// TestProperty_RuntimeFiltering_OnlyUserAvailableRuntimes verifies that
// GetUserRuntimes for user U returns ONLY runtimes belonging to user U's daemon
// with status "available".
func TestProperty_RuntimeFiltering_OnlyUserAvailableRuntimes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryRuntimeService()
		ctx := context.Background()

		// Generate 2-5 users.
		numUsers := rapid.IntRange(2, 5).Draw(t, "num_users")
		userIDs := make([]string, numUsers)
		for i := range numUsers {
			userIDs[i] = fmt.Sprintf("user-%d", i)
		}

		// Each user gets one daemon with 1-5 runtimes of mixed statuses.
		type userSetup struct {
			daemonID string
			runtimes []protocol.RuntimeInfo
		}
		setups := make([]userSetup, numUsers)

		runtimeIdx := 0
		for i, userID := range userIDs {
			daemonID := fmt.Sprintf("daemon-%d", i)
			numRuntimes := rapid.IntRange(1, 5).Draw(t, fmt.Sprintf("num_runtimes_%d", i))
			runtimes := make([]protocol.RuntimeInfo, numRuntimes)
			for j := range numRuntimes {
				runtimes[j] = genRuntimeInfo(t, runtimeIdx)
				runtimeIdx++
			}

			setups[i] = userSetup{daemonID: daemonID, runtimes: runtimes}

			err := svc.RegisterDaemon(ctx, DaemonRegistration{
				DaemonID: daemonID,
				UserID:   userID,
				Runtimes: runtimes,
			})
			if err != nil {
				t.Fatalf("RegisterDaemon failed for %s: %v", userID, err)
			}
		}

		// Pick a target user to query.
		targetIdx := rapid.IntRange(0, numUsers-1).Draw(t, "target_user_idx")
		targetUserID := userIDs[targetIdx]

		// Query runtimes for the target user.
		result, err := svc.GetUserRuntimes(ctx, targetUserID)
		if err != nil {
			t.Fatalf("GetUserRuntimes failed: %v", err)
		}

		// Count expected available runtimes for the target user.
		expectedCount := 0
		for _, ri := range setups[targetIdx].runtimes {
			if ri.Status == "available" {
				expectedCount++
			}
		}

		// Verify count matches.
		if len(result) != expectedCount {
			t.Fatalf("GetUserRuntimes returned %d runtimes, want %d for user %s",
				len(result), expectedCount, targetUserID)
		}

		// Verify all returned runtimes belong to the target user's daemon.
		for _, rt := range result {
			if rt.DaemonID != setups[targetIdx].daemonID {
				t.Errorf("runtime %s belongs to daemon %s, expected %s",
					rt.ID, rt.DaemonID, setups[targetIdx].daemonID)
			}
			if rt.Status != "available" {
				t.Errorf("runtime %s has status %q, expected %q",
					rt.ID, rt.Status, "available")
			}
		}
	})
}

// TestProperty_RuntimeFiltering_NeverReturnsOtherUsersRuntimes verifies that
// GetUserRuntimes for user U NEVER returns runtimes belonging to other users.
func TestProperty_RuntimeFiltering_NeverReturnsOtherUsersRuntimes(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryRuntimeService()
		ctx := context.Background()

		// Generate 2-5 users.
		numUsers := rapid.IntRange(2, 5).Draw(t, "num_users")
		userIDs := make([]string, numUsers)
		daemonIDs := make([]string, numUsers)

		runtimeIdx := 0
		for i := range numUsers {
			userIDs[i] = fmt.Sprintf("user-%d", i)
			daemonIDs[i] = fmt.Sprintf("daemon-%d", i)

			numRuntimes := rapid.IntRange(1, 5).Draw(t, fmt.Sprintf("num_runtimes_%d", i))
			runtimes := make([]protocol.RuntimeInfo, numRuntimes)
			for j := range numRuntimes {
				runtimes[j] = genRuntimeInfo(t, runtimeIdx)
				runtimeIdx++
			}

			err := svc.RegisterDaemon(ctx, DaemonRegistration{
				DaemonID: daemonIDs[i],
				UserID:   userIDs[i],
				Runtimes: runtimes,
			})
			if err != nil {
				t.Fatalf("RegisterDaemon failed: %v", err)
			}
		}

		// For each user, verify their runtimes never include other users' daemons.
		for i, userID := range userIDs {
			result, err := svc.GetUserRuntimes(ctx, userID)
			if err != nil {
				t.Fatalf("GetUserRuntimes failed for %s: %v", userID, err)
			}

			for _, rt := range result {
				if rt.DaemonID != daemonIDs[i] {
					t.Errorf("user %s got runtime from daemon %s (expected only %s)",
						userID, rt.DaemonID, daemonIDs[i])
				}
			}
		}
	})
}

// TestProperty_RuntimeFiltering_OfflineDaemonReturnsEmpty verifies that
// for any user whose daemon is marked offline (via DeregisterDaemon),
// GetUserRuntimes returns empty.
func TestProperty_RuntimeFiltering_OfflineDaemonReturnsEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryRuntimeService()
		ctx := context.Background()

		// Generate 2-5 users.
		numUsers := rapid.IntRange(2, 5).Draw(t, "num_users")

		runtimeIdx := 0
		for i := range numUsers {
			userID := fmt.Sprintf("user-%d", i)
			daemonID := fmt.Sprintf("daemon-%d", i)

			numRuntimes := rapid.IntRange(1, 5).Draw(t, fmt.Sprintf("num_runtimes_%d", i))
			runtimes := make([]protocol.RuntimeInfo, numRuntimes)
			for j := range numRuntimes {
				// Force at least some to be "available" to make the test meaningful.
				runtimes[j] = protocol.RuntimeInfo{
					AgentType:  fmt.Sprintf("agent-%d", runtimeIdx),
					BinaryPath: fmt.Sprintf("/usr/bin/agent-%d", runtimeIdx),
					Version:    "1.0.0",
					Status:     "available",
				}
				runtimeIdx++
			}

			err := svc.RegisterDaemon(ctx, DaemonRegistration{
				DaemonID: daemonID,
				UserID:   userID,
				Runtimes: runtimes,
			})
			if err != nil {
				t.Fatalf("RegisterDaemon failed: %v", err)
			}
		}

		// Pick a random user and deregister their daemon.
		targetIdx := rapid.IntRange(0, numUsers-1).Draw(t, "target_user_idx")
		targetUserID := fmt.Sprintf("user-%d", targetIdx)
		targetDaemonID := fmt.Sprintf("daemon-%d", targetIdx)

		err := svc.DeregisterDaemon(ctx, targetDaemonID)
		if err != nil {
			t.Fatalf("DeregisterDaemon failed: %v", err)
		}

		// GetUserRuntimes should return empty for the deregistered user.
		result, err := svc.GetUserRuntimes(ctx, targetUserID)
		if err != nil {
			t.Fatalf("GetUserRuntimes failed: %v", err)
		}
		if len(result) != 0 {
			t.Errorf("GetUserRuntimes returned %d runtimes for user with offline daemon, want 0",
				len(result))
		}
	})
}

// TestProperty_RuntimeFiltering_AllUnavailableReturnsEmpty verifies that
// for any user with all runtimes having status "unavailable",
// GetUserRuntimes returns empty.
func TestProperty_RuntimeFiltering_AllUnavailableReturnsEmpty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		svc := NewInMemoryRuntimeService()
		ctx := context.Background()

		// Generate 2-5 users.
		numUsers := rapid.IntRange(2, 5).Draw(t, "num_users")

		runtimeIdx := 0
		for i := range numUsers {
			userID := fmt.Sprintf("user-%d", i)
			daemonID := fmt.Sprintf("daemon-%d", i)

			numRuntimes := rapid.IntRange(1, 5).Draw(t, fmt.Sprintf("num_runtimes_%d", i))
			runtimes := make([]protocol.RuntimeInfo, numRuntimes)
			for j := range numRuntimes {
				// All runtimes for this user are "unavailable".
				runtimes[j] = protocol.RuntimeInfo{
					AgentType:  fmt.Sprintf("agent-%d", runtimeIdx),
					BinaryPath: fmt.Sprintf("/usr/bin/agent-%d", runtimeIdx),
					Version:    "1.0.0",
					Status:     "unavailable",
				}
				runtimeIdx++
			}

			err := svc.RegisterDaemon(ctx, DaemonRegistration{
				DaemonID: daemonID,
				UserID:   userID,
				Runtimes: runtimes,
			})
			if err != nil {
				t.Fatalf("RegisterDaemon failed: %v", err)
			}
		}

		// For every user, GetUserRuntimes should return empty since all are unavailable.
		for i := range numUsers {
			userID := fmt.Sprintf("user-%d", i)
			result, err := svc.GetUserRuntimes(ctx, userID)
			if err != nil {
				t.Fatalf("GetUserRuntimes failed for %s: %v", userID, err)
			}
			if len(result) != 0 {
				t.Errorf("GetUserRuntimes returned %d runtimes for user %s (all unavailable), want 0",
					len(result), userID)
			}
		}
	})
}
