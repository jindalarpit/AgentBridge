package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"pgregory.net/rapid"
)

// Feature: daemon-browser-login, Property 6: Config File Round-Trip
// For any valid Config object (token ≤ 2048 chars, server_url ≤ 2048 chars,
// user_email ≤ 254 chars), writing the config to disk and reading it back
// SHALL produce a Config with identical field values.
//
// **Validates: Requirements 4.1, 4.4**

func genValidToken(t *rapid.T) string {
	// Generate a token string up to 2048 characters using printable ASCII.
	maxLen := 2048
	n := rapid.IntRange(0, maxLen).Draw(t, "token-len")
	if n == 0 {
		return ""
	}
	// Use printable ASCII characters (32-126) to avoid JSON encoding issues.
	runes := make([]rune, n)
	for i := range runes {
		runes[i] = rune(rapid.IntRange(32, 126).Draw(t, "token-char"))
	}
	return string(runes)
}

func genValidServerURL(t *rapid.T) string {
	maxLen := 2048
	n := rapid.IntRange(0, maxLen).Draw(t, "url-len")
	if n == 0 {
		return ""
	}
	runes := make([]rune, n)
	for i := range runes {
		runes[i] = rune(rapid.IntRange(32, 126).Draw(t, "url-char"))
	}
	return string(runes)
}

func genValidUserEmail(t *rapid.T) string {
	maxLen := 254
	n := rapid.IntRange(0, maxLen).Draw(t, "email-len")
	if n == 0 {
		return ""
	}
	runes := make([]rune, n)
	for i := range runes {
		runes[i] = rune(rapid.IntRange(32, 126).Draw(t, "email-char"))
	}
	return string(runes)
}

func TestProperty_ConfigFileRoundTrip(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		token := genValidToken(rt)
		serverURL := genValidServerURL(rt)
		userEmail := genValidUserEmail(rt)

		cfg := Config{
			Token:     token,
			ServerURL: serverURL,
			UserEmail: userEmail,
		}

		// Write to a temp directory.
		dir, err := os.MkdirTemp("", "config-roundtrip-*")
		if err != nil {
			t.Fatalf("MkdirTemp() error: %v", err)
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "config.json")

		if err := Save(cfg, path); err != nil {
			t.Fatalf("Save() error: %v", err)
		}

		// Read it back.
		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		// Property: field values must be identical after round-trip.
		if loaded.Token != cfg.Token {
			rt.Errorf("Token mismatch: got %q, want %q", loaded.Token, cfg.Token)
		}
		if loaded.ServerURL != cfg.ServerURL {
			rt.Errorf("ServerURL mismatch: got %q, want %q", loaded.ServerURL, cfg.ServerURL)
		}
		if loaded.UserEmail != cfg.UserEmail {
			rt.Errorf("UserEmail mismatch: got %q, want %q", loaded.UserEmail, cfg.UserEmail)
		}
	})
}

// Feature: daemon-browser-login, Property 7: Config Write Preserves Unrelated Fields
// For any existing config JSON containing additional fields beyond token, server_url,
// and user_email, writing a new token/server_url/user_email SHALL preserve all other
// fields with their original values unchanged.
//
// **Validates: Requirements 4.2**

// genExtraFieldKey generates a JSON key that is not one of the known config fields.
func genExtraFieldKey(t *rapid.T, label string) string {
	// Generate a key that doesn't collide with known fields.
	for {
		n := rapid.IntRange(1, 30).Draw(t, label+"-key-len")
		runes := make([]rune, n)
		for i := range runes {
			// Use lowercase letters and underscores for valid JSON keys.
			chars := "abcdefghijklmnopqrstuvwxyz_0123456789"
			idx := rapid.IntRange(0, len(chars)-1).Draw(t, label+"-key-char")
			runes[i] = rune(chars[idx])
		}
		key := string(runes)
		if key != "token" && key != "server_url" && key != "user_email" {
			return key
		}
	}
}

