// Package auth provides HTTP client functionality for communicating with the
// AgentBridge server's daemon token API, including token exchange, revocation,
// and user identity retrieval.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TokenClient handles communication with the daemon-tokens API.
type TokenClient struct {
	serverURL  string
	httpClient *http.Client
}

// NewTokenClient creates a new TokenClient configured to talk to the given server URL.
// The serverURL should be the base URL of the AgentBridge server (e.g. "http://localhost:8080").
func NewTokenClient(serverURL string) *TokenClient {
	return &TokenClient{
		serverURL: strings.TrimRight(serverURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// exchangeRequest is the JSON body sent to POST /api/daemon-tokens.
type exchangeRequest struct {
	Name          string `json:"name"`
	ExpiresInDays int    `json:"expires_in_days"`
}

// exchangeResponse is the JSON body returned from POST /api/daemon-tokens.
type exchangeResponse struct {
	Token string `json:"token"`
}

// ExchangeJWT sends the short-lived JWT to the server and receives a daemon token.
// It POSTs to /api/daemon-tokens with the JWT as Bearer auth and a JSON body
// containing the token name and expiry. Returns the plaintext daemon token on success.
func (tc *TokenClient) ExchangeJWT(ctx context.Context, jwt string, name string, expiresInDays int) (string, error) {
	body := exchangeRequest{
		Name:          name,
		ExpiresInDays: expiresInDays,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal token exchange request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tc.serverURL+"/api/daemon-tokens", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("failed to create token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := tc.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token exchange request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("token exchange failed: invalid or expired credentials (HTTP 401)")
	}
	if resp.StatusCode >= 500 {
		return "", fmt.Errorf("token exchange failed: server error (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("token exchange failed: unexpected response (HTTP %d)", resp.StatusCode)
	}

	var result exchangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse token exchange response: %w", err)
	}
	if result.Token == "" {
		return "", fmt.Errorf("token exchange failed: server returned empty token")
	}

	return result.Token, nil
}

// Revoke deletes the current daemon token server-side.
// It sends DELETE to /api/daemon-tokens/current with the token as Bearer auth.
func (tc *TokenClient) Revoke(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, tc.serverURL+"/api/daemon-tokens/current", nil)
	if err != nil {
		return fmt.Errorf("failed to create revocation request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := tc.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("token revocation request failed: %w", err)
	}
	defer resp.Body.Close()

	// Drain body to allow connection reuse.
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("token revocation failed: invalid or expired token (HTTP 401)")
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("token revocation failed: server error (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("token revocation failed: unexpected response (HTTP %d)", resp.StatusCode)
	}

	return nil
}

// meResponse is the JSON body returned from GET /api/auth/me.
type meResponse struct {
	Email string `json:"email"`
}

// GetMe fetches the authenticated user's email address.
// It sends GET to /api/auth/me with the token as Bearer auth.
func (tc *TokenClient) GetMe(ctx context.Context, token string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tc.serverURL+"/api/auth/me", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create auth/me request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := tc.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("auth/me request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return "", fmt.Errorf("auth/me failed: invalid or expired token (HTTP 401)")
	}
	if resp.StatusCode >= 500 {
		return "", fmt.Errorf("auth/me failed: server error (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("auth/me failed: unexpected response (HTTP %d)", resp.StatusCode)
	}

	var result meResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse auth/me response: %w", err)
	}
	if result.Email == "" {
		return "", fmt.Errorf("auth/me failed: server returned empty email")
	}

	return result.Email, nil
}
