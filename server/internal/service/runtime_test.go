package service

import (
	"context"
	"testing"

	"github.com/user/agentbridge/server/pkg/protocol"
)

func TestRegisterDaemon_CreatesRecords(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	reg := DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
			{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "2.0.0", Status: "available"},
		},
	}

	err := svc.RegisterDaemon(ctx, reg)
	if err != nil {
		t.Fatalf("RegisterDaemon failed: %v", err)
	}

	// Verify daemon was created.
	daemon, exists := svc.daemons["daemon-1"]
	if !exists {
		t.Fatal("daemon record not created")
	}
	if daemon.UserID != "user-1" {
		t.Errorf("daemon.UserID = %q, want %q", daemon.UserID, "user-1")
	}
	if daemon.Status != "online" {
		t.Errorf("daemon.Status = %q, want %q", daemon.Status, "online")
	}

	// Verify runtimes were created.
	runtimes := getRuntimesForDaemon(svc, "daemon-1")
	if len(runtimes) != 2 {
		t.Fatalf("got %d runtimes, want 2", len(runtimes))
	}

	// Verify runtime details.
	foundClaude := false
	foundGemini := false
	for _, rt := range runtimes {
		switch rt.AgentType {
		case "claude":
			foundClaude = true
			if rt.Version != "1.0.0" {
				t.Errorf("claude version = %q, want %q", rt.Version, "1.0.0")
			}
			if rt.Status != "available" {
				t.Errorf("claude status = %q, want %q", rt.Status, "available")
			}
		case "gemini":
			foundGemini = true
			if rt.Version != "2.0.0" {
				t.Errorf("gemini version = %q, want %q", rt.Version, "2.0.0")
			}
		}
	}
	if !foundClaude {
		t.Error("claude runtime not found")
	}
	if !foundGemini {
		t.Error("gemini runtime not found")
	}
}

func TestRegisterDaemon_ReplacesRuntimesOnReRegistration(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	// First registration with 2 runtimes.
	reg1 := DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
			{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "2.0.0", Status: "available"},
		},
	}
	if err := svc.RegisterDaemon(ctx, reg1); err != nil {
		t.Fatalf("first RegisterDaemon failed: %v", err)
	}

	// Re-register with different runtimes.
	reg2 := DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "kiro-cli", BinaryPath: "/usr/bin/kiro", Version: "3.0.0", Status: "available"},
		},
	}
	if err := svc.RegisterDaemon(ctx, reg2); err != nil {
		t.Fatalf("second RegisterDaemon failed: %v", err)
	}

	// Verify old runtimes are gone and new ones are present.
	runtimes := getRuntimesForDaemon(svc, "daemon-1")
	if len(runtimes) != 1 {
		t.Fatalf("got %d runtimes after re-registration, want 1", len(runtimes))
	}
	if runtimes[0].AgentType != "kiro-cli" {
		t.Errorf("runtime agent_type = %q, want %q", runtimes[0].AgentType, "kiro-cli")
	}
	if runtimes[0].Version != "3.0.0" {
		t.Errorf("runtime version = %q, want %q", runtimes[0].Version, "3.0.0")
	}
}

func TestDeregisterDaemon_MarksOffline(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	reg := DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
	}
	if err := svc.RegisterDaemon(ctx, reg); err != nil {
		t.Fatalf("RegisterDaemon failed: %v", err)
	}

	err := svc.DeregisterDaemon(ctx, "daemon-1")
	if err != nil {
		t.Fatalf("DeregisterDaemon failed: %v", err)
	}

	// Verify daemon is offline.
	daemon := svc.daemons["daemon-1"]
	if daemon.Status != "offline" {
		t.Errorf("daemon.Status = %q, want %q", daemon.Status, "offline")
	}

	// Verify runtimes are offline.
	runtimes := getRuntimesForDaemon(svc, "daemon-1")
	for _, rt := range runtimes {
		if rt.Status != "offline" {
			t.Errorf("runtime %s status = %q, want %q", rt.ID, rt.Status, "offline")
		}
	}
}

