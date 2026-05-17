package agent

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

// SupportedAgents is the list of agent binary names that the detector scans for.
var SupportedAgents = []string{
	"claude",
	"kiro-cli",
	"gemini",
	"codex",
	"copilot",
	"opencode",
	"hermes",
	"pi",
	"cursor-agent",
	"kimi",
}

// EnvOverrides maps agent binary names to environment variable names that can
// override the binary path (e.g., "claude" → "MULTICA_CLAUDE_PATH").
var EnvOverrides = map[string]string{
	"claude":       "MULTICA_CLAUDE_PATH",
	"kiro-cli":     "MULTICA_KIRO_CLI_PATH",
	"gemini":       "MULTICA_GEMINI_PATH",
	"codex":        "MULTICA_CODEX_PATH",
	"copilot":      "MULTICA_COPILOT_PATH",
	"opencode":     "MULTICA_OPENCODE_PATH",
	"hermes":       "MULTICA_HERMES_PATH",
	"pi":           "MULTICA_PI_PATH",
	"cursor-agent": "MULTICA_CURSOR_AGENT_PATH",
	"kimi":         "MULTICA_KIMI_PATH",
}

// DefaultRescanInterval is the default interval between agent detection scans.
const DefaultRescanInterval = 60 * time.Second

// VersionTimeout is the maximum time allowed for a version command to complete.
const VersionTimeout = 10 * time.Second

// AgentDetector scans for available agent CLIs.
type AgentDetector interface {
	Scan() []protocol.RuntimeInfo
	RescanInterval() time.Duration
}

// LookPathFunc is a function type for looking up binaries in PATH.
// This allows injection of custom lookup logic for testing.
type LookPathFunc func(file string) (string, error)

// EnvLookupFunc is a function type for looking up environment variables.
// This allows injection of custom env lookup logic for testing.
type EnvLookupFunc func(key string) string

// CommandRunner is a function type that executes a command and returns its output.
// This allows injection of custom command execution for testing.
type CommandRunner func(ctx context.Context, path string, args ...string) (string, error)

// Detector implements AgentDetector by scanning the system for supported agent binaries.
type Detector struct {
	rescanInterval time.Duration
	lookPath       LookPathFunc
	envLookup      EnvLookupFunc
	runCommand     CommandRunner
}

// DetectorOption is a functional option for configuring a Detector.
type DetectorOption func(*Detector)

// WithLookPath sets a custom LookPath function for the detector.
func WithLookPath(fn LookPathFunc) DetectorOption {
	return func(d *Detector) {
		d.lookPath = fn
	}
}

// WithEnvLookup sets a custom environment variable lookup function for the detector.
func WithEnvLookup(fn EnvLookupFunc) DetectorOption {
	return func(d *Detector) {
		d.envLookup = fn
	}
}

// WithCommandRunner sets a custom command runner for the detector.
func WithCommandRunner(fn CommandRunner) DetectorOption {
	return func(d *Detector) {
		d.runCommand = fn
	}
}

// NewDetector creates a new Detector with the given rescan interval and options.
// If interval is zero, DefaultRescanInterval is used.
func NewDetector(interval time.Duration, opts ...DetectorOption) *Detector {
	if interval <= 0 {
		interval = DefaultRescanInterval
	}
	d := &Detector{
		rescanInterval: interval,
		lookPath:       exec.LookPath,
		envLookup:      defaultEnvLookup,
		runCommand:     defaultCommandRunner,
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Scan scans the system for supported agent binaries and returns RuntimeInfo
// for each found binary. Binaries not found on the system are skipped.
func (d *Detector) Scan() []protocol.RuntimeInfo {
	var runtimes []protocol.RuntimeInfo

	for _, agentName := range SupportedAgents {
		binaryPath := d.findBinary(agentName)
		if binaryPath == "" {
			continue
		}

		version, err := d.getVersion(binaryPath)
		if err != nil {
			runtimes = append(runtimes, protocol.RuntimeInfo{
				AgentType:  agentName,
				BinaryPath: binaryPath,
				Version:    "unknown",
				Status:     "unavailable",
			})
			continue
		}

		runtimes = append(runtimes, protocol.RuntimeInfo{
			AgentType:  agentName,
			BinaryPath: binaryPath,
			Version:    version,
			Status:     "available",
		})
	}

	return runtimes
}

// RescanInterval returns the configured interval between detection scans.
func (d *Detector) RescanInterval() time.Duration {
	return d.rescanInterval
}

// findBinary looks up the binary path for an agent. It checks the environment
// variable override first, then falls back to exec.LookPath.
func (d *Detector) findBinary(agentName string) string {
	// Check environment variable override first
	if envVar, ok := EnvOverrides[agentName]; ok {
		if envPath := d.envLookup(envVar); envPath != "" {
			return envPath
		}
	}

	// Fall back to PATH lookup
	path, err := d.lookPath(agentName)
	if err != nil {
		return ""
	}
	return path
}

// getVersion executes the binary with --version flag and parses the output.
// It applies a 10-second timeout.
func (d *Detector) getVersion(binaryPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), VersionTimeout)
	defer cancel()

	output, err := d.runCommand(ctx, binaryPath, "--version")
	if err != nil {
		return "", err
	}

	return parseVersion(output), nil
}

// parseVersion extracts a version string from command output.
// It takes the first non-empty line and trims whitespace.
func parseVersion(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return "unknown"
	}

	// Take the first line of output
	lines := strings.SplitN(output, "\n", 2)
	version := strings.TrimSpace(lines[0])
	if version == "" {
		return "unknown"
	}
	return version
}

// defaultEnvLookup uses os.Getenv to look up environment variables.
func defaultEnvLookup(key string) string {
	// Import os at the top would create a circular dependency concern in tests,
	// so we use a function variable approach. In production, this calls os.Getenv.
	return getenv(key)
}

// defaultCommandRunner executes a command with the given context and returns stdout.
func defaultCommandRunner(ctx context.Context, path string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return stdout.String(), nil
}
