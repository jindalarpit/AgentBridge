// Package executor handles agent CLI invocation and output streaming.
package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

// DefaultTimeout is the maximum time allowed for an agent CLI to produce output.
const DefaultTimeout = 300 * time.Second

// ExecutionRequest contains all information needed to invoke an agent CLI.
type ExecutionRequest struct {
	SessionID  string
	RuntimeID  string
	BinaryPath string
	Content    string
	History    []protocol.HistoryItem
}

// StreamToken represents a single token emitted during agent response streaming.
type StreamToken struct {
	SessionID string
	Seq       int
	Content   string
	Done      bool
	Error     error
	ElapsedMs int64
}

// AgentExecutor defines the interface for invoking agent CLIs and streaming output.
type AgentExecutor interface {
	Execute(ctx context.Context, req ExecutionRequest) (<-chan StreamToken, error)
	Cancel(sessionID string) error
}

// activeSession tracks a running agent process for cancellation support.
type activeSession struct {
	cancel context.CancelFunc
	cmd    *exec.Cmd
}

// Executor implements AgentExecutor by spawning agent CLI processes.
type Executor struct {
	mu       sync.Mutex
	sessions map[string]*activeSession
	timeout  time.Duration
}

// NewExecutor creates a new Executor with default settings.
func NewExecutor() *Executor {
	return &Executor{
		sessions: make(map[string]*activeSession),
		timeout:  DefaultTimeout,
	}
}

// NewExecutorWithTimeout creates a new Executor with a custom timeout duration.
func NewExecutorWithTimeout(timeout time.Duration) *Executor {
	return &Executor{
		sessions: make(map[string]*activeSession),
		timeout:  timeout,
	}
}

// Execute spawns the agent CLI process, pipes conversation history via stdin,
// and streams stdout tokens back through the returned channel.
func (e *Executor) Execute(ctx context.Context, req ExecutionRequest) (<-chan StreamToken, error) {
	if req.BinaryPath == "" {
		return nil, fmt.Errorf("executor: binary path is required")
	}
	if req.SessionID == "" {
		return nil, fmt.Errorf("executor: session ID is required")
	}

	// Create a cancellable context with the configured timeout.
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)

	// Build the command. Different agent CLIs have different invocation patterns.
	args := buildAgentArgs(req.BinaryPath, req.Content)
	cmd := exec.CommandContext(execCtx, args[0], args[1:]...)

	// Pipe conversation history as JSON to stdin.
	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("executor: failed to create stdin pipe: %w", err)
	}

	// Capture stdout for streaming.
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("executor: failed to create stdout pipe: %w", err)
	}

	// Start the process.
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("executor: failed to start process: %w", err)
	}

	// Register the active session for cancellation support.
	e.mu.Lock()
	e.sessions[req.SessionID] = &activeSession{
		cancel: cancel,
		cmd:    cmd,
	}
	e.mu.Unlock()

	// Write history to stdin in a goroutine (non-blocking).
	go func() {
		defer stdinPipe.Close()
		if len(req.History) > 0 {
			_ = json.NewEncoder(stdinPipe).Encode(req.History)
		}
	}()

	// Create the output channel.
	tokenCh := make(chan StreamToken, 64)
	startTime := time.Now()

	// Stream stdout line-by-line in a goroutine.
	go func() {
		defer close(tokenCh)
		defer func() {
			e.mu.Lock()
			delete(e.sessions, req.SessionID)
			e.mu.Unlock()
			cancel()
		}()

		scanner := bufio.NewScanner(stdoutPipe)
		seq := 0

		for scanner.Scan() {
			seq++
			line := scanner.Text()
			token := StreamToken{
				SessionID: req.SessionID,
				Seq:       seq,
				Content:   line,
				ElapsedMs: time.Since(startTime).Milliseconds(),
			}

			select {
			case tokenCh <- token:
			case <-execCtx.Done():
				// Context cancelled or timed out.
				elapsed := time.Since(startTime).Milliseconds()
				if execCtx.Err() == context.DeadlineExceeded {
					tokenCh <- StreamToken{
						SessionID: req.SessionID,
						Seq:       seq + 1,
						Error:     fmt.Errorf("agent timeout: no response within %v", e.timeout),
						ElapsedMs: elapsed,
					}
				} else {
					tokenCh <- StreamToken{
						SessionID: req.SessionID,
						Seq:       seq + 1,
						Error:     fmt.Errorf("execution cancelled"),
						ElapsedMs: elapsed,
					}
				}
				return
			}
		}

		// Wait for the process to finish.
		waitErr := cmd.Wait()
		elapsed := time.Since(startTime).Milliseconds()

		if waitErr != nil {
			// Check if it was a timeout or cancellation.
			if execCtx.Err() == context.DeadlineExceeded {
				tokenCh <- StreamToken{
					SessionID: req.SessionID,
					Seq:       seq + 1,
					Error:     fmt.Errorf("agent timeout: no response within %v", e.timeout),
					ElapsedMs: elapsed,
				}
			} else if execCtx.Err() == context.Canceled {
				tokenCh <- StreamToken{
					SessionID: req.SessionID,
					Seq:       seq + 1,
					Error:     fmt.Errorf("execution cancelled"),
					ElapsedMs: elapsed,
				}
			} else {
				// Process crashed or exited with non-zero.
				tokenCh <- StreamToken{
					SessionID: req.SessionID,
					Seq:       seq + 1,
					Error:     fmt.Errorf("agent process error: %w", waitErr),
					ElapsedMs: elapsed,
				}
			}
			return
		}

		// Emit the Done token on successful completion.
		tokenCh <- StreamToken{
			SessionID: req.SessionID,
			Seq:       seq + 1,
			Done:      true,
			ElapsedMs: elapsed,
		}
	}()

	return tokenCh, nil
}

// Cancel terminates the agent process for the given session.
func (e *Executor) Cancel(sessionID string) error {
	e.mu.Lock()
	session, exists := e.sessions[sessionID]
	e.mu.Unlock()

	if !exists {
		return fmt.Errorf("executor: no active session found for %s", sessionID)
	}

	// Cancel the context, which will kill the process via exec.CommandContext.
	session.cancel()
	return nil
}

// buildAgentArgs constructs the correct command-line arguments for each agent CLI.
// Different agents have different invocation patterns:
//   - claude: claude -p "message" --output-format stream-json
//   - opencode: opencode run "message"
//   - gemini: gemini "message"
//   - codex: codex "message"
//   - others: binary "message" (default)
func buildAgentArgs(binaryPath, content string) []string {
	// Extract the binary name from the path for pattern matching.
	name := binaryPath
	if idx := lastIndexByte(name, '/'); idx >= 0 {
		name = name[idx+1:]
	}

	switch name {
	case "opencode":
		return []string{binaryPath, "run", content}
	case "claude":
		return []string{binaryPath, "-p", content, "--output-format", "stream-json"}
	default:
		return []string{binaryPath, content}
	}
}

// lastIndexByte returns the index of the last occurrence of c in s, or -1.
func lastIndexByte(s string, c byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			return i
		}
	}
	return -1
}
