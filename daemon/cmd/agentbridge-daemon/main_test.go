package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

func TestIsAlreadyRunning_NoPIDFile(t *testing.T) {
	// No PID file should mean not running.
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "daemon.pid")

	if isAlreadyRunning(pidPath) {
		t.Error("expected isAlreadyRunning to return false when PID file does not exist")
	}
}

func TestIsAlreadyRunning_InvalidPIDContent(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "daemon.pid")

	// Write non-numeric content.
	if err := os.WriteFile(pidPath, []byte("not-a-pid"), 0o644); err != nil {
		t.Fatal(err)
	}

	if isAlreadyRunning(pidPath) {
		t.Error("expected isAlreadyRunning to return false for invalid PID content")
	}
}

func TestIsAlreadyRunning_StalePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "daemon.pid")

	// Use a PID that almost certainly doesn't exist (very high number).
	stalePID := 99999999
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(stalePID)), 0o644); err != nil {
		t.Fatal(err)
	}

	if isAlreadyRunning(pidPath) {
		t.Error("expected isAlreadyRunning to return false for stale PID")
	}
}

func TestIsAlreadyRunning_CurrentProcess(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "daemon.pid")

	// Write our own PID — we are definitely running.
	currentPID := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(currentPID)), 0o644); err != nil {
		t.Fatal(err)
	}

	if !isAlreadyRunning(pidPath) {
		t.Error("expected isAlreadyRunning to return true for current process PID")
	}
}

func TestGetDataDir_Default(t *testing.T) {
	// Unset the override env var.
	t.Setenv("AGENTBRIDGE_DATA_DIR", "")

	dir, err := getDataDir()
	if err != nil {
		t.Fatal(err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	expected := filepath.Join(home, ".agentbridge")
	if dir != expected {
		t.Errorf("expected %s, got %s", expected, dir)
	}
}

func TestGetDataDir_Override(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	dir, err := getDataDir()
	if err != nil {
		t.Fatal(err)
	}

	if dir != tmpDir {
		t.Errorf("expected %s, got %s", tmpDir, dir)
	}
}

func TestLoadConfig_MissingToken(t *testing.T) {
	// No env var token and no config file — should fail with guidance message.
	t.Setenv("AGENTBRIDGE_TOKEN", "")
	t.Setenv("AGENTBRIDGE_USER_ID", "user-123")

	// Point HOME to a temp dir so no config file is found.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	_, err := loadConfig()
	if err == nil {
		t.Error("expected error when no token is available")
	}
	if err != nil && !strings.Contains(err.Error(), "agentbridge-daemon login") {
		t.Errorf("expected guidance message mentioning 'agentbridge-daemon login', got: %v", err)
	}
}

func TestLoadConfig_ErrorMessageDoesNotExposeToken(t *testing.T) {
	// When loadConfig fails, the error message should never contain token values.
	// Test with a whitespace-only token (treated as empty).
	t.Setenv("AGENTBRIDGE_TOKEN", "   ")
	t.Setenv("AGENTBRIDGE_USER_ID", "user-123")

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	// Create a config file with a whitespace-only token.
	configDir := filepath.Join(tmpDir, ".agentbridge")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"token": "   "}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := loadConfig()
	if err == nil {
		t.Fatal("expected error when token is whitespace-only")
	}

	// The error message should not contain any token-like values.
	errMsg := err.Error()
	if strings.Contains(errMsg, "ab_") {
		t.Errorf("error message should not expose token prefix, got: %v", errMsg)
	}
	// Verify it gives guidance instead.
	if !strings.Contains(errMsg, "agentbridge-daemon login") {
		t.Errorf("expected guidance message, got: %v", errMsg)
	}
}

func TestLoadConfig_MissingUserID_WithEnvToken(t *testing.T) {
	// When token comes from env var, AGENTBRIDGE_USER_ID is required.
	t.Setenv("AGENTBRIDGE_TOKEN", "some-token")
	t.Setenv("AGENTBRIDGE_USER_ID", "")

	_, err := loadConfig()
	if err == nil {
		t.Error("expected error when AGENTBRIDGE_USER_ID is missing with env var token")
	}
}

func TestLoadConfig_MissingUserID_WithConfigFileToken(t *testing.T) {
	// When token comes from config file, AGENTBRIDGE_USER_ID is NOT required.
	t.Setenv("AGENTBRIDGE_TOKEN", "")
	t.Setenv("AGENTBRIDGE_USER_ID", "")

	// Create a config file with a token.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	configDir := filepath.Join(tmpDir, ".agentbridge")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configData := []byte(`{"token": "ab_testtoken1234567890abcdef1234567890abcdef1234567890abcdef12345678"}`)
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), configData, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("expected no error when token comes from config file without USER_ID, got: %v", err)
	}

	if cfg.UserID != "" {
		t.Errorf("expected empty UserID when token comes from config file, got %q", cfg.UserID)
	}
}