func TestDeregisterDaemon_NotFound(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	err := svc.DeregisterDaemon(ctx, "nonexistent")
	if err != ErrDaemonNotFound {
		t.Errorf("DeregisterDaemon error = %v, want ErrDaemonNotFound", err)
	}
}

func TestGetUserRuntimes_FiltersCorrectly(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	// Register daemon for user-1 with mixed statuses.
	reg1 := DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
			{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "2.0.0", Status: "unavailable"},
		},
	}
	if err := svc.RegisterDaemon(ctx, reg1); err != nil {
		t.Fatalf("RegisterDaemon for user-1 failed: %v", err)
	}

	// Register daemon for user-2.
	reg2 := DaemonRegistration{
		DaemonID: "daemon-2",
		UserID:   "user-2",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "codex", BinaryPath: "/usr/bin/codex", Version: "1.0.0", Status: "available"},
		},
	}
	if err := svc.RegisterDaemon(ctx, reg2); err != nil {
		t.Fatalf("RegisterDaemon for user-2 failed: %v", err)
	}

	// Get runtimes for user-1: should only get the "available" claude runtime.
	runtimes, err := svc.GetUserRuntimes(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetUserRuntimes failed: %v", err)
	}
	if len(runtimes) != 1 {
		t.Fatalf("got %d runtimes for user-1, want 1", len(runtimes))
	}
	if runtimes[0].AgentType != "claude" {
		t.Errorf("runtime agent_type = %q, want %q", runtimes[0].AgentType, "claude")
	}

	// Get runtimes for user-2: should get codex.
	runtimes2, err := svc.GetUserRuntimes(ctx, "user-2")
	if err != nil {
		t.Fatalf("GetUserRuntimes for user-2 failed: %v", err)
	}
	if len(runtimes2) != 1 {
		t.Fatalf("got %d runtimes for user-2, want 1", len(runtimes2))
	}
	if runtimes2[0].AgentType != "codex" {
		t.Errorf("runtime agent_type = %q, want %q", runtimes2[0].AgentType, "codex")
	}

	// Get runtimes for unknown user: should be empty.
	runtimes3, err := svc.GetUserRuntimes(ctx, "user-unknown")
	if err != nil {
		t.Fatalf("GetUserRuntimes for unknown user failed: %v", err)
	}
	if len(runtimes3) != 0 {
		t.Errorf("got %d runtimes for unknown user, want 0", len(runtimes3))
	}
}

func TestGetUserRuntimes_ExcludesOfflineDaemons(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	reg := DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
	}
	if err := svc.RegisterDaemon(ctx, reg); err != nil {
		t.Fatalf("RegisterDaemon failed: %v", err)
	}

	// Mark daemon offline.
	if err := svc.DeregisterDaemon(ctx, "daemon-1"); err != nil {
		t.Fatalf("DeregisterDaemon failed: %v", err)
	}

	// Should return no runtimes since daemon is offline.
	runtimes, err := svc.GetUserRuntimes(ctx, "user-1")
	if err != nil {
		t.Fatalf("GetUserRuntimes failed: %v", err)
	}
	if len(runtimes) != 0 {
		t.Errorf("got %d runtimes for offline daemon, want 0", len(runtimes))
	}
}

func TestMarkOffline(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	reg := DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
			{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "2.0.0", Status: "available"},
		},
	}
	if err := svc.RegisterDaemon(ctx, reg); err != nil {
		t.Fatalf("RegisterDaemon failed: %v", err)
	}

	err := svc.MarkOffline(ctx, "daemon-1")
	if err != nil {
		t.Fatalf("MarkOffline failed: %v", err)
	}

	// Verify daemon is offline.
	daemon := svc.daemons["daemon-1"]
	if daemon.Status != "offline" {
		t.Errorf("daemon.Status = %q, want %q", daemon.Status, "offline")
	}

	// Verify all runtimes are offline.
	runtimes := getRuntimesForDaemon(svc, "daemon-1")
	for _, rt := range runtimes {
		if rt.Status != "offline" {
			t.Errorf("runtime %s status = %q, want %q", rt.ID, rt.Status, "offline")
		}
	}
}

