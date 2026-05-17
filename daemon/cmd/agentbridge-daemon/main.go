package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/user/agentbridge/daemon/internal/agent"
	"github.com/user/agentbridge/daemon/internal/connection"
	"github.com/user/agentbridge/daemon/internal/executor"
	"github.com/user/agentbridge/daemon/internal/heartbeat"
	"github.com/user/agentbridge/daemon/internal/logging"
	"github.com/user/agentbridge/daemon/pkg/protocol"
)

const (
	// defaultServerURL is the default WebSocket server URL.
	defaultServerURL = "ws://localhost:8080/ws/daemon"

	// defaultDataDir is the default directory for daemon state files.
	defaultDataDir = ".agentbridge"

	// pidFileName is the name of the PID file.
	pidFileName = "daemon.pid"

	// statusFileName is the name of the status file written by the daemon process.
	statusFileName = "daemon.status"

	// startupTimeout is the maximum time allowed for startup to complete.
	startupTimeout = 10 * time.Second

	// statusUpdateInterval is how often the daemon writes its status file.
	statusUpdateInterval = 2 * time.Second
)

// DaemonStatus represents the daemon's current state, serialized to the status file.
type DaemonStatus struct {
	ConnectionState string              `json:"connection_state"` // connected, disconnected, reconnecting
	StartedAt       time.Time           `json:"started_at"`
	Agents          []protocol.RuntimeInfo `json:"agents"`
	TaskStatus      string              `json:"task_status"` // idle, executing
	PID             int                 `json:"pid"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "start":
		if err := runStart(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "stop":
		if err := runStop(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "status":
		if err := runStatus(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "--help", "-h", "help":
		printUsage()
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: agentbridge-daemon <command>

Commands:
  start    Start the daemon (background process)
  stop     Stop the running daemon
  status   Show daemon status

Environment Variables:
  AGENTBRIDGE_SERVER_URL   Server WebSocket URL (default: %s)
  AGENTBRIDGE_TOKEN        Authentication token for server connection
  AGENTBRIDGE_USER_ID      User ID for daemon registration
  AGENTBRIDGE_DAEMON_ID    Daemon ID (default: hostname-based)
  AGENTBRIDGE_DATA_DIR     Data directory (default: ~/.agentbridge)
  AGENTBRIDGE_LOG_FILE     Log file path (default: ~/.agentbridge/daemon.log)
`, defaultServerURL)
}

// runStart implements the "start" command logic.
func runStart() error {
	// If AGENTBRIDGE_DAEMON_CHILD is set, we are the forked child process.
	if os.Getenv("AGENTBRIDGE_DAEMON_CHILD") == "1" {
		return runDaemonProcess()
	}

	// Parent process: check PID, fork child, wait for confirmation.
	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to determine data directory: %w", err)
	}

	pidPath := filepath.Join(dataDir, pidFileName)

	// Check if daemon is already running.
	if isAlreadyRunning(pidPath) {
		return errors.New("daemon is already running")
	}

	// Ensure data directory exists.
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Fork the daemon as a child process.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to determine executable path: %w", err)
	}

	cmd := exec.Command(exe, "start")
	cmd.Env = append(os.Environ(), "AGENTBRIDGE_DAEMON_CHILD=1")
	cmd.Stdout = nil
	cmd.Stderr = nil

	// Detach from parent process group.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon process: %w", err)
	}

	// Wait briefly for the PID file to appear, confirming startup.
	deadline := time.Now().Add(startupTimeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidPath); err == nil {
			fmt.Printf("agentbridge-daemon started (PID %d)\n", cmd.Process.Pid)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	// If PID file didn't appear, the child likely failed.
	return errors.New("daemon failed to start within timeout (check logs)")
}

