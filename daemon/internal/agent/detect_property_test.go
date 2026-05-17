package agent

import (
	"context"
	"errors"
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 1.1, 1.3, 1.4**
//
// Property 1: Agent Detection Correctness
// For any system PATH configuration containing a subset of supported agent binaries
// (with optional environment variable overrides), the detection scan SHALL return a
// RuntimeInfo for each found binary with correct agent_type, binary_path, and status
// ("available" if version retrieval succeeded, "unavailable" otherwise), and SHALL NOT
// include entries for binaries not present on the system.

// genAgentSubset generates a random non-empty subset of SupportedAgents.
func genAgentSubset(t *rapid.T) []string {
	// Generate a bitmask to select a subset of agents
	n := len(SupportedAgents)
	subset := make([]string, 0)
	for i := 0; i < n; i++ {
		if rapid.Bool().Draw(t, SupportedAgents[i]) {
			subset = append(subset, SupportedAgents[i])
		}
	}
	return subset
}

// genBinaryPath generates a plausible binary path for testing.
func genBinaryPath(t *rapid.T, label string) string {
	dirs := []string{"/usr/local/bin/", "/usr/bin/", "/opt/bin/", "/home/user/.local/bin/"}
	dir := rapid.SampledFrom(dirs).Draw(t, label+"-dir")
	return dir + label
}

func TestProperty_ScanReturnsExactlyPresentBinaries(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random subset of agents present in PATH
		presentAgents := genAgentSubset(t)

		// Build the PATH lookup map
		pathMap := make(map[string]string)
		for _, agent := range presentAgents {
			pathMap[agent] = "/usr/local/bin/" + agent
		}

		// All version commands succeed
		versionOutputs := make(map[string]string)
		for _, agent := range presentAgents {
			versionOutputs["/usr/local/bin/"+agent] = agent + " v1.0.0"
		}

		d := NewDetector(0,
			WithLookPath(mockLookPath(pathMap)),
			WithEnvLookup(mockEnvLookup(map[string]string{})),
			WithCommandRunner(mockCommandRunner(versionOutputs, nil)),
		)

		runtimes := d.Scan()

		// Property: Scan returns exactly the set of present agents (no more, no less)
		if len(runtimes) != len(presentAgents) {
			t.Fatalf("expected %d runtimes, got %d (present=%v)", len(presentAgents), len(runtimes), presentAgents)
		}

		// Build a set of returned agent types
		returnedAgents := make(map[string]bool)
		for _, rt := range runtimes {
			returnedAgents[rt.AgentType] = true
		}

		// Every present agent must be in the result
		for _, agent := range presentAgents {
			if !returnedAgents[agent] {
				t.Errorf("agent %q is present in PATH but not in scan results", agent)
			}
		}

		// No agent not in presentAgents should appear
		presentSet := make(map[string]bool)
		for _, agent := range presentAgents {
			presentSet[agent] = true
		}
		for _, rt := range runtimes {
			if !presentSet[rt.AgentType] {
				t.Errorf("agent %q is in scan results but not present in PATH", rt.AgentType)
			}
		}
	})
}

func TestProperty_AvailableStatusWhenVersionSucceeds(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random subset of agents
		presentAgents := genAgentSubset(t)
		if len(presentAgents) == 0 {
			return // skip empty subsets for this property
		}

		// All agents have successful version commands
		pathMap := make(map[string]string)
		versionOutputs := make(map[string]string)
		for _, agent := range presentAgents {
			path := "/usr/local/bin/" + agent
			pathMap[agent] = path
			// Generate a random version string
			major := rapid.IntRange(0, 99).Draw(t, agent+"-major")
			minor := rapid.IntRange(0, 99).Draw(t, agent+"-minor")
			patch := rapid.IntRange(0, 99).Draw(t, agent+"-patch")
			versionOutputs[path] = agent + " v" + itoa(major) + "." + itoa(minor) + "." + itoa(patch)
		}

		d := NewDetector(0,
			WithLookPath(mockLookPath(pathMap)),
			WithEnvLookup(mockEnvLookup(map[string]string{})),
			WithCommandRunner(mockCommandRunner(versionOutputs, nil)),
		)

		runtimes := d.Scan()

		// Property: Every runtime where version command succeeded has status "available"
		for _, rt := range runtimes {
			if rt.Status != "available" {
				t.Errorf("agent %q has status %q, expected 'available' (version command succeeded)", rt.AgentType, rt.Status)
			}
			if rt.Version == "unknown" {
				t.Errorf("agent %q has version 'unknown' but version command succeeded", rt.AgentType)
			}
		}
	})
}

func TestProperty_UnavailableStatusWhenVersionFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random subset of agents
		presentAgents := genAgentSubset(t)
		if len(presentAgents) == 0 {
			return // skip empty subsets for this property
		}

		// All agents have failing version commands
		pathMap := make(map[string]string)
		failSet := make(map[string]bool)
		for _, agent := range presentAgents {
			path := "/usr/local/bin/" + agent
			pathMap[agent] = path
			failSet[path] = true
		}

		d := NewDetector(0,
			WithLookPath(mockLookPath(pathMap)),
			WithEnvLookup(mockEnvLookup(map[string]string{})),
			WithCommandRunner(mockCommandRunner(nil, failSet)),
		)

		runtimes := d.Scan()

		// Property: Every runtime where version command failed has status "unavailable"
		for _, rt := range runtimes {
			if rt.Status != "unavailable" {
				t.Errorf("agent %q has status %q, expected 'unavailable' (version command failed)", rt.AgentType, rt.Status)
			}
			if rt.Version != "unknown" {
				t.Errorf("agent %q has version %q, expected 'unknown' (version command failed)", rt.AgentType, rt.Version)
			}
		}
	})
}

func TestProperty_ScanNeverReturnsAbsentAgents(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random subset of agents present in PATH
		presentAgents := genAgentSubset(t)

		presentSet := make(map[string]bool)
		pathMap := make(map[string]string)
		versionOutputs := make(map[string]string)
		for _, agent := range presentAgents {
			presentSet[agent] = true
			path := "/usr/local/bin/" + agent
			pathMap[agent] = path
			versionOutputs[path] = agent + " v1.0.0"
		}

		d := NewDetector(0,
			WithLookPath(mockLookPath(pathMap)),
			WithEnvLookup(mockEnvLookup(map[string]string{})),
			WithCommandRunner(mockCommandRunner(versionOutputs, nil)),
		)

		runtimes := d.Scan()

		// Property: No runtime in the result corresponds to an agent NOT in PATH or env
		for _, rt := range runtimes {
			if !presentSet[rt.AgentType] {
				t.Errorf("scan returned agent %q which is not present in PATH or env overrides", rt.AgentType)
			}
		}

		// Also verify: agents NOT present should NOT appear
		returnedSet := make(map[string]bool)
		for _, rt := range runtimes {
			returnedSet[rt.AgentType] = true
		}
		for _, agent := range SupportedAgents {
			if !presentSet[agent] && returnedSet[agent] {
				t.Errorf("agent %q is not present but appeared in scan results", agent)
			}
		}
	})
}

