package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestLogout_ValidToken_CallsDeleteAndClearsToken(t *testing.T) {
	// Mock server that returns 204 for DELETE /api/daemon-tokens/current.
	var deleteCalled atomic.Int32
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/api/daemon-tokens/current" {
			t.Errorf("path = %s, want /api/daemon-tokens/current", r.URL.Path)
		}
		receivedAuth = r.Header.Get("Authorization")
		deleteCalled.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// Create a temp config file with a token and server_url pointing to mock server.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := map[string]string{
		"token":      "ab_test1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd",
		"server_url": server.URL,
		"user_email": "user@example.com",
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Run logout.
	err = runLogout(configPath)
	if err != nil {
		t.Fatalf("runLogout() error: %v", err)
	}

	// Verify DELETE was called.
	if deleteCalled.Load() != 1 {
		t.Errorf("DELETE called %d times, want 1", deleteCalled.Load())
	}

	// Verify the Authorization header was correct.
	expectedAuth := "Bearer ab_test1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd"
	if receivedAuth != expectedAuth {
		t.Errorf("Authorization = %q, want %q", receivedAuth, expectedAuth)
	}

	// Verify token is cleared from config file.
	savedData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config after logout: %v", err)
	}
	var savedCfg map[string]interface{}
	if err := json.Unmarshal(savedData, &savedCfg); err != nil {
		t.Fatalf("failed to parse config after logout: %v", err)
	}
	if token, ok := savedCfg["token"]; ok && token != "" {
		t.Errorf("token should be cleared after logout, got %q", token)
	}
}

func TestLogout_ServerFailure_StillClearsToken(t *testing.T) {
	// Mock server that returns 500 to simulate server failure.
	var deleteCalled atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deleteCalled.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Create a temp config file with a token.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := map[string]string{
		"token":      "ab_aabbccdd1234567890abcdef1234567890abcdef1234567890abcdef12345678",
		"server_url": server.URL,
		"user_email": "user@example.com",
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Run logout — should succeed (not return error) even though server failed.
	err = runLogout(configPath)
	if err != nil {
		t.Fatalf("runLogout() should not return error on server failure, got: %v", err)
	}

	// Verify DELETE was attempted.
	if deleteCalled.Load() != 1 {
		t.Errorf("DELETE called %d times, want 1", deleteCalled.Load())
	}

	// Verify token is still cleared from config file despite server failure.
	savedData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config after logout: %v", err)
	}
	var savedCfg map[string]interface{}
	if err := json.Unmarshal(savedData, &savedCfg); err != nil {
		t.Fatalf("failed to parse config after logout: %v", err)
	}
	if token, ok := savedCfg["token"]; ok && token != "" {
		t.Errorf("token should be cleared after logout even on server failure, got %q", token)
	}
}

func TestLogout_NoToken_PrintsMessageAndExits(t *testing.T) {
	// Mock server — should NOT be called when there's no token.
	var deleteCalled atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deleteCalled.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// Create a temp config file with no token (empty string).
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := map[string]string{
		"token":      "",
		"server_url": server.URL,
		"user_email": "user@example.com",
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Run logout — should succeed (return nil).
	err = runLogout(configPath)
	if err != nil {
		t.Fatalf("runLogout() should return nil when no token exists, got: %v", err)
	}

	// Verify no HTTP calls were made.
	if deleteCalled.Load() != 0 {
		t.Errorf("DELETE should not be called when no token exists, but was called %d times", deleteCalled.Load())
	}
}

func TestLogout_NoToken_MissingConfigFile(t *testing.T) {
	// When config file doesn't exist, logout should succeed with "no active session" message.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nonexistent", "config.json")

	err := runLogout(configPath)
	if err != nil {
		t.Fatalf("runLogout() should return nil when config file is missing, got: %v", err)
	}
}
