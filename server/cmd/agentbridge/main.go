package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/user/agentbridge/server/internal/clientws"
	"github.com/user/agentbridge/server/internal/config"
	"github.com/user/agentbridge/server/internal/daemonws"
	"github.com/user/agentbridge/server/internal/handler"
	"github.com/user/agentbridge/server/internal/service"
	"github.com/user/agentbridge/server/pkg/protocol"
)

func main() {
	// Load configuration from environment variables.
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	log.Printf("AgentBridge server starting on %s", cfg.Addr())

	// --- Database Connection (optional) ---
	var dbPool *pgxpool.Pool
	if cfg.DatabaseURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("failed to create database pool: %v", err)
		}

		// Verify connectivity.
		if err := pool.Ping(ctx); err != nil {
			log.Fatalf("failed to connect to database: %v", err)
		}

		dbPool = pool
		defer dbPool.Close()
		log.Println("database connection established")

		// Run migrations.
		if err := runMigrations(ctx, dbPool); err != nil {
			log.Fatalf("failed to run migrations: %v", err)
		}
		log.Println("database migrations complete")
	} else {
		log.Println("DATABASE_URL not set, running with in-memory services")
	}

	// --- Service Layer ---
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	messageQueue := service.NewMessageQueue()

	// --- WebSocket Hubs ---
	clientHub := clientws.NewHub()
	daemonHub := daemonws.NewHub()

	// --- Heartbeat Checker ---
	heartbeatChecker := daemonws.NewHeartbeatChecker(daemonws.DefaultHeartbeatInterval)

	// Wire heartbeat handler: when a daemon sends a heartbeat, record it in
	// both the runtime service and the heartbeat checker.
	daemonHub.SetHeartbeatHandler(func(daemonID string) {
		heartbeatChecker.RecordHeartbeat(daemonID)
		if err := runtimeSvc.UpdateHeartbeat(context.Background(), daemonID); err != nil {
			log.Printf("heartbeat: failed to update heartbeat for daemon %s: %v", daemonID, err)
		}
	})

	// Wire registration handler: when a daemon registers, persist the registration.
	daemonHub.SetRegistrationHandler(func(identity daemonws.DaemonIdentity, payload protocol.DaemonRegisterPayload) error {
		reg := service.DaemonRegistration{
			DaemonID: payload.DaemonID,
			UserID:   identity.UserID,
			Runtimes: payload.Runtimes,
		}
		if err := runtimeSvc.RegisterDaemon(context.Background(), reg); err != nil {
			return err
		}
		heartbeatChecker.RecordHeartbeat(payload.DaemonID)
		return nil
	})

	// Start heartbeat checker with timeout callback.
	heartbeatChecker.Start(context.Background(), func(daemonID string) {
		if err := runtimeSvc.MarkOffline(context.Background(), daemonID); err != nil {
			log.Printf("heartbeat: failed to mark daemon %s offline: %v", daemonID, err)
		}
	})
	defer heartbeatChecker.Stop()

	// --- Daemon Relay (routes daemon messages to clients) ---
	daemonRelay := service.NewDaemonRelay(daemonHub, clientHub, runtimeSvc, chatSvc)

	// --- WebSocket Router (routes client messages to services) ---
	wsRouter := handler.NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, messageQueue)

	// Wire the daemon relay's response-complete callback to drain the message queue.
	daemonRelay.SetOnResponseComplete(wsRouter.DrainQueue)

	// --- HTTP Handlers ---
	authHandler := handler.NewAuthHandler(cfg.JWTSecret)
	sessionHandler := handler.NewSessionHandler(chatSvc)
	runtimeHandler := handler.NewRuntimeHandler(runtimeSvc, chatSvc)

	// --- HTTP Router ---
	routerCfg := handler.RouterConfig{
		JWTSecret:    cfg.JWTSecret,
		CORSOrigins:  cfg.CORSOrigins,
		RateLimitRPS: cfg.RateLimitRPS,
	}

	routerDeps := handler.RouterDeps{
		AuthHandler:    authHandler,
		SessionHandler: sessionHandler,
		RuntimeHandler: runtimeHandler,
		ClientHub:      clientHub,
		DaemonHub:      daemonHub,
	}

	router := handler.NewRouter(routerCfg, routerDeps)

	// --- HTTP Server ---
	srv := &http.Server{
		Addr:         cfg.Addr(),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// --- Graceful Shutdown ---
	// Start the server in a goroutine.
	go func() {
		log.Printf("HTTP server listening on %s", cfg.Addr())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	// Wait for interrupt signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	log.Printf("received signal %v, shutting down gracefully...", sig)

	// Give outstanding requests up to 30 seconds to complete.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	log.Println("server stopped")
}

// runMigrations executes SQL migration files against the database.
// It reads migration files from the server/migrations/ directory and applies
// them in order. For simplicity, it uses a migrations tracking table.
func runMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Create migrations tracking table if it doesn't exist.
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Define migrations in order.
	migrations := []struct {
		version string
		file    string
	}{
		{version: "001_initial_schema", file: "migrations/001_initial_schema.up.sql"},
	}

	for _, m := range migrations {
		// Check if already applied.
		var exists bool
		err := pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)", m.version).Scan(&exists)
		if err != nil {
			return fmt.Errorf("failed to check migration %s: %w", m.version, err)
		}
		if exists {
			continue
		}

		// Read and execute the migration file.
		sql, err := os.ReadFile(m.file)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", m.file, err)
		}

		_, err = pool.Exec(ctx, string(sql))
		if err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", m.version, err)
		}

		// Record the migration.
		_, err = pool.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", m.version)
		if err != nil {
			return fmt.Errorf("failed to record migration %s: %w", m.version, err)
		}

		log.Printf("applied migration: %s", m.version)
	}

	return nil
}
