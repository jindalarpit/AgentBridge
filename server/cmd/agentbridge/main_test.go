package main

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/user/agentbridge/server/internal/clientws"
	"github.com/user/agentbridge/server/internal/config"
	"github.com/user/agentbridge/server/internal/daemonws"
	"github.com/user/agentbridge/server/internal/handler"
	"github.com/user/agentbridge/server/internal/service"
	"github.com/user/agentbridge/server/pkg/protocol"
)

func TestServerWiring(t *testing.T) {
	// Test that all components can be wired together without panicking.
	os.Setenv("JWT_SECRET", "test-secret")
	os.Setenv("PORT", "19876")
	defer func() {
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("PORT")
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Service layer.
	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()
	messageQueue := service.NewMessageQueue()

	// WebSocket hubs.
	clientHub := clientws.NewHub()
	daemonHub := daemonws.NewHub()

	// Heartbeat checker.
	heartbeatChecker := daemonws.NewHeartbeatChecker(daemonws.DefaultHeartbeatInterval)

	// Wire heartbeat handler.
	daemonHub.SetHeartbeatHandler(func(daemonID string) {
		heartbeatChecker.RecordHeartbeat(daemonID)
		_ = runtimeSvc.UpdateHeartbeat(context.Background(), daemonID)
	})

	// Wire registration handler.
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

	// Start heartbeat checker.
	heartbeatChecker.Start(context.Background(), func(daemonID string) {
		_ = runtimeSvc.MarkOffline(context.Background(), daemonID)
	})
	defer heartbeatChecker.Stop()

	// Daemon relay.
	daemonRelay := service.NewDaemonRelay(daemonHub, clientHub, runtimeSvc, chatSvc)

	// WebSocket router.
	wsRouter := handler.NewWSRouter(clientHub, daemonHub, chatSvc, runtimeSvc, messageQueue)
	daemonRelay.SetOnResponseComplete(wsRouter.DrainQueue)

	// HTTP handlers.
	authHandler := handler.NewAuthHandler(cfg.JWTSecret)
	sessionHandler := handler.NewSessionHandler(chatSvc)
	runtimeHandler := handler.NewRuntimeHandler(runtimeSvc, chatSvc)

	// HTTP router.
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

	// Start server.
	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.Errorf("server error: %v", err)
		}
	}()

	// Give the server a moment to start.
	time.Sleep(50 * time.Millisecond)

	// Test health-like endpoint (auth register should respond).
	resp, err := http.Post("http://localhost:19876/api/auth/register", "application/json", nil)
	if err != nil {
		t.Fatalf("failed to reach server: %v", err)
	}
	defer resp.Body.Close()

	// We expect a 400 (bad request) since we sent no body, not a 500 or connection error.
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}

	// Shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("shutdown error: %v", err)
	}

	// Suppress unused variable warnings.
	_ = daemonRelay
	_ = wsRouter
}

func TestConfigLoading(t *testing.T) {
	// Test that config loads correctly with all env vars set.
	os.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/testdb")
	os.Setenv("JWT_SECRET", "test-jwt-secret")
	os.Setenv("PORT", "9090")
	os.Setenv("CORS_ORIGINS", "http://localhost:3000")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("PORT")
		os.Unsetenv("CORS_ORIGINS")
	}()

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.DatabaseURL != "postgres://test:test@localhost:5432/testdb" {
		t.Errorf("unexpected DatabaseURL: %q", cfg.DatabaseURL)
	}
	if cfg.JWTSecret != "test-jwt-secret" {
		t.Errorf("unexpected JWTSecret: %q", cfg.JWTSecret)
	}
	if cfg.Port != 9090 {
		t.Errorf("expected port 9090, got %d", cfg.Port)
	}
	if cfg.CORSOrigins != "http://localhost:3000" {
		t.Errorf("unexpected CORSOrigins: %q", cfg.CORSOrigins)
	}
}

func TestGracefulShutdown(t *testing.T) {
	// Test that the server shuts down gracefully.
	cfg := &config.Config{
		JWTSecret:    "test-secret",
		Port:         19877,
		CORSOrigins:  "*",
		RateLimitRPS: 0,
	}

	chatSvc := service.NewInMemoryChatService()
	runtimeSvc := service.NewInMemoryRuntimeService()

	clientHub := clientws.NewHub()
	daemonHub := daemonws.NewHub()

	authHandler := handler.NewAuthHandler(cfg.JWTSecret)
	sessionHandler := handler.NewSessionHandler(chatSvc)
	runtimeHandler := handler.NewRuntimeHandler(runtimeSvc, chatSvc)

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

	srv := &http.Server{
		Addr:    cfg.Addr(),
		Handler: router,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			t.Errorf("server error: %v", err)
		}
	}()

	time.Sleep(50 * time.Millisecond)

	// Verify server is running.
	resp, err := http.Post("http://localhost:19877/api/auth/login", "application/json", nil)
	if err != nil {
		t.Fatalf("server not reachable: %v", err)
	}
	resp.Body.Close()

	// Graceful shutdown.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown error: %v", err)
	}

	// Verify server is no longer accepting connections.
	_, err = http.Post("http://localhost:19877/api/auth/login", "application/json", nil)
	if err == nil {
		t.Error("expected connection error after shutdown, got nil")
	}
}
