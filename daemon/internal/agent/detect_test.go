package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

// --- Test helpers ---

// mockLookPath returns a LookPathFunc that resolves binaries from a map.
func mockLookPath(available map[string]string) LookPathFunc {
	return func(file string) (string, error) {
		if path, ok := available[file]; ok {
			return path, nil
		}
		return "", errors.New("not found")
	}
}

// mockEnvLookup returns an EnvLookupFunc that resolves env vars from a map.
func mockEnvLookup(envVars map[string]string) EnvLookupFunc {
	return func(key string) string {
		return envVars[key]
	}
}

// mockCommandRunner returns a CommandRunner that returns predefined outputs.
func mockCommandRunner(outputs map[string]string, failSet map[string]bool) CommandRunner {
	return func(ctx context.Context, path string, args ...string) (string, error) {
		if failSet != nil && failSet[path] {
			return "", errors.New("command failed")
		}
		if output, ok := outputs[path]; ok {
			return output, nil
		}
		return "", errors.New("command not found")
	}
}

// timeoutCommandRunner returns a CommandRunner that simulates a timeout error.
func timeoutCommandRunner() CommandRunner {
	return func(ctx context.Context, path string, args ...string) (string, error) {
		return "", context.DeadlineExceeded
	}
}

// --- Unit Tests ---

func TestNewDetector_DefaultInterval(t *testing.T) {
	d := NewDetector(0)
	if d.RescanInterval() != DefaultRescanInterval {
		t.Errorf("expected default interval %v, got %v", DefaultRescanInterval, d.RescanInterval())
	}
}

func TestNewDetector_CustomInterval(t *testing.T) {
	interval := 30 * time.Second
	d := NewDetector(interval)
	if d.RescanInterval() != interval {
		t.Errorf("expected interval %v, got %v", interval, d.RescanInterval())
	}
}

func TestScan_NoBinariesFound(t *testing.T) {
	d := NewDetector(60*time.Second,
		WithLookPath(mockLookPath(map[string]string{})),
		WithEnvLookup(mockEnvLookup(map[string]string{})),
		WithCommandRunner(mockCommandRunner(nil, nil)),
	)

	runtimes := d.Scan()
	if len(runtimes) != 0 {
		t.Errorf("expected empty runtimes, got %d entries", len(runtimes))
	}
}

func TestScan_BinaryFoundWithVersion(t *testing.T) {
	d := NewDetector(60*time.Second,
		WithLookPath(mockLookPath(map[string]string{
			"claude": "/usr/local/bin/claude",
		})),
		WithEnvLookup(mockEnvLookup(map[string]string{})),
		WithCommandRunner(mockCommandRunner(
			map[string]string{"/usr/local/bin/claude": "claude v1.2.3\n"},
			nil,
		)),
	)

	runtimes := d.Scan()
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}

	rt := runtimes[0]
	assertRuntime(t, rt, "claude", "/usr/local/bin/claude", "claude v1.2.3", "available")
}

func TestScan_BinaryFoundVersionFails(t *testing.T) {
	d := NewDetector(60*time.Second,
		WithLookPath(mockLookPath(map[string]string{
			"gemini": "/usr/bin/gemini",
		})),
		WithEnvLookup(mockEnvLookup(map[string]string{})),
		WithCommandRunner(mockCommandRunner(
			nil,
			map[string]bool{"/usr/bin/gemini": true},
		)),
	)

	runtimes := d.Scan()
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}

	rt := runtimes[0]
	assertRuntime(t, rt, "gemini", "/usr/bin/gemini", "unknown", "unavailable")
}