// genJSONValue generates a random JSON-serializable value.
func genJSONValue(t *rapid.T, label string) json.RawMessage {
	// Pick a random JSON value type.
	valueType := rapid.IntRange(0, 3).Draw(t, label+"-type")
	switch valueType {
	case 0: // string
		n := rapid.IntRange(0, 50).Draw(t, label+"-str-len")
		runes := make([]rune, n)
		for i := range runes {
			runes[i] = rune(rapid.IntRange(32, 126).Draw(t, label+"-str-char"))
		}
		b, _ := json.Marshal(string(runes))
		return b
	case 1: // number
		num := rapid.IntRange(-1000, 1000).Draw(t, label+"-num")
		b, _ := json.Marshal(num)
		return b
	case 2: // boolean
		val := rapid.Bool().Draw(t, label+"-bool")
		b, _ := json.Marshal(val)
		return b
	default: // null
		return json.RawMessage("null")
	}
}

func TestProperty_ConfigWritePreservesUnrelatedFields(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate 1-5 extra fields with random keys and values.
		numExtra := rapid.IntRange(1, 5).Draw(rt, "num-extra")
		extraFields := make(map[string]json.RawMessage)
		for i := 0; i < numExtra; i++ {
			key := genExtraFieldKey(rt, "extra"+string(rune('0'+i)))
			val := genJSONValue(rt, "extra-val"+string(rune('0'+i)))
			extraFields[key] = val
		}

		// Build an initial JSON object with known fields + extra fields.
		initial := make(map[string]json.RawMessage)
		initial["token"], _ = json.Marshal("old_token")
		initial["server_url"], _ = json.Marshal("ws://old.server")
		initial["user_email"], _ = json.Marshal("old@example.com")
		for k, v := range extraFields {
			initial[k] = v
		}

		// Write the initial JSON to disk.
		dir, err := os.MkdirTemp("", "config-preserve-*")
		if err != nil {
			t.Fatalf("MkdirTemp() error: %v", err)
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "config.json")
		initialData, _ := json.Marshal(initial)
		if err := os.WriteFile(path, initialData, 0o644); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}

		// Load, modify known fields, save.
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error: %v", err)
		}

		// Generate new values for known fields.
		cfg.Token = genValidToken(rt)
		cfg.ServerURL = genValidServerURL(rt)
		cfg.UserEmail = genValidUserEmail(rt)

		if err := Save(cfg, path); err != nil {
			t.Fatalf("Save() error: %v", err)
		}

		// Read back the raw JSON and verify extra fields are preserved.
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error: %v", err)
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("Unmarshal() error: %v", err)
		}

		// Property: every extra field must be preserved with its original value.
		for key, expectedVal := range extraFields {
			actualVal, ok := raw[key]
			if !ok {
				rt.Errorf("extra field %q was lost after Save", key)
				continue
			}
			// Compare JSON values by normalizing (compact form).
			var expectedNorm, actualNorm interface{}
			json.Unmarshal(expectedVal, &expectedNorm)
			json.Unmarshal(actualVal, &actualNorm)

			expectedBytes, _ := json.Marshal(expectedNorm)
			actualBytes, _ := json.Marshal(actualNorm)

			if string(expectedBytes) != string(actualBytes) {
				rt.Errorf("extra field %q changed: got %s, want %s", key, actualBytes, expectedBytes)
			}
		}
	})
}

// Feature: daemon-browser-login, Property 8: Invalid Token Sources Treated as Unavailable
// For any string composed entirely of whitespace characters (including empty string),
// and for any byte sequence that is not valid JSON, the auth resolution SHALL treat
// the token as unavailable.
//
// **Validates: Requirements 4.3, 4.5**

// genWhitespaceString generates a string composed entirely of whitespace characters
// (including empty string).
func genWhitespaceString(t *rapid.T) string {
	whitespaceChars := []rune{' ', '\t', '\n', '\r', '\v', '\f'}
	n := rapid.IntRange(0, 50).Draw(t, "ws-len")
	runes := make([]rune, n)
	for i := range runes {
		idx := rapid.IntRange(0, len(whitespaceChars)-1).Draw(t, "ws-char")
		runes[i] = whitespaceChars[idx]
	}
	return string(runes)
}