func TestMarkOffline_NotFound(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	err := svc.MarkOffline(ctx, "nonexistent")
	if err != ErrDaemonNotFound {
		t.Errorf("MarkOffline error = %v, want ErrDaemonNotFound", err)
	}
}

func TestUpdateHeartbeat(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	reg := DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{},
	}
	if err := svc.RegisterDaemon(ctx, reg); err != nil {
		t.Fatalf("RegisterDaemon failed: %v", err)
	}

	initialTime := svc.daemons["daemon-1"].LastSeenAt

	err := svc.UpdateHeartbeat(ctx, "daemon-1")
	if err != nil {
		t.Fatalf("UpdateHeartbeat failed: %v", err)
	}

	updatedTime := svc.daemons["daemon-1"].LastSeenAt
	if !updatedTime.After(initialTime) && !updatedTime.Equal(initialTime) {
		t.Errorf("LastSeenAt was not updated: initial=%v, updated=%v", initialTime, updatedTime)
	}
}

func TestUpdateHeartbeat_NotFound(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	err := svc.UpdateHeartbeat(ctx, "nonexistent")
	if err != ErrDaemonNotFound {
		t.Errorf("UpdateHeartbeat error = %v, want ErrDaemonNotFound", err)
	}
}

func TestBindRuntime(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	reg := DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
	}
	if err := svc.RegisterDaemon(ctx, reg); err != nil {
		t.Fatalf("RegisterDaemon failed: %v", err)
	}

	// Find the runtime ID.
	runtimes := getRuntimesForDaemon(svc, "daemon-1")
	if len(runtimes) == 0 {
		t.Fatal("no runtimes found")
	}
	runtimeID := runtimes[0].ID

	err := svc.BindRuntime(ctx, "session-1", runtimeID)
	if err != nil {
		t.Fatalf("BindRuntime failed: %v", err)
	}

	// Verify binding was stored.
	if svc.bindings["session-1"] != runtimeID {
		t.Errorf("binding = %q, want %q", svc.bindings["session-1"], runtimeID)
	}
}

func TestBindRuntime_NotFound(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	err := svc.BindRuntime(ctx, "session-1", "nonexistent-runtime")
	if err != ErrRuntimeNotFound {
		t.Errorf("BindRuntime error = %v, want ErrRuntimeNotFound", err)
	}
}

func TestBindRuntime_OfflineRuntime(t *testing.T) {
	svc := NewInMemoryRuntimeService()
	ctx := context.Background()

	reg := DaemonRegistration{
		DaemonID: "daemon-1",
		UserID:   "user-1",
		Runtimes: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0.0", Status: "available"},
		},
	}
	if err := svc.RegisterDaemon(ctx, reg); err != nil {
		t.Fatalf("RegisterDaemon failed: %v", err)
	}

	runtimes := getRuntimesForDaemon(svc, "daemon-1")
	runtimeID := runtimes[0].ID

	// Mark offline.
	if err := svc.MarkOffline(ctx, "daemon-1"); err != nil {
		t.Fatalf("MarkOffline failed: %v", err)
	}

	// Attempt to bind should fail.
	err := svc.BindRuntime(ctx, "session-1", runtimeID)
	if err != ErrRuntimeOffline {
		t.Errorf("BindRuntime error = %v, want ErrRuntimeOffline", err)
	}
}

// Helper function to get all runtimes for a daemon.
func getRuntimesForDaemon(svc *InMemoryRuntimeService, daemonID string) []Runtime {
	var result []Runtime
	for _, rt := range svc.runtimes {
		if rt.DaemonID == daemonID {
			result = append(result, *rt)
		}
	}
	return result
}
