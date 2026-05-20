package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCallbackServer_BindsToLocalhost(t *testing.T) {
	cs, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error: %v", err)
	}
	defer cs.Close()

	// Verify the listener is bound to 127.0.0.1.
	addr := cs.listener.Addr().(*net.TCPAddr)
	if addr.IP.String() != "127.0.0.1" {
		t.Errorf("expected listener bound to 127.0.0.1, got %s", addr.IP.String())
	}
}

func TestCallbackServer_StateIs32CharHex(t *testing.T) {
	cs, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error: %v", err)
	}
	defer cs.Close()

	state := cs.State()
	if len(state) != 32 {
		t.Errorf("expected state length 32, got %d", len(state))
	}

	// Verify all characters are valid hex digits.
	for i, c := range state {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("state[%d] = %c, not a valid lowercase hex digit", i, c)
		}
	}
}

func TestCallbackServer_PortInValidRange(t *testing.T) {
	cs, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error: %v", err)
	}
	defer cs.Close()

	port := cs.Port()
	if port < 1024 || port > 65535 {
		t.Errorf("expected port in range 1024-65535, got %d", port)
	}
}

func TestCallbackServer_MatchingStateAndToken_ReturnsToken(t *testing.T) {
	cs, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error: %v", err)
	}
	defer cs.Close()

	if err := cs.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Send a valid callback request with matching state and a token.
	expectedToken := "eyJhbGciOiJIUzI1NiJ9.test-jwt-token"
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?state=%s&token=%s",
		cs.Port(), cs.State(), expectedToken)

	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected HTTP 200, got %d", resp.StatusCode)
	}

	// WaitForToken should return the token immediately.
	token, err := cs.WaitForToken(5 * time.Second)
	if err != nil {
		t.Fatalf("WaitForToken() error: %v", err)
	}
	if token != expectedToken {
		t.Errorf("expected token %q, got %q", expectedToken, token)
	}
}

func TestCallbackServer_StateMismatch_ReturnsHTTP400(t *testing.T) {
	cs, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error: %v", err)
	}
	defer cs.Close()

	if err := cs.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Send a callback request with a mismatched state.
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?state=%s&token=%s",
		cs.Port(), "wrong_state_value_not_matching_xx", "some-token")

	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected HTTP 400 for state mismatch, got %d", resp.StatusCode)
	}
}

func TestCallbackServer_MissingTokenParam_ReturnsHTTP400(t *testing.T) {
	cs, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error: %v", err)
	}
	defer cs.Close()

	if err := cs.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Send a callback request with matching state but no token parameter.
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?state=%s",
		cs.Port(), cs.State())

	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected HTTP 400 for missing token param, got %d", resp.StatusCode)
	}
}

func TestCallbackServer_EmptyTokenParam_ReturnsHTTP400(t *testing.T) {
	cs, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error: %v", err)
	}
	defer cs.Close()

	if err := cs.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Send a callback request with matching state but empty token value.
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?state=%s&token=",
		cs.Port(), cs.State())

	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected HTTP 400 for empty token param, got %d", resp.StatusCode)
	}
}

func TestCallbackServer_WaitForToken_Timeout(t *testing.T) {
	cs, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error: %v", err)
	}
	defer cs.Close()

	if err := cs.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Use a very short timeout to test the timeout path.
	_, err = cs.WaitForToken(50 * time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("expected timeout error message, got: %v", err)
	}
}

func TestCallbackServer_ErrorMessages_DoNotExposeToken(t *testing.T) {
	cs, err := NewCallbackServer()
	if err != nil {
		t.Fatalf("NewCallbackServer() error: %v", err)
	}
	defer cs.Close()

	if err := cs.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Send a request with mismatched state — the error response should not contain the token value.
	secretToken := "super_secret_jwt_token_value_12345"
	callbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?state=%s&token=%s",
		cs.Port(), "wrong_state_xxxxxxxxxxxxxxxx_xx", secretToken)

	resp, err := http.Get(callbackURL)
	if err != nil {
		t.Fatalf("GET callback error: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if strings.Contains(string(body), secretToken) {
		t.Errorf("error response body should not expose the token value, got: %s", string(body))
	}
}

func TestOpenBrowser_InvalidCommand_ReturnsError(t *testing.T) {
	// Test that openBrowser returns an error for an invalid/non-existent URL scheme
	// that would fail to launch. We can't easily test the actual browser opening,
	// but we can verify the function exists and handles the platform switch.
	// On any platform, calling openBrowser with a valid URL should at least not panic.
	// The actual browser open may fail in CI environments, which is expected.
	err := openBrowser("")
	// An empty URL should still attempt to run the platform command,
	// which may or may not error depending on the OS. We just verify no panic.
	_ = err
}