// runStop implements the "stop" command logic.
func runStop() error {
	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to determine data directory: %w", err)
	}

	pidPath := filepath.Join(dataDir, pidFileName)

	// Read PID file.
	data, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("no running daemon found")
			return nil
		}
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		// PID file is corrupted; clean it up.
		os.Remove(pidPath)
		fmt.Println("no running daemon found (removed stale PID file)")
		return nil
	}

	// Check if the process is actually running.
	proc, err := os.FindProcess(pid)
	if err != nil {
		os.Remove(pidPath)
		fmt.Println("no running daemon found (removed stale PID file)")
		return nil
	}

	// On Unix, FindProcess always succeeds. Send signal 0 to check if process exists.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		// Process is not running; clean up stale PID file.
		os.Remove(pidPath)
		fmt.Println("no running daemon found (removed stale PID file)")
		return nil
	}

	// Send SIGTERM to trigger graceful shutdown.
	// The daemon process handles SIGTERM by deregistering from the server,
	// closing the WebSocket connection, and removing the PID file.
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send stop signal to daemon (PID %d): %w", pid, err)
	}

	// Wait for the process to exit (up to 10 seconds).
	deadline := time.Now().Add(startupTimeout)
	for time.Now().Before(deadline) {
		// Check if process is still running.
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			// Process has exited.
			// Clean up PID file if it still exists (daemon should remove it, but be safe).
			os.Remove(pidPath)
			fmt.Printf("agentbridge-daemon stopped (PID %d)\n", pid)
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Process didn't exit within timeout; force kill.
	if err := proc.Signal(syscall.SIGKILL); err == nil {
		os.Remove(pidPath)
		fmt.Printf("agentbridge-daemon forcefully stopped (PID %d)\n", pid)
		return nil
	}

	return fmt.Errorf("daemon (PID %d) did not stop within 10 seconds", pid)
}

// runStatus implements the "status" command logic.
// It reads the daemon's status file and displays connection state, uptime,
// detected agents, and task status.
func runStatus() error {
	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to determine data directory: %w", err)
	}

	pidPath := filepath.Join(dataDir, pidFileName)
	statusPath := filepath.Join(dataDir, statusFileName)

	// Check if daemon is running via PID file.
	if !isAlreadyRunning(pidPath) {
		fmt.Println("Status: not running")
		fmt.Println("No daemon process is currently active.")
		return nil
	}

	// Read the status file.
	data, err := os.ReadFile(statusPath)
	if err != nil {
		// Daemon is running but no status file — might be starting up.
		if os.IsNotExist(err) {
			fmt.Println("Status: starting")
			fmt.Println("Daemon process is running but status is not yet available.")
			return nil
		}
		return fmt.Errorf("failed to read status file: %w", err)
	}

	var status DaemonStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return fmt.Errorf("failed to parse status file: %w", err)
	}

	// Check if the status file is stale (older than 10 seconds).
	if time.Since(status.UpdatedAt) > 10*time.Second {
		fmt.Println("Status: stale (status file not recently updated)")
		fmt.Printf("  Last updated: %s ago\n", formatDuration(time.Since(status.UpdatedAt)))
		return nil
	}

	// Display status information.
	uptime := time.Since(status.StartedAt)

	fmt.Printf("Connection: %s\n", status.ConnectionState)
	fmt.Printf("Uptime: %.0f seconds\n", uptime.Seconds())
	fmt.Printf("Task: %s\n", status.TaskStatus)
	fmt.Printf("PID: %d\n", status.PID)

	// Display detected agents.
	if len(status.Agents) == 0 {
		fmt.Println("Agents: none detected")
	} else {
		fmt.Printf("Agents: %d detected\n", len(status.Agents))
		for _, a := range status.Agents {
			statusLabel := a.Status
			fmt.Printf("  - %s %s (%s)\n", a.AgentType, a.Version, statusLabel)
		}
	}

	return nil
}

// formatDuration formats a duration into a human-readable string.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}