func TestLoadConfig_ConfigFileToken_IgnoresUserID(t *testing.T) {
	// When token comes from config file, AGENTBRIDGE_USER_ID should be ignored.
	t.Setenv("AGENTBRIDGE_TOKEN", "")
	t.Setenv("AGENTBRIDGE_USER_ID", "should-be-ignored")

	// Create a config file with a token.
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	configDir := filepath.Join(tmpDir, ".agentbridge")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configData := []byte(`{"token": "ab_testtoken1234567890abcdef1234567890abcdef1234567890abcdef12345678"}`)
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), configData, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.UserID != "" {
		t.Errorf("expected UserID to be ignored (empty) when token comes from config file, got %q", cfg.UserID)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("AGENTBRIDGE_TOKEN", "test-token")
	t.Setenv("AGENTBRIDGE_USER_ID", "user-123")
	t.Setenv("AGENTBRIDGE_SERVER_URL", "")
	t.Setenv("AGENTBRIDGE_DAEMON_ID", "")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ServerURL != defaultServerURL {
		t.Errorf("expected default server URL %s, got %s", defaultServerURL, cfg.ServerURL)
	}

	if cfg.Token != "test-token" {
		t.Errorf("expected token 'test-token', got %s", cfg.Token)
	}

	if cfg.UserID != "user-123" {
		t.Errorf("expected user ID 'user-123', got %s", cfg.UserID)
	}

	// DaemonID should be hostname-based.
	hostname, _ := os.Hostname()
	expectedDaemonID := "daemon-" + hostname
	if cfg.DaemonID != expectedDaemonID {
		t.Errorf("expected daemon ID %s, got %s", expectedDaemonID, cfg.DaemonID)
	}
}

func TestLoadConfig_CustomValues(t *testing.T) {
	t.Setenv("AGENTBRIDGE_TOKEN", "my-token")
	t.Setenv("AGENTBRIDGE_USER_ID", "user-456")
	t.Setenv("AGENTBRIDGE_SERVER_URL", "ws://custom:9090/ws/daemon")
	t.Setenv("AGENTBRIDGE_DAEMON_ID", "my-daemon")

	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ServerURL != "ws://custom:9090/ws/daemon" {
		t.Errorf("expected custom server URL, got %s", cfg.ServerURL)
	}

	if cfg.DaemonID != "my-daemon" {
		t.Errorf("expected daemon ID 'my-daemon', got %s", cfg.DaemonID)
	}
}

// --- Stop command tests ---

func TestRunStop_NoPIDFile(t *testing.T) {
	// When no PID file exists, stop should output "no running daemon found" and succeed.
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	err := runStop()
	if err != nil {
		t.Errorf("expected no error when no PID file exists, got: %v", err)
	}
}

func TestRunStop_InvalidPIDContent(t *testing.T) {
	// When PID file contains non-numeric content, stop should clean up and succeed.
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte("not-a-number"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runStop()
	if err != nil {
		t.Errorf("expected no error for invalid PID content, got: %v", err)
	}

	// PID file should be cleaned up.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("expected PID file to be removed after invalid content")
	}
}

func TestRunStop_StalePID(t *testing.T) {
	// When PID file references a non-running process, stop should clean up and succeed.
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	pidPath := filepath.Join(tmpDir, pidFileName)
	// Use a PID that almost certainly doesn't exist.
	stalePID := 99999999
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(stalePID)), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runStop()
	if err != nil {
		t.Errorf("expected no error for stale PID, got: %v", err)
	}

	// PID file should be cleaned up.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("expected PID file to be removed after stale PID")
	}
}

func TestRunStop_RunningProcess(t *testing.T) {
	// Start a long-running process, write its PID, then stop it.
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	// Start tail -f /dev/null which exits immediately on SIGTERM.
	cmd := exec.Command("tail", "-f", "/dev/null")
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	defer cmd.Wait() // avoid zombies

	pid := cmd.Process.Pid
	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		cmd.Process.Kill()
		t.Fatal(err)
	}

	// Verify the process is running.
	proc, _ := os.FindProcess(pid)
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("test process not running before stop: %v", err)
	}

	// Run stop — should succeed quickly since tail responds to SIGTERM.
	err := runStop()
	if err != nil {
		t.Errorf("expected no error stopping running process, got: %v", err)
	}

	// PID file should be cleaned up.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("expected PID file to be removed after stop")
	}
}