func TestProperty_EnvOverrideTakesPrecedenceOverPath(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Pick a random agent to have both PATH and env override
		agentIdx := rapid.IntRange(0, len(SupportedAgents)-1).Draw(t, "agentIdx")
		agent := SupportedAgents[agentIdx]

		pathBinary := "/usr/local/bin/" + agent
		envBinary := "/custom/override/" + agent

		// Both PATH and env override are set
		pathMap := map[string]string{agent: pathBinary}
		envKey := EnvOverrides[agent]
		envMap := map[string]string{envKey: envBinary}

		// Version command succeeds for the env override path
		versionOutputs := map[string]string{
			envBinary: agent + " v2.0.0",
		}

		d := NewDetector(0,
			WithLookPath(mockLookPath(pathMap)),
			WithEnvLookup(mockEnvLookup(envMap)),
			WithCommandRunner(mockCommandRunner(versionOutputs, nil)),
		)

		runtimes := d.Scan()

		// Property: The env override path is used, not the PATH binary
		if len(runtimes) != 1 {
			t.Fatalf("expected 1 runtime, got %d", len(runtimes))
		}

		rt := runtimes[0]
		if rt.BinaryPath != envBinary {
			t.Errorf("expected binary_path %q (env override), got %q", envBinary, rt.BinaryPath)
		}
		if rt.AgentType != agent {
			t.Errorf("expected agent_type %q, got %q", agent, rt.AgentType)
		}
	})
}

func TestProperty_MixedAvailabilityStatus(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random subset of agents
		presentAgents := genAgentSubset(t)
		if len(presentAgents) == 0 {
			return
		}

		// Randomly decide which agents have successful vs failed version commands
		pathMap := make(map[string]string)
		versionOutputs := make(map[string]string)
		failSet := make(map[string]bool)
		expectedStatus := make(map[string]string)

		for _, agent := range presentAgents {
			path := "/usr/local/bin/" + agent
			pathMap[agent] = path

			versionSucceeds := rapid.Bool().Draw(t, agent+"-version-ok")
			if versionSucceeds {
				versionOutputs[path] = agent + " v1.0.0"
				expectedStatus[agent] = "available"
			} else {
				failSet[path] = true
				expectedStatus[agent] = "unavailable"
			}
		}

		d := NewDetector(0,
			WithLookPath(mockLookPath(pathMap)),
			WithEnvLookup(mockEnvLookup(map[string]string{})),
			WithCommandRunner(mockCommandRunner(versionOutputs, failSet)),
		)

		runtimes := d.Scan()

		// Property: Each runtime has the correct status based on version command result
		for _, rt := range runtimes {
			expected, ok := expectedStatus[rt.AgentType]
			if !ok {
				t.Errorf("unexpected agent %q in results", rt.AgentType)
				continue
			}
			if rt.Status != expected {
				t.Errorf("agent %q: expected status %q, got %q", rt.AgentType, expected, rt.Status)
			}
		}
	})
}

func TestProperty_CorrectAgentTypeAndBinaryPath(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random subset of agents
		presentAgents := genAgentSubset(t)
		if len(presentAgents) == 0 {
			return
		}

		// Assign random paths to each agent
		pathMap := make(map[string]string)
		versionOutputs := make(map[string]string)
		expectedPaths := make(map[string]string)

		dirs := []string{"/usr/local/bin/", "/usr/bin/", "/opt/tools/", "/home/user/bin/"}

		for _, agent := range presentAgents {
			dir := rapid.SampledFrom(dirs).Draw(t, agent+"-dir")
			path := dir + agent
			pathMap[agent] = path
			expectedPaths[agent] = path
			versionOutputs[path] = agent + " v1.0.0"
		}

		d := NewDetector(0,
			WithLookPath(mockLookPath(pathMap)),
			WithEnvLookup(mockEnvLookup(map[string]string{})),
			WithCommandRunner(mockCommandRunner(versionOutputs, nil)),
		)

		runtimes := d.Scan()

		// Property: Each runtime has the correct agent_type and binary_path
		for _, rt := range runtimes {
			expectedPath, ok := expectedPaths[rt.AgentType]
			if !ok {
				t.Errorf("unexpected agent %q in results", rt.AgentType)
				continue
			}
			if rt.BinaryPath != expectedPath {
				t.Errorf("agent %q: expected binary_path %q, got %q", rt.AgentType, expectedPath, rt.BinaryPath)
			}
		}
	})
}