// runDaemonProcess is the main loop for the background daemon process.
// It wires all daemon components together:
//   AgentDetector → ServerConnection → HeartbeatTicker → TaskHandler
// and handles graceful shutdown on SIGTERM/SIGINT.
func runDaemonProcess() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load configuration from environment.
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	dataDir, err := getDataDir()
	if err != nil {
		return fmt.Errorf("failed to determine data directory: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}

	// Initialize logger.
	logFilePath := os.Getenv("AGENTBRIDGE_LOG_FILE")
	if logFilePath == "" {
		logFilePath = filepath.Join(dataDir, logging.DefaultLogFileName)
	}
	logger, err := logging.New(logging.Config{
		FilePath:   logFilePath,
		MaxSize:    logging.DefaultMaxSize,
		MaxBackups: logging.DefaultMaxBackups,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}
	defer logger.Close()

	// Redirect standard log output to the file logger.
	log.SetOutput(logger.Writer())
	log.SetFlags(log.LstdFlags)

	logger.Printf("daemon starting (PID %d)", os.Getpid())

	pidPath := filepath.Join(dataDir, pidFileName)
	statusPath := filepath.Join(dataDir, statusFileName)

	// Write PID file.
	pid := os.Getpid()
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(pid)), 0o644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	defer os.Remove(pidPath)
	defer os.Remove(statusPath)

	startedAt := time.Now()

	// --- Component 1: AgentDetector ---
	// Perform initial agent detection.
	detector := agent.NewDetector(0) // use default rescan interval
	runtimes := detector.Scan()

	if len(runtimes) == 0 {
		logger.Println("warning: no supported agent CLIs detected")
	}

	// Status tracking state.
	var statusMu sync.RWMutex
	connectionState := "disconnected"
	taskStatus := "idle"
	currentRuntimes := runtimes

	// Runtime resolver: maps runtime IDs (agent_type) to binary paths.
	// The TaskHandler uses this to find the binary for a given runtime.
	var runtimesMu sync.RWMutex
	runtimeMap := buildRuntimeMap(runtimes)

	runtimeResolver := func(runtimeID string) (string, bool) {
		runtimesMu.RLock()
		defer runtimesMu.RUnlock()
		path, ok := runtimeMap[runtimeID]
		return path, ok
	}

	// Helper to write the status file.
	writeStatus := func() {
		statusMu.RLock()
		status := DaemonStatus{
			ConnectionState: connectionState,
			StartedAt:       startedAt,
			Agents:          currentRuntimes,
			TaskStatus:      taskStatus,
			PID:             pid,
			UpdatedAt:       time.Now(),
		}
		statusMu.RUnlock()

		data, err := json.Marshal(status)
		if err != nil {
			return
		}
		_ = os.WriteFile(statusPath, data, 0o644)
	}

	// --- Component 2: ServerConnection ---
	// Connect to server via WebSocket.
	conn := connection.NewConnection(cfg.ServerURL, cfg.Token)

	connectCtx, connectCancel := context.WithTimeout(ctx, startupTimeout)
	defer connectCancel()

	if err := conn.Connect(connectCtx); err != nil {
		// Remove PID file on connection failure.
		os.Remove(pidPath)
		logger.Printf("failed to connect to server: %v", err)
		return fmt.Errorf("failed to connect to server: %w", err)
	}

	// Update connection state.
	statusMu.Lock()
	connectionState = "connected"
	statusMu.Unlock()
	logger.Printf("connected to server at %s", cfg.ServerURL)

	// Register with server.
	if err := sendRegistration(conn, cfg.DaemonID, cfg.UserID, runtimes); err != nil {
		conn.Close()
		os.Remove(pidPath)
		logger.Printf("failed to register with server: %v", err)
		return fmt.Errorf("failed to register with server: %w", err)
	}
	logger.Printf("registered with server (daemon_id=%s, runtimes=%d)", cfg.DaemonID, len(runtimes))

	// Write initial status file.
	writeStatus()

	// --- Component 3: HeartbeatTicker ---
	// Start heartbeat ticker to maintain liveness with the server.
	hb := heartbeat.NewTicker(0, conn.Send) // use default interval (15s)
	hb.Start()

	// --- Component 4: TaskHandler ---
	// Wire the TaskHandler to listen for chat:task messages from the server
	// and dispatch them to the AgentExecutor for CLI invocation.
	exec := executor.NewExecutor()
	executor.RegisterWithConnection(conn, exec, runtimeResolver)
	logger.Println("task handler registered for chat:task messages")

	// --- Periodic Rescan ---
	// Start the agent scanner for periodic rescan. When runtimes change,
	// update the runtime map and re-register with the server.
	scanner := agent.NewScanner(detector, func(newRuntimes []protocol.RuntimeInfo) {
		logger.Printf("agent rescan detected changes: %d runtimes", len(newRuntimes))

		// Update the runtime map for the TaskHandler resolver.
		runtimesMu.Lock()
		runtimeMap = buildRuntimeMap(newRuntimes)
		runtimesMu.Unlock()

		// Update status tracking.
		statusMu.Lock()
		currentRuntimes = newRuntimes
		statusMu.Unlock()

		// Re-register with server to update runtime list.
		if err := sendRegistration(conn, cfg.DaemonID, cfg.UserID, newRuntimes); err != nil {
			logger.Printf("failed to re-register after rescan: %v", err)
		} else {
			logger.Printf("re-registered with server after rescan (runtimes=%d)", len(newRuntimes))
		}
	})

	// Run scanner in background goroutine.
	scannerDone := make(chan struct{})
	go func() {
		defer close(scannerDone)
		scanner.Start(ctx)
	}()

	// Start status file updater goroutine.
	statusDone := make(chan struct{})
	go func() {
		defer close(statusDone)
		ticker := time.NewTicker(statusUpdateInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Update connection state based on actual connection status.
				statusMu.Lock()
				if conn.IsConnected() {
					connectionState = "connected"
				} else {
					connectionState = "disconnected"
				}
				statusMu.Unlock()
				writeStatus()
			}
		}
	}()

	// --- Graceful Shutdown ---
	// Set up signal handling for SIGTERM/SIGINT.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Wait for shutdown signal.
	select {
	case sig := <-sigCh:
		logger.Printf("received signal %v, shutting down", sig)
	case <-ctx.Done():
		logger.Println("context cancelled, shutting down")
	}

	// Stop signal notifications.
	signal.Stop(sigCh)

	// Cancel context to stop scanner and status updater.
	cancel()

	// Stop the heartbeat ticker.
	hb.Stop()
	logger.Println("heartbeat ticker stopped")

	// Wait for background goroutines to finish.
	<-scannerDone
	<-statusDone

	// Close the WebSocket connection (sends close frame to server).
	conn.Close()
	logger.Println("server connection closed")

	// Clean up PID and status files (handled by defers, but log for clarity).
	logger.Println("daemon stopped")

	return nil
}