// --- Status command tests ---

func TestRunStatus_NoDaemon(t *testing.T) {
	// When no PID file exists, status should report "not running".
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	err := runStatus()
	if err != nil {
		t.Errorf("expected no error when no daemon is running, got: %v", err)
	}
}

func TestRunStatus_DaemonRunningNoStatusFile(t *testing.T) {
	// When PID file exists (daemon running) but no status file, report "starting".
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	// Write our own PID to simulate a running daemon.
	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runStatus()
	if err != nil {
		t.Errorf("expected no error when status file is missing, got: %v", err)
	}
}

func TestRunStatus_ValidStatusFile(t *testing.T) {
	// When a valid status file exists, status should report all fields.
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	// Write our own PID to simulate a running daemon.
	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a valid status file.
	status := DaemonStatus{
		ConnectionState: "connected",
		StartedAt:       time.Now().Add(-5 * time.Minute),
		Agents: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.2.3", Status: "available"},
			{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "unknown", Status: "unavailable"},
		},
		TaskStatus: "idle",
		PID:        os.Getpid(),
		UpdatedAt:  time.Now(),
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}

	statusPath := filepath.Join(tmpDir, statusFileName)
	if err := os.WriteFile(statusPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	err = runStatus()
	if err != nil {
		t.Errorf("expected no error for valid status file, got: %v", err)
	}
}