func TestProperty_ContextTimeoutResultsInUnavailable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random subset of agents
		presentAgents := genAgentSubset(t)
		if len(presentAgents) == 0 {
			return
		}

		pathMap := make(map[string]string)
		for _, agent := range presentAgents {
			pathMap[agent] = "/usr/local/bin/" + agent
		}

		// All version commands time out
		timeoutRunner := func(ctx context.Context, path string, args ...string) (string, error) {
			return "", context.DeadlineExceeded
		}

		d := NewDetector(0,
			WithLookPath(mockLookPath(pathMap)),
			WithEnvLookup(mockEnvLookup(map[string]string{})),
			WithCommandRunner(timeoutRunner),
		)

		runtimes := d.Scan()

		// Property: All runtimes should be "unavailable" when version times out
		if len(runtimes) != len(presentAgents) {
			t.Fatalf("expected %d runtimes, got %d", len(presentAgents), len(runtimes))
		}
		for _, rt := range runtimes {
			if rt.Status != "unavailable" {
				t.Errorf("agent %q: expected status 'unavailable' on timeout, got %q", rt.AgentType, rt.Status)
			}
			if rt.Version != "unknown" {
				t.Errorf("agent %q: expected version 'unknown' on timeout, got %q", rt.AgentType, rt.Version)
			}
		}
	})
}

func TestProperty_EnvOverrideOnlyAgentDetected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Pick a random agent that is ONLY available via env override (not in PATH)
		agentIdx := rapid.IntRange(0, len(SupportedAgents)-1).Draw(t, "agentIdx")
		agent := SupportedAgents[agentIdx]

		envBinary := "/custom/path/" + agent
		envKey := EnvOverrides[agent]

		// Nothing in PATH, only env override
		envMap := map[string]string{envKey: envBinary}
		versionOutputs := map[string]string{envBinary: agent + " v3.0.0"}

		d := NewDetector(0,
			WithLookPath(mockLookPath(map[string]string{})), // empty PATH
			WithEnvLookup(mockEnvLookup(envMap)),
			WithCommandRunner(mockCommandRunner(versionOutputs, nil)),
		)

		runtimes := d.Scan()

		// Property: Agent found via env override should appear in results
		if len(runtimes) != 1 {
			t.Fatalf("expected 1 runtime (from env override), got %d", len(runtimes))
		}

		rt := runtimes[0]
		if rt.AgentType != agent {
			t.Errorf("expected agent_type %q, got %q", agent, rt.AgentType)
		}
		if rt.BinaryPath != envBinary {
			t.Errorf("expected binary_path %q, got %q", envBinary, rt.BinaryPath)
		}
		if rt.Status != "available" {
			t.Errorf("expected status 'available', got %q", rt.Status)
		}
	})
}

func TestProperty_CommandErrorResultsInUnavailable(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a random subset of agents
		presentAgents := genAgentSubset(t)
		if len(presentAgents) == 0 {
			return
		}

		pathMap := make(map[string]string)
		for _, agent := range presentAgents {
			pathMap[agent] = "/usr/local/bin/" + agent
		}

		// All version commands return various errors
		errorRunner := func(ctx context.Context, path string, args ...string) (string, error) {
			return "", errors.New("exit status 1")
		}

		d := NewDetector(0,
			WithLookPath(mockLookPath(pathMap)),
			WithEnvLookup(mockEnvLookup(map[string]string{})),
			WithCommandRunner(errorRunner),
		)

		runtimes := d.Scan()

		// Property: All runtimes should be "unavailable" when command errors
		if len(runtimes) != len(presentAgents) {
			t.Fatalf("expected %d runtimes, got %d", len(presentAgents), len(runtimes))
		}
		for _, rt := range runtimes {
			if rt.Status != "unavailable" {
				t.Errorf("agent %q: expected status 'unavailable' on error, got %q", rt.AgentType, rt.Status)
			}
		}
	})
}

// itoa is a simple int-to-string helper to avoid importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