// genInvalidJSON generates a byte sequence that is NOT valid JSON.
func genInvalidJSON(t *rapid.T) []byte {
	// Strategies for invalid JSON.
	strategy := rapid.IntRange(0, 4).Draw(t, "invalid-json-strategy")
	switch strategy {
	case 0: // Truncated object
		return []byte(`{"key": "value"`)
	case 1: // Random bytes
		n := rapid.IntRange(1, 100).Draw(t, "random-bytes-len")
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(rapid.IntRange(0, 255).Draw(t, "random-byte"))
		}
		// Ensure it's not accidentally valid JSON.
		if json.Valid(b) {
			return []byte("{{{invalid")
		}
		return b
	case 2: // Trailing garbage
		return []byte(`{"token":"abc"} garbage`)
	case 3: // Unquoted key
		return []byte(`{token: "abc"}`)
	default: // Just plain text
		n := rapid.IntRange(1, 50).Draw(t, "text-len")
		runes := make([]rune, n)
		for i := range runes {
			runes[i] = rune(rapid.IntRange(65, 90).Draw(t, "text-char"))
		}
		s := string(runes)
		if json.Valid([]byte(s)) {
			return []byte("not{valid}json")
		}
		return []byte(s)
	}
}

func TestProperty_WhitespaceTokenTreatedAsUnavailable(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a whitespace-only string (including empty).
		wsToken := genWhitespaceString(rt)

		// Property: IsTokenEmpty must return true for any whitespace-only string.
		if !IsTokenEmpty(wsToken) {
			rt.Errorf("IsTokenEmpty(%q) = false, want true (whitespace-only tokens are unavailable)", wsToken)
		}

		// Also verify via strings.TrimSpace consistency.
		if strings.TrimSpace(wsToken) != "" {
			rt.Errorf("strings.TrimSpace(%q) is non-empty but IsTokenEmpty returned true", wsToken)
		}
	})
}

func TestProperty_NonWhitespaceTokenTreatedAsAvailable(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a string that contains at least one non-whitespace character.
		n := rapid.IntRange(1, 100).Draw(rt, "token-len")
		runes := make([]rune, n)
		for i := range runes {
			runes[i] = rune(rapid.IntRange(32, 126).Draw(rt, "token-char"))
		}
		token := string(runes)

		// Ensure at least one non-whitespace character.
		hasNonWS := false
		for _, r := range token {
			if !unicode.IsSpace(r) {
				hasNonWS = true
				break
			}
		}
		if !hasNonWS {
			// Force a non-whitespace character.
			token = token + "x"
		}

		// Property: IsTokenEmpty must return false for any string with non-whitespace.
		if IsTokenEmpty(token) {
			rt.Errorf("IsTokenEmpty(%q) = true, want false (token has non-whitespace content)", token)
		}
	})
}

func TestProperty_InvalidJSONTreatedAsNoToken(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate invalid JSON content.
		invalidData := genInvalidJSON(rt)

		// Write invalid JSON to a config file.
		dir, err := os.MkdirTemp("", "config-invalid-json-*")
		if err != nil {
			t.Fatalf("MkdirTemp() error: %v", err)
		}
		defer os.RemoveAll(dir)
		path := filepath.Join(dir, "config.json")
		if err := os.WriteFile(path, invalidData, 0o644); err != nil {
			t.Fatalf("WriteFile() error: %v", err)
		}

		// Property: Load must return a zero-value Config (token unavailable).
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error: %v (should not error on invalid JSON)", err)
		}

		if cfg.Token != "" {
			rt.Errorf("Load() on invalid JSON returned Token=%q, want empty", cfg.Token)
		}
		if cfg.ServerURL != "" {
			rt.Errorf("Load() on invalid JSON returned ServerURL=%q, want empty", cfg.ServerURL)
		}
		if cfg.UserEmail != "" {
			rt.Errorf("Load() on invalid JSON returned UserEmail=%q, want empty", cfg.UserEmail)
		}

		// The token from this config should be treated as unavailable.
		if !IsTokenEmpty(cfg.Token) {
			rt.Errorf("IsTokenEmpty(%q) = false after loading invalid JSON config", cfg.Token)
		}
	})
}

// Feature: daemon-browser-login, Property 9: Auth Resolution Precedence
// For any non-whitespace value V in `AGENTBRIDGE_TOKEN` and any value F in the
// config file's token field, the resolved token SHALL equal `strings.TrimSpace(V)`
// regardless of F's value.
//
// **Validates: Requirements 5.1, 5.2**

