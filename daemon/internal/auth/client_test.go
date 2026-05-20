package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExchangeJWT_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path.
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/daemon-tokens" {
			t.Errorf("path = %s, want /api/daemon-tokens", r.URL.Path)
		}

		// Verify Authorization header.
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-jwt-token" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-jwt-token")
		}

		// Verify Content-Type.
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("Content-Type = %q, want %q", ct, "application/json")
		}

		// Verify request body.
		var body exchangeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if body.Name != "Daemon (testhost)" {
			t.Errorf("name = %q, want %q", body.Name, "Daemon (testhost)")
		}
		if body.ExpiresInDays != 90 {
			t.Errorf("expires_in_days = %d, want 90", body.ExpiresInDays)
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"token": "ab_test1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd"})
	}))
	defer server.Close()

	client := NewTokenClient(server.URL)
	token, err := client.ExchangeJWT(context.Background(), "test-jwt-token", "Daemon (testhost)", 90)
	if err != nil {
		t.Fatalf("ExchangeJWT() error: %v", err)
	}
	if token != "ab_test1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd" {
		t.Errorf("token = %q, want %q", token, "ab_test1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcd")
	}
}

func TestExchangeJWT_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewTokenClient(server.URL)
	_, err := client.ExchangeJWT(context.Background(), "expired-jwt", "Daemon (host)", 90)
	if err == nil {
		t.Fatal("ExchangeJWT() expected error for 401 response, got nil")
	}
	if !strings.Contains(err.Error(), "invalid or expired credentials") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "invalid or expired credentials")
	}
}

func TestExchangeJWT_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewTokenClient(server.URL)
	_, err := client.ExchangeJWT(context.Background(), "some-jwt", "Daemon (host)", 90)
	if err == nil {
		t.Fatal("ExchangeJWT() expected error for 500 response, got nil")
	}
	if !strings.Contains(err.Error(), "server error") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "server error")
	}
}

func TestRevoke_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
		}
		if r.URL.Path != "/api/daemon-tokens/current" {
			t.Errorf("path = %s, want /api/daemon-tokens/current", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer ab_mytoken123" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer ab_mytoken123")
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewTokenClient(server.URL)
	err := client.Revoke(context.Background(), "ab_mytoken123")
	if err != nil {
		t.Fatalf("Revoke() error: %v", err)
	}
}

func TestRevoke_NetworkFailure(t *testing.T) {
	// Use an invalid URL that will cause a connection failure.
	client := NewTokenClient("http://127.0.0.1:1")
	err := client.Revoke(context.Background(), "ab_sometoken")
	if err == nil {
		t.Fatal("Revoke() expected error for network failure, got nil")
	}
	if !strings.Contains(err.Error(), "token revocation request failed") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "token revocation request failed")
	}
}

func TestGetMe_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/auth/me" {
			t.Errorf("path = %s, want /api/auth/me", r.URL.Path)
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer ab_validtoken" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer ab_validtoken")
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"email": "user@example.com"})
	}))
	defer server.Close()

	client := NewTokenClient(server.URL)
	email, err := client.GetMe(context.Background(), "ab_validtoken")
	if err != nil {
		t.Fatalf("GetMe() error: %v", err)
	}
	if email != "user@example.com" {
		t.Errorf("email = %q, want %q", email, "user@example.com")
	}
}

func TestGetMe_Unauthorized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewTokenClient(server.URL)
	_, err := client.GetMe(context.Background(), "ab_expiredtoken")
	if err == nil {
		t.Fatal("GetMe() expected error for 401 response, got nil")
	}
	if !strings.Contains(err.Error(), "invalid or expired token") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "invalid or expired token")
	}
}
