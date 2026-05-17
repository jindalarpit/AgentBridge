package executor

import (
	"context"
	"testing"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
)

func TestNewExecutor(t *testing.T) {
	e := NewExecutor()
	if e == nil {
		t.Fatal("NewExecutor returned nil")
	}
	if e.timeout != DefaultTimeout {
		t.Errorf("expected timeout %v, got %v", DefaultTimeout, e.timeout)
	}
	if e.sessions == nil {
		t.Fatal("sessions map is nil")
	}
}

func TestNewExecutorWithTimeout(t *testing.T) {
	timeout := 10 * time.Second
	e := NewExecutorWithTimeout(timeout)
	if e.timeout != timeout {
		t.Errorf("expected timeout %v, got %v", timeout, e.timeout)
	}
}

func TestExecute_EmptyBinaryPath(t *testing.T) {
	e := NewExecutor()
	_, err := e.Execute(context.Background(), ExecutionRequest{
		SessionID:  "session-1",
		BinaryPath: "",
		Content:    "hello",
	})
	if err == nil {
		t.Fatal("expected error for empty binary path")
	}
}

func TestExecute_EmptySessionID(t *testing.T) {
	e := NewExecutor()
	_, err := e.Execute(context.Background(), ExecutionRequest{
		SessionID:  "",
		BinaryPath: "/bin/echo",
		Content:    "hello",
	})
	if err == nil {
		t.Fatal("expected error for empty session ID")
	}
}

func TestExecute_EchoCommand(t *testing.T) {
	e := NewExecutorWithTimeout(5 * time.Second)

	req := ExecutionRequest{
		SessionID:  "test-session-1",
		BinaryPath: "/bin/echo",
		Content:    "hello world",
	}

	ch, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var tokens []StreamToken
	for token := range ch {
		tokens = append(tokens, token)
	}

	if len(tokens) == 0 {
		t.Fatal("expected at least one token")
	}

	// echo outputs "hello world" as a single line, then we get a Done token.
	// Find the content token.
	var contentToken *StreamToken
	var doneToken *StreamToken
	for i := range tokens {
		if tokens[i].Content == "hello world" {
			contentToken = &tokens[i]
		}
		if tokens[i].Done {
			doneToken = &tokens[i]
		}
	}

	if contentToken == nil {
		t.Errorf("expected a token with content 'hello world', got tokens: %+v", tokens)
	}
	if doneToken == nil {
		t.Fatal("expected a Done token")
	}
}

func TestExecute_MonotonicSequenceNumbers(t *testing.T) {
	e := NewExecutorWithTimeout(5 * time.Second)

	// Use printf to output multiple lines.
	req := ExecutionRequest{
		SessionID:  "test-session-seq",
		BinaryPath: "/usr/bin/printf",
		Content:    "line1\nline2\nline3\n",
		History:    []protocol.HistoryItem{},
	}

	ch, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var tokens []StreamToken
	for token := range ch {
		tokens = append(tokens, token)
	}

	// Verify sequence numbers are monotonically increasing.
	prevSeq := 0
	for _, token := range tokens {
		if token.Seq <= prevSeq {
			t.Errorf("sequence not monotonically increasing: prev=%d, current=%d", prevSeq, token.Seq)
		}
		prevSeq = token.Seq
	}
}

func TestExecute_Timeout(t *testing.T) {
	// Use a very short timeout to trigger timeout behavior.
	e := NewExecutorWithTimeout(100 * time.Millisecond)

	req := ExecutionRequest{
		SessionID:  "test-session-timeout",
		BinaryPath: "/bin/sleep",
		Content:    "10",
	}

	ch, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var tokens []StreamToken
	for token := range ch {
		tokens = append(tokens, token)
	}

	// Should get an error token due to timeout.
	if len(tokens) == 0 {
		t.Fatal("expected at least one token (error)")
	}

	lastToken := tokens[len(tokens)-1]
	if lastToken.Error == nil {
		t.Fatal("expected error token due to timeout")
	}
}

