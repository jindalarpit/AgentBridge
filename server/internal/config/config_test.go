package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Clear relevant env vars to test defaults.
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("JWT_SECRET")
	os.Unsetenv("PORT")
	os.Unsetenv("CORS_ORIGINS")
	os.Unsetenv("RATE_LIMIT_RPS")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.DatabaseURL != "" {
		t.Errorf("expected empty DatabaseURL, got %q", cfg.DatabaseURL)
	}
	if cfg.JWTSecret != "dev-secret-change-me" {
		t.Errorf("expected default JWTSecret, got %q", cfg.JWTSecret)
	}
	if cfg.Port != 8080 {
		t.Errorf("expected Port 8080, got %d", cfg.Port)
	}
	if cfg.CORSOrigins != "" {
		t.Errorf("expected empty CORSOrigins, got %q", cfg.CORSOrigins)
	}
	if cfg.RateLimitRPS != 100 {
		t.Errorf("expected RateLimitRPS 100, got %d", cfg.RateLimitRPS)
	}
}

func TestLoad_FromEnv(t *testing.T) {
	os.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")
	os.Setenv("JWT_SECRET", "my-secret")
	os.Setenv("PORT", "9090")
	os.Setenv("CORS_ORIGINS", "http://localhost:3000,http://localhost:4000")
	os.Setenv("RATE_LIMIT_RPS", "50")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("PORT")
		os.Unsetenv("CORS_ORIGINS")
		os.Unsetenv("RATE_LIMIT_RPS")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.DatabaseURL != "postgres://user:pass@localhost:5432/db" {
		t.Errorf("unexpected DatabaseURL: %q", cfg.DatabaseURL)
	}
	if cfg.JWTSecret != "my-secret" {
		t.Errorf("unexpected JWTSecret: %q", cfg.JWTSecret)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected Port 9090, got %d", cfg.Port)
	}
	if cfg.CORSOrigins != "http://localhost:3000,http://localhost:4000" {
		t.Errorf("unexpected CORSOrigins: %q", cfg.CORSOrigins)
	}
	if cfg.RateLimitRPS != 50 {
		t.Errorf("expected RateLimitRPS 50, got %d", cfg.RateLimitRPS)
	}
}

func TestLoad_InvalidPort(t *testing.T) {
	os.Setenv("PORT", "not-a-number")
	defer os.Unsetenv("PORT")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid PORT, got nil")
	}
}

func TestLoad_PortOutOfRange(t *testing.T) {
	os.Setenv("PORT", "99999")
	defer os.Unsetenv("PORT")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for out-of-range PORT, got nil")
	}
}

func TestConfig_Validate(t *testing.T) {
	cfg := &Config{
		JWTSecret: "secret",
		Port:      8080,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() returned error: %v", err)
	}
}

func TestConfig_Validate_MissingSecret(t *testing.T) {
	cfg := &Config{
		JWTSecret: "",
		Port:      8080,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty JWTSecret, got nil")
	}
}

func TestConfig_Validate_InvalidPort(t *testing.T) {
	cfg := &Config{
		JWTSecret: "secret",
		Port:      0,
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for invalid Port, got nil")
	}
}

func TestConfig_Addr(t *testing.T) {
	cfg := &Config{Port: 3000}
	if addr := cfg.Addr(); addr != ":3000" {
		t.Errorf("expected :3000, got %q", addr)
	}
}