// buildRuntimeMap creates a map from runtime identifiers to binary paths.
// It maps both the agent_type (e.g., "claude") and the binary path itself
// as keys, so the TaskHandler can resolve runtimes by either identifier.
func buildRuntimeMap(runtimes []protocol.RuntimeInfo) map[string]string {
	m := make(map[string]string, len(runtimes)*2)
	for _, r := range runtimes {
		if r.Status == "available" && r.BinaryPath != "" {
			m[r.AgentType] = r.BinaryPath
			m[r.BinaryPath] = r.BinaryPath
		}
	}
	return m
}

// daemonConfig holds the daemon's runtime configuration.
type daemonConfig struct {
	ServerURL string
	Token     string
	UserID    string
	DaemonID  string
}

// loadConfig reads daemon configuration from environment variables.
func loadConfig() (*daemonConfig, error) {
	serverURL := os.Getenv("AGENTBRIDGE_SERVER_URL")
	if serverURL == "" {
		serverURL = defaultServerURL
	}

	token := os.Getenv("AGENTBRIDGE_TOKEN")
	if token == "" {
		return nil, errors.New("AGENTBRIDGE_TOKEN environment variable is required")
	}

	userID := os.Getenv("AGENTBRIDGE_USER_ID")
	if userID == "" {
		return nil, errors.New("AGENTBRIDGE_USER_ID environment variable is required")
	}

	daemonID := os.Getenv("AGENTBRIDGE_DAEMON_ID")
	if daemonID == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("failed to determine hostname for daemon ID: %w", err)
		}
		daemonID = fmt.Sprintf("daemon-%s", hostname)
	}

	return &daemonConfig{
		ServerURL: serverURL,
		Token:     token,
		UserID:    userID,
		DaemonID:  daemonID,
	}, nil
}

// getDataDir returns the path to the daemon's data directory.
func getDataDir() (string, error) {
	if dir := os.Getenv("AGENTBRIDGE_DATA_DIR"); dir != "" {
		return dir, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, defaultDataDir), nil
}

// isAlreadyRunning checks if a daemon process is already running by reading
// the PID file and verifying the process exists.
func isAlreadyRunning(pidPath string) bool {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return false
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return false
	}

	// Check if the process is still running.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// On Unix, FindProcess always succeeds. Send signal 0 to check if process exists.
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// sendRegistration sends the daemon:register message to the server.
func sendRegistration(conn *connection.Connection, daemonID, userID string, runtimes []protocol.RuntimeInfo) error {
	payload := protocol.DaemonRegisterPayload{
		DaemonID: daemonID,
		UserID:   userID,
		Runtimes: runtimes,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal registration payload: %w", err)
	}

	msg := protocol.Message{
		Type:    protocol.TypeDaemonRegister,
		Payload: payloadBytes,
	}

	return conn.Send(msg)
}