func TestExecute_Cancel(t *testing.T) {
	e := NewExecutorWithTimeout(30 * time.Second)

	req := ExecutionRequest{
		SessionID:  "test-session-cancel",
		BinaryPath: "/bin/sleep",
		Content:    "30",
	}

	ch, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Give the process a moment to start.
	time.Sleep(50 * time.Millisecond)

	// Cancel the session.
	if err := e.Cancel("test-session-cancel"); err != nil {
		t.Fatalf("Cancel failed: %v", err)
	}

	var tokens []StreamToken
	for token := range ch {
		tokens = append(tokens, token)
	}

	// Should get an error token due to cancellation.
	if len(tokens) == 0 {
		t.Fatal("expected at least one token (cancellation error)")
	}

	lastToken := tokens[len(tokens)-1]
	if lastToken.Error == nil {
		t.Fatal("expected error token due to cancellation")
	}
}

func TestCancel_NoActiveSession(t *testing.T) {
	e := NewExecutor()
	err := e.Cancel("nonexistent-session")
	if err == nil {
		t.Fatal("expected error for non-existent session")
	}
}

func TestExecute_WithHistory(t *testing.T) {
	e := NewExecutorWithTimeout(5 * time.Second)

	req := ExecutionRequest{
		SessionID:  "test-session-history",
		BinaryPath: "/bin/echo",
		Content:    "response",
		History: []protocol.HistoryItem{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
		},
	}

	ch, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var tokens []StreamToken
	for token := range ch {
		tokens = append(tokens, token)
	}

	// Should complete successfully even with history.
	var doneToken *StreamToken
	for i := range tokens {
		if tokens[i].Done {
			doneToken = &tokens[i]
		}
	}
	if doneToken == nil {
		t.Fatal("expected a Done token")
	}
}

func TestExecute_InvalidBinary(t *testing.T) {
	e := NewExecutorWithTimeout(5 * time.Second)

	req := ExecutionRequest{
		SessionID:  "test-session-invalid",
		BinaryPath: "/nonexistent/binary/path",
		Content:    "hello",
	}

	_, err := e.Execute(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for invalid binary path")
	}
}

func TestExecute_ProcessCrash(t *testing.T) {
	e := NewExecutorWithTimeout(5 * time.Second)

	// Use sh -c "exit 1" to simulate a crash (non-zero exit).
	// Since BinaryPath is the command and Content is the first arg,
	// we pass the script via Content.
	req := ExecutionRequest{
		SessionID:  "test-session-crash",
		BinaryPath: "/bin/sh",
		Content:    "-c",
	}

	// This won't work as expected since Content becomes the arg.
	// Let's use "false" which always exits with 1.
	req = ExecutionRequest{
		SessionID:  "test-session-crash",
		BinaryPath: "/usr/bin/false",
		Content:    "",
	}

	ch, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var tokens []StreamToken
	for token := range ch {
		tokens = append(tokens, token)
	}

	// Should get an error token due to non-zero exit.
	if len(tokens) == 0 {
		t.Fatal("expected at least one token (error)")
	}

	lastToken := tokens[len(tokens)-1]
	if lastToken.Error == nil {
		t.Fatal("expected error token due to process crash")
	}
}

func TestExecute_ElapsedTime(t *testing.T) {
	e := NewExecutorWithTimeout(5 * time.Second)

	req := ExecutionRequest{
		SessionID:  "test-session-elapsed",
		BinaryPath: "/bin/echo",
		Content:    "timing test",
	}

	ch, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	for token := range ch {
		if token.ElapsedMs < 0 {
			t.Errorf("ElapsedMs should be non-negative, got %d", token.ElapsedMs)
		}
	}
}

func TestExecute_SessionCleanup(t *testing.T) {
	e := NewExecutorWithTimeout(5 * time.Second)

	req := ExecutionRequest{
		SessionID:  "test-session-cleanup",
		BinaryPath: "/bin/echo",
		Content:    "done",
	}

	ch, err := e.Execute(context.Background(), req)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Drain the channel.
	for range ch {
	}

	// After completion, the session should be cleaned up.
	e.mu.Lock()
	_, exists := e.sessions["test-session-cleanup"]
	e.mu.Unlock()

	if exists {
		t.Fatal("session should be cleaned up after completion")
	}
}