func TestScan_EnvOverrideTakesPrecedence(t *testing.T) {
	d := NewDetector(60*time.Second,
		WithLookPath(mockLookPath(map[string]string{
			"claude": "/usr/local/bin/claude",
		})),
		WithEnvLookup(mockEnvLookup(map[string]string{
			"MULTICA_CLAUDE_PATH": "/custom/path/claude",
		})),
		WithCommandRunner(mockCommandRunner(
			map[string]string{"/custom/path/claude": "claude v2.0.0\n"},
			nil,
		)),
	)

	runtimes := d.Scan()
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}

	rt := runtimes[0]
	// Should use the env override path, not the PATH lookup
	assertRuntime(t, rt, "claude", "/custom/path/claude", "claude v2.0.0", "available")
}

func TestScan_EnvOverrideEmptyFallsBackToPath(t *testing.T) {
	d := NewDetector(60*time.Second,
		WithLookPath(mockLookPath(map[string]string{
			"claude": "/usr/local/bin/claude",
		})),
		WithEnvLookup(mockEnvLookup(map[string]string{
			"MULTICA_CLAUDE_PATH": "", // empty means not set
		})),
		WithCommandRunner(mockCommandRunner(
			map[string]string{"/usr/local/bin/claude": "claude v1.0.0\n"},
			nil,
		)),
	)

	runtimes := d.Scan()
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}

	rt := runtimes[0]
	assertRuntime(t, rt, "claude", "/usr/local/bin/claude", "claude v1.0.0", "available")
}

func TestScan_MultipleBinaries(t *testing.T) {
	d := NewDetector(60*time.Second,
		WithLookPath(mockLookPath(map[string]string{
			"claude":  "/usr/local/bin/claude",
			"gemini":  "/usr/bin/gemini",
			"copilot": "/usr/bin/copilot",
		})),
		WithEnvLookup(mockEnvLookup(map[string]string{})),
		WithCommandRunner(mockCommandRunner(
			map[string]string{
				"/usr/local/bin/claude": "claude v1.2.3\n",
				"/usr/bin/gemini":       "gemini 0.5.1\n",
			},
			map[string]bool{"/usr/bin/copilot": true},
		)),
	)

	runtimes := d.Scan()
	if len(runtimes) != 3 {
		t.Fatalf("expected 3 runtimes, got %d", len(runtimes))
	}

	// Verify order matches SupportedAgents order
	assertRuntime(t, runtimes[0], "claude", "/usr/local/bin/claude", "claude v1.2.3", "available")
	assertRuntime(t, runtimes[1], "gemini", "/usr/bin/gemini", "gemini 0.5.1", "available")
	assertRuntime(t, runtimes[2], "copilot", "/usr/bin/copilot", "unknown", "unavailable")
}

func TestScan_TimeoutHandling(t *testing.T) {
	// Use a very short timeout for testing
	d := NewDetector(60*time.Second,
		WithLookPath(mockLookPath(map[string]string{
			"claude": "/usr/local/bin/claude",
		})),
		WithEnvLookup(mockEnvLookup(map[string]string{})),
		WithCommandRunner(timeoutCommandRunner()),
	)

	runtimes := d.Scan()
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}

	rt := runtimes[0]
	assertRuntime(t, rt, "claude", "/usr/local/bin/claude", "unknown", "unavailable")
}

func TestScan_OnlyEnvOverrideNoPathBinary(t *testing.T) {
	// Agent not in PATH but available via env override
	d := NewDetector(60*time.Second,
		WithLookPath(mockLookPath(map[string]string{})), // nothing in PATH
		WithEnvLookup(mockEnvLookup(map[string]string{
			"MULTICA_KIRO_CLI_PATH": "/opt/kiro/kiro-cli",
		})),
		WithCommandRunner(mockCommandRunner(
			map[string]string{"/opt/kiro/kiro-cli": "kiro-cli 3.1.0\n"},
			nil,
		)),
	)

	runtimes := d.Scan()
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}

	rt := runtimes[0]
	assertRuntime(t, rt, "kiro-cli", "/opt/kiro/kiro-cli", "kiro-cli 3.1.0", "available")
}

