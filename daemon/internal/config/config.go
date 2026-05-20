// Package config manages reading and writing the daemon configuration file
// at ~/.agentbridge/config.json. It supports atomic writes, permission enforcement,
// and preservation of unknown JSON fields across read/write cycles.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config represents the persisted daemon configuration.
type Config struct {
	Token     string `json:"token,omitempty"`
	ServerURL string `json:"server_url,omitempty"`
	UserEmail string `json:"user_email,omitempty"`
	// Extra holds any additional fields from the JSON file that should be preserved.
	Extra map[string]json.RawMessage `json:"-"`
}

// knownFields is the set of JSON keys managed by Config struct fields.
var knownFields = map[string]bool{
	"token":      true,
	"server_url": true,
	"user_email": true,
}

// DefaultPath returns the default config file path: ~/.agentbridge/config.json.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine home directory: %w", err)
	}
	return filepath.Join(home, ".agentbridge", "config.json"), nil
}

// Load reads and parses the config file at the given path.
// Returns a zero-value Config (not an error) if the file doesn't exist or
// contains invalid JSON, per requirement 4.5.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// File missing or unreadable — treat as empty config.
		return Config{}, nil
	}

	var cfg Config

	// First unmarshal into the struct to get known fields.
	if err := json.Unmarshal(data, &cfg); err != nil {
		// Invalid JSON — treat as empty config.
		return Config{}, nil
	}

	// Now unmarshal into a raw map to capture extra fields.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		// Should not happen if the first unmarshal succeeded, but be safe.
		return cfg, nil
	}

	// Collect fields not in the known set.
	extra := make(map[string]json.RawMessage)
	for k, v := range raw {
		if !knownFields[k] {
			extra[k] = v
		}
	}
	if len(extra) > 0 {
		cfg.Extra = extra
	}

	return cfg, nil
}

// Save writes the config atomically to the given path using a temporary file
// and rename. The file is written with 0600 permissions and the parent directory
// is created with 0700 permissions if it doesn't exist.
func Save(cfg Config, path string) error {
	dir := filepath.Dir(path)

	// Create directory with 0700 if it doesn't exist.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory %s: %w", dir, err)
	}

	// Build the JSON object preserving extra fields.
	data, err := marshalConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to a temp file in the same directory for atomic rename.
	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	// Clean up temp file on any error path.
	defer func() {
		if tmpPath != "" {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Set permissions before rename so the file is never world-readable.
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("failed to set file permissions: %w", err)
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename config file: %w", err)
	}

	// Rename succeeded — prevent deferred cleanup.
	tmpPath = ""
	return nil
}

// ResolveToken returns the effective token using the precedence rules:
// 1. AGENTBRIDGE_TOKEN env var (trimmed, if non-whitespace)
// 2. Config file token field (if present and non-whitespace)
// Returns ("", nil) if no token is available (caller should exit with guidance).
func ResolveToken(configPath string) (string, error) {
	// Check env var first — it takes precedence for CI/automation.
	if envToken := os.Getenv("AGENTBRIDGE_TOKEN"); !IsTokenEmpty(envToken) {
		return strings.TrimSpace(envToken), nil
	}

	// Fall back to config file token.
	cfg, err := Load(configPath)
	if err != nil {
		return "", err
	}

	if !IsTokenEmpty(cfg.Token) {
		return strings.TrimSpace(cfg.Token), nil
	}

	return "", nil
}

// IsTokenEmpty returns true if the token is empty or contains only whitespace.
func IsTokenEmpty(token string) bool {
	return strings.TrimSpace(token) == ""
}

// marshalConfig produces a JSON byte slice that includes both the known Config
// fields and any extra fields, preserving round-trip fidelity.
func marshalConfig(cfg Config) ([]byte, error) {
	// Start with extra fields as the base.
	merged := make(map[string]json.RawMessage)
	for k, v := range cfg.Extra {
		merged[k] = v
	}

	// Marshal and set known fields (only if non-empty, matching omitempty behavior).
	if cfg.Token != "" {
		b, _ := json.Marshal(cfg.Token)
		merged["token"] = b
	}
	if cfg.ServerURL != "" {
		b, _ := json.Marshal(cfg.ServerURL)
		merged["server_url"] = b
	}
	if cfg.UserEmail != "" {
		b, _ := json.Marshal(cfg.UserEmail)
		merged["user_email"] = b
	}

	return json.MarshalIndent(merged, "", "  ")
}