// genNonWhitespaceString generates a string that contains at least one non-whitespace character.
func genNonWhitespaceString(t *rapid.T) string {
	// Generate a string with at least one non-whitespace character.
	// We build a string with optional leading/trailing whitespace around a non-whitespace core.
	leadingWS := genWhitespaceString(t)
	trailingWS := genWhitespaceString(t)

	// Generate a non-empty core with at least one non-whitespace char.
	coreLen := rapid.IntRange(1, 100).Draw(t, "core-len")
	runes := make([]rune, coreLen)
	for i := range runes {
		// Use printable ASCII (33-126) to ensure non-whitespace.
		runes[i] = rune(rapid.IntRange(33, 126).Draw(t, "core-char"))
	}
	core := string(runes)

	return leadingWS + core + trailingWS
}

func TestProperty_AuthResolutionPrecedence_EnvVarWins(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a non-whitespace env var value.
		envValue := genNonWhitespaceString(rt)

		// Generate any config file token value (could be empty, whitespace, or valid).
		configTokenStrategy := rapid.IntRange(0, 2).Draw(rt, "config-strategy")
		var configToken string
		switch configTokenStrategy {
		case 0:
			configToken = "" // empty
		case 1:
			configToken = genWhitespaceString(rt) // whitespace-only
		default:
			configToken = genNonWhitespaceString(rt) // valid non-whitespace token
		}

		// Write a config file with the generated token.
		dir, err := os.MkdirTemp("", "config-precedence-*")
		if err != nil {
			t.Fatalf("MkdirTemp() error: %v", err)
		}
		defer os.RemoveAll(dir)
		configPath := filepath.Join(dir, "config.json")

		cfg := Config{Token: configToken}
		if err := Save(cfg, configPath); err != nil {
			t.Fatalf("Save() error: %v", err)
		}

		// Set the env var.
		os.Setenv("AGENTBRIDGE_TOKEN", envValue)
		defer os.Unsetenv("AGENTBRIDGE_TOKEN")

		// Resolve the token.
		resolved, err := ResolveToken(configPath)
		if err != nil {
			rt.Fatalf("ResolveToken() error: %v", err)
		}

		// Property: resolved token SHALL equal strings.TrimSpace(envValue).
		expected := strings.TrimSpace(envValue)
		if resolved != expected {
			rt.Errorf("ResolveToken() = %q, want %q (env var should take precedence over config token %q)",
				resolved, expected, configToken)
		}
	})
}

func TestProperty_AuthResolutionPrecedence_FallbackToConfigFile(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a whitespace-only env var value (or empty).
		envValue := genWhitespaceString(rt)

		// Generate a non-whitespace config file token.
		configToken := genNonWhitespaceString(rt)

		// Write a config file with the generated token.
		dir, err := os.MkdirTemp("", "config-fallback-*")
		if err != nil {
			t.Fatalf("MkdirTemp() error: %v", err)
		}
		defer os.RemoveAll(dir)
		configPath := filepath.Join(dir, "config.json")

		cfg := Config{Token: configToken}
		if err := Save(cfg, configPath); err != nil {
			t.Fatalf("Save() error: %v", err)
		}

		// Set the env var to whitespace-only.
		os.Setenv("AGENTBRIDGE_TOKEN", envValue)
		defer os.Unsetenv("AGENTBRIDGE_TOKEN")

		// Resolve the token.
		resolved, err := ResolveToken(configPath)
		if err != nil {
			rt.Fatalf("ResolveToken() error: %v", err)
		}

		// Property: when env var is whitespace-only, config file token is used (trimmed).
		expected := strings.TrimSpace(configToken)
		if resolved != expected {
			rt.Errorf("ResolveToken() = %q, want %q (should fall back to config file token when env var is %q)",
				resolved, expected, envValue)
		}
	})
}

func TestProperty_MissingFileTreatedAsNoToken(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		// Generate a random non-existent path.
		dir, err := os.MkdirTemp("", "config-missing-*")
		if err != nil {
			t.Fatalf("MkdirTemp() error: %v", err)
		}
		defer os.RemoveAll(dir)
		filename := "nonexistent_" + string(rune(rapid.IntRange(65, 90).Draw(rt, "char"))) + ".json"
		path := filepath.Join(dir, filename)

		// Property: Load on missing file returns zero-value Config.
		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("Load() error: %v (should not error on missing file)", err)
		}

		if cfg.Token != "" {
			rt.Errorf("Load() on missing file returned Token=%q, want empty", cfg.Token)
		}
		if !IsTokenEmpty(cfg.Token) {
			rt.Errorf("IsTokenEmpty(%q) = false for missing file config", cfg.Token)
		}
	})
}