func TestScan_MultilineVersionOutput(t *testing.T) {
	d := NewDetector(60*time.Second,
		WithLookPath(mockLookPath(map[string]string{
			"codex": "/usr/bin/codex",
		})),
		WithEnvLookup(mockEnvLookup(map[string]string{})),
		WithCommandRunner(mockCommandRunner(
			map[string]string{"/usr/bin/codex": "codex version 2.1.0\nBuild: abc123\nDate: 2024-01-01\n"},
			nil,
		)),
	)

	runtimes := d.Scan()
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}

	rt := runtimes[0]
	// Should only take the first line
	assertRuntime(t, rt, "codex", "/usr/bin/codex", "codex version 2.1.0", "available")
}

func TestScan_EmptyVersionOutput(t *testing.T) {
	d := NewDetector(60*time.Second,
		WithLookPath(mockLookPath(map[string]string{
			"pi": "/usr/bin/pi",
		})),
		WithEnvLookup(mockEnvLookup(map[string]string{})),
		WithCommandRunner(mockCommandRunner(
			map[string]string{"/usr/bin/pi": ""},
			nil,
		)),
	)

	runtimes := d.Scan()
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime, got %d", len(runtimes))
	}

	rt := runtimes[0]
	// Empty output should result in "unknown" version but still "available"
	// since the command succeeded (exit code 0)
	assertRuntime(t, rt, "pi", "/usr/bin/pi", "unknown", "available")
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude v1.2.3", "claude v1.2.3"},
		{"  claude v1.2.3  ", "claude v1.2.3"},
		{"v1.0.0\nsome extra info", "v1.0.0"},
		{"", "unknown"},
		{"   ", "unknown"},
		{"\n\n", "unknown"},
		{"gemini 0.5.1\n", "gemini 0.5.1"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("input=%q", tt.input), func(t *testing.T) {
			result := parseVersion(tt.input)
			if result != tt.expected {
				t.Errorf("parseVersion(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRescanInterval(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected time.Duration
	}{
		{0, DefaultRescanInterval},
		{-1 * time.Second, DefaultRescanInterval},
		{30 * time.Second, 30 * time.Second},
		{120 * time.Second, 120 * time.Second},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("interval=%v", tt.input), func(t *testing.T) {
			d := NewDetector(tt.input)
			if d.RescanInterval() != tt.expected {
				t.Errorf("RescanInterval() = %v, want %v", d.RescanInterval(), tt.expected)
			}
		})
	}
}

func TestSupportedAgentsList(t *testing.T) {
	expected := []string{
		"claude", "kiro-cli", "gemini", "codex", "copilot",
		"opencode", "hermes", "pi", "cursor-agent", "kimi",
	}

	if len(SupportedAgents) != len(expected) {
		t.Fatalf("expected %d supported agents, got %d", len(expected), len(SupportedAgents))
	}

	for i, name := range expected {
		if SupportedAgents[i] != name {
			t.Errorf("SupportedAgents[%d] = %q, want %q", i, SupportedAgents[i], name)
		}
	}
}

func TestEnvOverrides_AllAgentsHaveOverride(t *testing.T) {
	for _, agentName := range SupportedAgents {
		if _, ok := EnvOverrides[agentName]; !ok {
			t.Errorf("agent %q has no env override defined", agentName)
		}
	}
}

func TestDetectorImplementsInterface(t *testing.T) {
	// Compile-time check that Detector implements AgentDetector
	var _ AgentDetector = (*Detector)(nil)
}

// --- Helpers ---

func assertRuntime(t *testing.T, rt protocol.RuntimeInfo, agentType, binaryPath, version, status string) {
	t.Helper()
	if rt.AgentType != agentType {
		t.Errorf("AgentType = %q, want %q", rt.AgentType, agentType)
	}
	if rt.BinaryPath != binaryPath {
		t.Errorf("BinaryPath = %q, want %q", rt.BinaryPath, binaryPath)
	}
	if rt.Version != version {
		t.Errorf("Version = %q, want %q", rt.Version, version)
	}
	if rt.Status != status {
		t.Errorf("Status = %q, want %q", rt.Status, status)
	}
}
