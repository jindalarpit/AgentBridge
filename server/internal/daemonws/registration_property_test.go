package daemonws

import (
	"testing"

	"github.com/user/agentbridge/server/pkg/protocol"
	"pgregory.net/rapid"
)

// **Validates: Requirements 2.9**
//
// Property 5: Registration Validation
// For any DaemonRegister payload with at least one missing required field
// (daemon_id, user_id, or runtimes) or a user_id that does not correspond
// to an existing user, the server SHALL reject the registration with a
// non-empty error message and SHALL NOT create or modify any daemon or
// runtime records.

// genNonEmptyString generates a non-empty string for valid field values.
func genNonEmptyString() *rapid.Generator[string] {
	return rapid.StringMatching(`[a-zA-Z0-9_-]{1,64}`)
}

// genRuntimeInfo generates a valid RuntimeInfo struct.
func genRuntimeInfo() *rapid.Generator[protocol.RuntimeInfo] {
	return rapid.Custom(func(t *rapid.T) protocol.RuntimeInfo {
		return protocol.RuntimeInfo{
			AgentType:  rapid.SampledFrom([]string{"claude", "gemini", "kiro-cli", "codex", "copilot"}).Draw(t, "agent_type"),
			BinaryPath: rapid.StringMatching(`/[a-z/]{1,50}`).Draw(t, "binary_path"),
			Version:    rapid.StringMatching(`[0-9]+\.[0-9]+\.[0-9]+`).Draw(t, "version"),
			Status:     rapid.SampledFrom([]string{"available", "unavailable"}).Draw(t, "status"),
		}
	})
}

// genValidPayload generates a DaemonRegisterPayload with all required fields populated.
func genValidPayload() *rapid.Generator[protocol.DaemonRegisterPayload] {
	return rapid.Custom(func(t *rapid.T) protocol.DaemonRegisterPayload {
		numRuntimes := rapid.IntRange(0, 10).Draw(t, "num_runtimes")
		runtimes := make([]protocol.RuntimeInfo, numRuntimes)
		for i := range runtimes {
			runtimes[i] = genRuntimeInfo().Draw(t, "runtime")
		}
		return protocol.DaemonRegisterPayload{
			DaemonID: genNonEmptyString().Draw(t, "daemon_id"),
			UserID:   genNonEmptyString().Draw(t, "user_id"),
			Runtimes: runtimes,
		}
	})
}

// TestProperty_ValidRegistrationPasses verifies that for any valid
// DaemonRegisterPayload (non-empty daemon_id, non-empty user_id, non-nil
// runtimes), validation passes.
func TestProperty_ValidRegistrationPasses(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		payload := genValidPayload().Draw(t, "payload")

		err := ValidateRegistration(payload)
		if err != nil {
			t.Fatalf("expected valid payload to pass validation, got error: %v\npayload: %+v", err, payload)
		}
	})
}

// TestProperty_EmptyDaemonIDFails verifies that for any payload with an
// empty daemon_id, validation fails with a non-empty error.
func TestProperty_EmptyDaemonIDFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		payload := genValidPayload().Draw(t, "payload")
		payload.DaemonID = "" // Force empty daemon_id

		err := ValidateRegistration(payload)
		if err == nil {
			t.Fatal("expected validation to fail for empty daemon_id")
		}
		if err.Error() == "" {
			t.Fatal("expected non-empty error message")
		}
	})
}

// TestProperty_EmptyUserIDFails verifies that for any payload with an
// empty user_id, validation fails with a non-empty error.
func TestProperty_EmptyUserIDFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		payload := genValidPayload().Draw(t, "payload")
		payload.UserID = "" // Force empty user_id

		err := ValidateRegistration(payload)
		if err == nil {
			t.Fatal("expected validation to fail for empty user_id")
		}
		if err.Error() == "" {
			t.Fatal("expected non-empty error message")
		}
	})
}

// TestProperty_NilRuntimesFails verifies that for any payload with nil
// runtimes, validation fails with a non-empty error.
func TestProperty_NilRuntimesFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		payload := protocol.DaemonRegisterPayload{
			DaemonID: genNonEmptyString().Draw(t, "daemon_id"),
			UserID:   genNonEmptyString().Draw(t, "user_id"),
			Runtimes: nil, // Force nil runtimes
		}

		err := ValidateRegistration(payload)
		if err == nil {
			t.Fatal("expected validation to fail for nil runtimes")
		}
		if err.Error() == "" {
			t.Fatal("expected non-empty error message")
		}
	})
}

// TestProperty_AnyCombinationOfMissingFieldsFails verifies that for any
// combination of missing fields (at least one missing), validation always fails.
func TestProperty_AnyCombinationOfMissingFieldsFails(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Generate a base valid payload
		payload := genValidPayload().Draw(t, "payload")

		// Randomly decide which fields to invalidate (at least one must be invalid)
		emptyDaemonID := rapid.Bool().Draw(t, "empty_daemon_id")
		emptyUserID := rapid.Bool().Draw(t, "empty_user_id")
		nilRuntimes := rapid.Bool().Draw(t, "nil_runtimes")

		// Ensure at least one field is invalid
		if !emptyDaemonID && !emptyUserID && !nilRuntimes {
			// Force at least one to be invalid
			switch rapid.IntRange(0, 2).Draw(t, "force_invalid") {
			case 0:
				emptyDaemonID = true
			case 1:
				emptyUserID = true
			case 2:
				nilRuntimes = true
			}
		}

		if emptyDaemonID {
			payload.DaemonID = ""
		}
		if emptyUserID {
			payload.UserID = ""
		}
		if nilRuntimes {
			payload.Runtimes = nil
		}

		err := ValidateRegistration(payload)
		if err == nil {
			t.Fatalf("expected validation to fail when fields are missing: daemon_id_empty=%v, user_id_empty=%v, runtimes_nil=%v",
				emptyDaemonID, emptyUserID, nilRuntimes)
		}
		if err.Error() == "" {
			t.Fatal("expected non-empty error message")
		}
	})
}
