package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/user/agentbridge/daemon/internal/auth"
	"github.com/user/agentbridge/daemon/internal/config"
)

// runLogout revokes the token server-side (best-effort) and removes it locally.
func runLogout(configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if config.IsTokenEmpty(cfg.Token) {
		fmt.Println("No active session found.")
		return nil
	}

	// Attempt server-side revocation with a 10-second timeout.
	var revokeErr error
	if cfg.ServerURL != "" {
		client := auth.NewTokenClient(cfg.ServerURL)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		revokeErr = client.Revoke(ctx, cfg.Token)
	}

	// Remove token from config regardless of revocation result, preserving other fields.
	cfg.Token = ""
	if err := config.Save(cfg, configPath); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	// If revocation failed, print warning to stderr.
	if revokeErr != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to revoke token server-side: %v\nThe server-side token may still be active.\n", revokeErr)
	}

	fmt.Println("Successfully logged out.")
	return nil
}
