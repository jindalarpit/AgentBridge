// Package config handles server configuration loading from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all server configuration values loaded from environment variables.
type Config struct {
	// DatabaseURL is the PostgreSQL connection string.
	// If empty, the server runs with in-memory services only.
	DatabaseURL string

	// JWTSecret is the secret used for signing and validating JWT tokens.
	JWTSecret string

	// Port is the HTTP server listen port.
	Port int

	// CORSOrigins is a comma-separated list of allowed CORS origins.
	// Use "*" to allow all origins.
	CORSOrigins string

	// RateLimitRPS is the maximum requests per second per IP.
	// A value of 0 disables rate limiting.
	RateLimitRPS int
}

// Load reads configuration from environment variables with sensible defaults
// for local development.
func Load() (*Config, error) {
	cfg := &Config{
		DatabaseURL:  os.Getenv("DATABASE_URL"),
		JWTSecret:    os.Getenv("JWT_SECRET"),
		CORSOrigins:  os.Getenv("CORS_ORIGINS"),
		Port:         8080,
		RateLimitRPS: 100,
	}

	// Parse PORT if set.
	if portStr := os.Getenv("PORT"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT value %q: %w", portStr, err)
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("PORT must be between 1 and 65535, got %d", port)
		}
		cfg.Port = port
	}

	// Parse RATE_LIMIT_RPS if set.
	if rpsStr := os.Getenv("RATE_LIMIT_RPS"); rpsStr != "" {
		rps, err := strconv.Atoi(rpsStr)
		if err != nil {
			return nil, fmt.Errorf("invalid RATE_LIMIT_RPS value %q: %w", rpsStr, err)
		}
		cfg.RateLimitRPS = rps
	}

	// JWT_SECRET is required for production but we allow a default for development.
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = "dev-secret-change-me"
	}

	return cfg, nil
}

// Validate checks that all required configuration values are present and valid.
func (c *Config) Validate() error {
	if c.JWTSecret == "" {
		return fmt.Errorf("JWT_SECRET is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("PORT must be between 1 and 65535")
	}
	return nil
}

// Addr returns the listen address string (e.g., ":8080").
func (c *Config) Addr() string {
	return fmt.Sprintf(":%d", c.Port)
}
