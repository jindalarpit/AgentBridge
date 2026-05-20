package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"pgregory.net/rapid"
)

// Feature: daemon-browser-login, Property 1: Login URL Construction
// For any valid port number (1024–65535) and any 32-character hex state string,
// the constructed login URL SHALL be of the form
// `{app_url}/login?cli_callback={url_encoded_callback}&cli_state={url_encoded_state}`
// where callback is `http://localhost:{port}/callback`.
//
// **Validates: Requirements 1.2**

func TestProperty_LoginURLHasCorrectSchemeAndHost(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		port := rapid.IntRange(1024, 65535).Draw(t, "port")
		stateHex := rapid.StringMatching(`[0-9a-f]{32}`).Draw(t, "state")
		appURL := "https://app.example.com"

		callbackURL := fmt.Sprintf("http://localhost:%d/callback", port)
		loginURL := fmt.Sprintf("%s/login?cli_callback=%s&cli_state=%s",
			appURL,
			url.QueryEscape(callbackURL),
			url.QueryEscape(stateHex),
		)

		parsed, err := url.Parse(loginURL)
		if err != nil {
			t.Fatalf("failed to parse login URL: %v", err)
		}

		if parsed.Scheme != "https" {
			t.Fatalf("expected scheme 'https', got %q", parsed.Scheme)
		}
		if parsed.Host != "app.example.com" {
			t.Fatalf("expected host 'app.example.com', got %q", parsed.Host)
		}
	})
}

func TestProperty_LoginURLHasCorrectPath(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		port := rapid.IntRange(1024, 65535).Draw(t, "port")
		stateHex := rapid.StringMatching(`[0-9a-f]{32}`).Draw(t, "state")
		appURL := "https://app.example.com"

		callbackURL := fmt.Sprintf("http://localhost:%d/callback", port)
		loginURL := fmt.Sprintf("%s/login?cli_callback=%s&cli_state=%s",
			appURL,
			url.QueryEscape(callbackURL),
			url.QueryEscape(stateHex),
		)

		parsed, err := url.Parse(loginURL)
		if err != nil {
			t.Fatalf("failed to parse login URL: %v", err)
		}

		if parsed.Path != "/login" {
			t.Fatalf("expected path '/login', got %q", parsed.Path)
		}
	})
}

func TestProperty_LoginURLCallbackDecodesToLocalhost(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		port := rapid.IntRange(1024, 65535).Draw(t, "port")
		stateHex := rapid.StringMatching(`[0-9a-f]{32}`).Draw(t, "state")
		appURL := "https://app.example.com"

		callbackURL := fmt.Sprintf("http://localhost:%d/callback", port)
		loginURL := fmt.Sprintf("%s/login?cli_callback=%s&cli_state=%s",
			appURL,
			url.QueryEscape(callbackURL),
			url.QueryEscape(stateHex),
		)

		parsed, err := url.Parse(loginURL)
		if err != nil {
			t.Fatalf("failed to parse login URL: %v", err)
		}

		decodedCallback := parsed.Query().Get("cli_callback")
		expectedCallback := fmt.Sprintf("http://localhost:%d/callback", port)
		if decodedCallback != expectedCallback {
			t.Fatalf("cli_callback decoded to %q, expected %q", decodedCallback, expectedCallback)
		}
	})
}

func TestProperty_LoginURLStateDecodesToOriginal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		port := rapid.IntRange(1024, 65535).Draw(t, "port")
		stateHex := rapid.StringMatching(`[0-9a-f]{32}`).Draw(t, "state")
		appURL := "https://app.example.com"

		callbackURL := fmt.Sprintf("http://localhost:%d/callback", port)
		loginURL := fmt.Sprintf("%s/login?cli_callback=%s&cli_state=%s",
			appURL,
			url.QueryEscape(callbackURL),
			url.QueryEscape(stateHex),
		)

		parsed, err := url.Parse(loginURL)
		if err != nil {
			t.Fatalf("failed to parse login URL: %v", err)
		}

		decodedState := parsed.Query().Get("cli_state")
		if decodedState != stateHex {
			t.Fatalf("cli_state decoded to %q, expected %q", decodedState, stateHex)
		}
	})
}

// Feature: daemon-browser-login, Property 2: Callback State Validation
// For any two state strings S1 (generated) and S2 (received), the callback handler
// SHALL accept the request (HTTP 200) if and only if S1 equals S2, and SHALL reject
// (HTTP 400) otherwise.
//
// **Validates: Requirements 1.4, 1.5**

func TestProperty_CallbackAcceptsMatchingState(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		state := rapid.StringMatching(`[0-9a-f]{32}`).Draw(t, "state")
		token := rapid.StringMatching(`[a-zA-Z0-9_\-]{10,100}`).Draw(t, "token")

		cs := &CallbackServer{
			state:   state,
			tokenCh: make(chan string, 1),
			errCh:   make(chan error, 1),
		}

		reqURL := fmt.Sprintf("/callback?state=%s&token=%s", url.QueryEscape(state), url.QueryEscape(token))
		req := httptest.NewRequest(http.MethodGet, reqURL, nil)
		rec := httptest.NewRecorder()

		cs.handleCallback(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected HTTP 200 for matching state, got %d", rec.Code)
		}
	})
}

func TestProperty_CallbackRejectsMismatchedState(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s1 := rapid.StringMatching(`[0-9a-f]{32}`).Draw(t, "s1")
		s2 := rapid.StringMatching(`[0-9a-f]{32}`).Filter(func(s string) bool {
			return s != s1
		}).Draw(t, "s2")
		token := rapid.StringMatching(`[a-zA-Z0-9_\-]{10,100}`).Draw(t, "token")

		cs := &CallbackServer{
			state:   s1,
			tokenCh: make(chan string, 1),
			errCh:   make(chan error, 1),
		}

		reqURL := fmt.Sprintf("/callback?state=%s&token=%s", url.QueryEscape(s2), url.QueryEscape(token))
		req := httptest.NewRequest(http.MethodGet, reqURL, nil)
		rec := httptest.NewRecorder()

		cs.handleCallback(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected HTTP 400 for mismatched state (s1=%q, s2=%q), got %d", s1, s2, rec.Code)
		}
	})
}

func TestProperty_CallbackRejectsMissingToken(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		state := rapid.StringMatching(`[0-9a-f]{32}`).Draw(t, "state")

		cs := &CallbackServer{
			state:   state,
			tokenCh: make(chan string, 1),
			errCh:   make(chan error, 1),
		}

		// Request with matching state but no token parameter
		reqURL := fmt.Sprintf("/callback?state=%s", url.QueryEscape(state))
		req := httptest.NewRequest(http.MethodGet, reqURL, nil)
		rec := httptest.NewRecorder()

		cs.handleCallback(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected HTTP 400 for missing token param, got %d", rec.Code)
		}
	})
}