func TestRunStatus_StaleStatusFile(t *testing.T) {
	// When the status file is older than 10 seconds, report "stale".
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	// Write our own PID to simulate a running daemon.
	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write a stale status file (updated 30 seconds ago).
	status := DaemonStatus{
		ConnectionState: "connected",
		StartedAt:       time.Now().Add(-10 * time.Minute),
		Agents:          []protocol.RuntimeInfo{},
		TaskStatus:      "idle",
		PID:             os.Getpid(),
		UpdatedAt:       time.Now().Add(-30 * time.Second),
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}

	statusPath := filepath.Join(tmpDir, statusFileName)
	if err := os.WriteFile(statusPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	err = runStatus()
	if err != nil {
		t.Errorf("expected no error for stale status file, got: %v", err)
	}
}

func TestRunStatus_InvalidStatusFile(t *testing.T) {
	// When the status file contains invalid JSON, report an error.
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	// Write our own PID to simulate a running daemon.
	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write invalid JSON.
	statusPath := filepath.Join(tmpDir, statusFileName)
	if err := os.WriteFile(statusPath, []byte("not valid json"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := runStatus()
	if err == nil {
		t.Error("expected error for invalid status file JSON")
	}
}

func TestRunStatus_DisconnectedState(t *testing.T) {
	// Verify status correctly reports disconnected state.
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	status := DaemonStatus{
		ConnectionState: "disconnected",
		StartedAt:       time.Now().Add(-2 * time.Minute),
		Agents:          []protocol.RuntimeInfo{},
		TaskStatus:      "idle",
		PID:             os.Getpid(),
		UpdatedAt:       time.Now(),
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}

	statusPath := filepath.Join(tmpDir, statusFileName)
	if err := os.WriteFile(statusPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	err = runStatus()
	if err != nil {
		t.Errorf("expected no error for disconnected state, got: %v", err)
	}
}

func TestRunStatus_ExecutingTask(t *testing.T) {
	// Verify status correctly reports executing task state.
	tmpDir := t.TempDir()
	t.Setenv("AGENTBRIDGE_DATA_DIR", tmpDir)

	pidPath := filepath.Join(tmpDir, pidFileName)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}

	status := DaemonStatus{
		ConnectionState: "connected",
		StartedAt:       time.Now().Add(-1 * time.Minute),
		Agents: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "3.0.0", Status: "available"},
		},
		TaskStatus: "executing",
		PID:        os.Getpid(),
		UpdatedAt:  time.Now(),
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}

	statusPath := filepath.Join(tmpDir, statusFileName)
	if err := os.WriteFile(statusPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	err = runStatus()
	if err != nil {
		t.Errorf("expected no error for executing task state, got: %v", err)
	}
}

func TestDaemonStatus_JSONRoundTrip(t *testing.T) {
	// Verify DaemonStatus serializes and deserializes correctly.
	original := DaemonStatus{
		ConnectionState: "reconnecting",
		StartedAt:       time.Now().Add(-30 * time.Minute).Truncate(time.Millisecond),
		Agents: []protocol.RuntimeInfo{
			{AgentType: "claude", BinaryPath: "/usr/local/bin/claude", Version: "2.1.0", Status: "available"},
			{AgentType: "kiro-cli", BinaryPath: "/usr/bin/kiro-cli", Version: "1.0.0", Status: "available"},
			{AgentType: "codex", BinaryPath: "/opt/codex", Version: "unknown", Status: "unavailable"},
		},
		TaskStatus: "idle",
		PID:        12345,
		UpdatedAt:  time.Now().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded DaemonStatus
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ConnectionState != original.ConnectionState {
		t.Errorf("ConnectionState: got %q, want %q", decoded.ConnectionState, original.ConnectionState)
	}
	if decoded.TaskStatus != original.TaskStatus {
		t.Errorf("TaskStatus: got %q, want %q", decoded.TaskStatus, original.TaskStatus)
	}
	if decoded.PID != original.PID {
		t.Errorf("PID: got %d, want %d", decoded.PID, original.PID)
	}
	if len(decoded.Agents) != len(original.Agents) {
		t.Fatalf("Agents count: got %d, want %d", len(decoded.Agents), len(original.Agents))
	}
	for i, a := range decoded.Agents {
		if a.AgentType != original.Agents[i].AgentType {
			t.Errorf("Agent[%d].AgentType: got %q, want %q", i, a.AgentType, original.Agents[i].AgentType)
		}
		if a.Version != original.Agents[i].Version {
			t.Errorf("Agent[%d].Version: got %q, want %q", i, a.Version, original.Agents[i].Version)
		}
		if a.Status != original.Agents[i].Status {
			t.Errorf("Agent[%d].Status: got %q, want %q", i, a.Status, original.Agents[i].Status)
		}
	}
	if !decoded.StartedAt.Equal(original.StartedAt) {
		t.Errorf("StartedAt: got %v, want %v", decoded.StartedAt, original.StartedAt)
	}
	if !decoded.UpdatedAt.Equal(original.UpdatedAt) {
		t.Errorf("UpdatedAt: got %v, want %v", decoded.UpdatedAt, original.UpdatedAt)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Second, "5s"},
		{45 * time.Second, "45s"},
		{90 * time.Second, "2m"},
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1.5h"},
		{3 * time.Hour, "3.0h"},
	}

	for _, tt := range tests {
		got := formatDuration(tt.d)
		if got != tt.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestBuildRuntimeMap_Empty(t *testing.T) {
	m := buildRuntimeMap(nil)
	if len(m) != 0 {
		t.Errorf("expected empty map, got %d entries", len(m))
	}
}

func TestBuildRuntimeMap_AvailableRuntimes(t *testing.T) {
	runtimes := []protocol.RuntimeInfo{
		{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"},
		{AgentType: "gemini", BinaryPath: "/usr/bin/gemini", Version: "2.0", Status: "available"},
	}

	m := buildRuntimeMap(runtimes)

	// Should map agent type to binary path.
	if path, ok := m["claude"]; !ok || path != "/usr/bin/claude" {
		t.Errorf("expected claude -> /usr/bin/claude, got %q (ok=%v)", path, ok)
	}
	if path, ok := m["gemini"]; !ok || path != "/usr/bin/gemini" {
		t.Errorf("expected gemini -> /usr/bin/gemini, got %q (ok=%v)", path, ok)
	}

	// Should also map binary path to itself.
	if path, ok := m["/usr/bin/claude"]; !ok || path != "/usr/bin/claude" {
		t.Errorf("expected /usr/bin/claude -> /usr/bin/claude, got %q (ok=%v)", path, ok)
	}
}

func TestBuildRuntimeMap_UnavailableExcluded(t *testing.T) {
	runtimes := []protocol.RuntimeInfo{
		{AgentType: "claude", BinaryPath: "/usr/bin/claude", Version: "1.0", Status: "available"},
		{AgentType: "codex", BinaryPath: "/usr/bin/codex", Version: "unknown", Status: "unavailable"},
	}

	m := buildRuntimeMap(runtimes)

	// Available runtime should be in the map.
	if _, ok := m["claude"]; !ok {
		t.Error("expected claude to be in the map")
	}

	// Unavailable runtime should NOT be in the map.
	if _, ok := m["codex"]; ok {
		t.Error("expected codex to NOT be in the map (unavailable)")
	}
}

func TestBuildRuntimeMap_EmptyBinaryPathExcluded(t *testing.T) {
	runtimes := []protocol.RuntimeInfo{
		{AgentType: "claude", BinaryPath: "", Version: "1.0", Status: "available"},
	}

	m := buildRuntimeMap(runtimes)

	// Runtime with empty binary path should not be in the map.
	if _, ok := m["claude"]; ok {
		t.Error("expected claude with empty binary path to NOT be in the map")
	}
}
