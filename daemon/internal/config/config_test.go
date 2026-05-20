package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPath(t *testing.T) {
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error: %v", err)
	}

	home, _ := os.UserHomeDir()
	expected := filepath.Join(home, ".agentbridge", "config.json")
	if path != expected {
		t.Errorf("DefaultPath() = %q, want %q", path, expected)
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Token != "" || cfg.ServerURL != "" || cfg.UserEmail != "" {
		t.Errorf("Load() on missing file should return zero-value Config, got %+v", cfg)
	}
	if cfg.Extra != nil {
		t.Errorf("Load() on missing file should have nil Extra, got %v", cfg.Extra)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte("not valid json {{{"), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Token != "" || cfg.ServerURL != "" || cfg.UserEmail != "" {
		t.Errorf("Load() on invalid JSON should return zero-value Config, got %+v", cfg)
	}
}

func TestLoadValidConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"token":"ab_test123","server_url":"ws://localhost:8080","user_email":"test@example.com"}`
	os.WriteFile(path, []byte(data), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Token != "ab_test123" {
		t.Errorf("Token = %q, want %q", cfg.Token, "ab_test123")
	}
	if cfg.ServerURL != "ws://localhost:8080" {
		t.Errorf("ServerURL = %q, want %q", cfg.ServerURL, "ws://localhost:8080")
	}
	if cfg.UserEmail != "test@example.com" {
		t.Errorf("UserEmail = %q, want %q", cfg.UserEmail, "test@example.com")
	}
}

func TestLoadPreservesExtraFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data := `{"token":"abc","custom_field":"hello","nested":{"a":1}}`
	os.WriteFile(path, []byte(data), 0o644)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Token != "abc" {
		t.Errorf("Token = %q, want %q", cfg.Token, "abc")
	}
	if cfg.Extra == nil {
		t.Fatal("Extra should not be nil")
	}
	if _, ok := cfg.Extra["custom_field"]; !ok {
		t.Error("Extra should contain 'custom_field'")
	}
	if _, ok := cfg.Extra["nested"]; !ok {
		t.Error("Extra should contain 'nested'")
	}
	// Known fields should NOT be in Extra.
	if _, ok := cfg.Extra["token"]; ok {
		t.Error("Extra should not contain 'token'")
	}
}

func TestSaveCreatesDirectoryAndFile(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "newdir", "subdir")
	path := filepath.Join(subdir, "config.json")

	cfg := Config{
		Token:     "ab_mytoken",
		ServerURL: "ws://example.com",
		UserEmail: "user@test.com",
	}

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Verify directory was created.
	info, err := os.Stat(subdir)
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
	// Check directory permissions (0700).
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory permissions = %o, want 0700", perm)
	}

	// Verify file exists and has correct permissions.
	finfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if perm := finfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestSaveFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg := Config{Token: "test"}
	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}
}

func TestSaveAtomicOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write initial config.
	cfg1 := Config{Token: "first"}
	if err := Save(cfg1, path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Overwrite with new config.
	cfg2 := Config{Token: "second", UserEmail: "new@test.com"}
	if err := Save(cfg2, path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Read back and verify.
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if loaded.Token != "second" {
		t.Errorf("Token = %q, want %q", loaded.Token, "second")
	}
	if loaded.UserEmail != "new@test.com" {
		t.Errorf("UserEmail = %q, want %q", loaded.UserEmail, "new@test.com")
	}
}

func TestSavePreservesExtraFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write a file with extra fields manually.
	initial := `{"token":"old","custom":"preserved","number":42}`
	os.WriteFile(path, []byte(initial), 0o644)

	// Load, modify known fields, save.
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	cfg.Token = "new_token"
	cfg.ServerURL = "ws://new.server"

	if err := Save(cfg, path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	// Read back raw JSON and verify extra fields preserved.
	data, _ := os.ReadFile(path)
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)

	if string(raw["custom"]) != `"preserved"` {
		t.Errorf("custom field = %s, want %q", raw["custom"], "preserved")
	}
	if string(raw["number"]) != "42" {
		t.Errorf("number field = %s, want 42", raw["number"])
	}
	// Verify known fields updated.
	if string(raw["token"]) != `"new_token"` {
		t.Errorf("token = %s, want %q", raw["token"], "new_token")
	}
	if string(raw["server_url"]) != `"ws://new.server"` {
		t.Errorf("server_url = %s, want %q", raw["server_url"], "ws://new.server")
	}
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	original := Config{
		Token:     "ab_roundtrip",
		ServerURL: "ws://localhost:8080/ws",
		UserEmail: "round@trip.com",
	}

	if err := Save(original, path); err != nil {
		t.Fatalf("Save() error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if loaded.Token != original.Token {
		t.Errorf("Token = %q, want %q", loaded.Token, original.Token)
	}
	if loaded.ServerURL != original.ServerURL {
		t.Errorf("ServerURL = %q, want %q", loaded.ServerURL, original.ServerURL)
	}
	if loaded.UserEmail != original.UserEmail {
		t.Errorf("UserEmail = %q, want %q", loaded.UserEmail, original.UserEmail)
	}
}

func TestIsTokenEmpty(t *testing.T) {
	tests := []struct {
		token string
		want  bool
	}{
		{"", true},
		{" ", true},
		{"\t", true},
		{"\n", true},
		{"  \t\n  ", true},
		{"abc", false},
		{" abc ", false},
		{"ab_token123", false},
		{" \t token \n ", false},
	}

	for _, tt := range tests {
		got := IsTokenEmpty(tt.token)
		if got != tt.want {
			t.Errorf("IsTokenEmpty(%q) = %v, want %v", tt.token, got, tt.want)
		}
	}
}
