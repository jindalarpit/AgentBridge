package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/user/agentbridge/daemon/pkg/protocol"
	"pgregory.net/rapid"
)

// **Validates: Requirements 6.3**
// Property 13: Stream Sequence Monotonicity
// For any sequence of stream tokens produced during a single agent response,
// each token's sequence number SHALL be strictly greater than the previous
// token's sequence number, starting from 1.

// genLineCount generates a random number of output lines between 1 and 50.
func genLineCount(t *rapid.T) int {
	return rapid.IntRange(1, 50).Draw(t, "lineCount")
}

// genLineContent generates a random non-empty line content (no newlines).
func genLineContent(t *rapid.T, label string) string {
	// Generate printable ASCII content without newlines, special printf format chars,
	// or leading dashes (which printf interprets as option flags).
	// First character uses a safe set (no dash), remaining characters can include dash.
	safeFirstChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 _-.:,;!?()[]{}=+<>"
	length := rapid.IntRange(1, 40).Draw(t, label+"-len")
	result := make([]byte, length)
	// First character must be safe (not a dash or special char that printf misinterprets)
	firstIdx := rapid.IntRange(0, len(safeFirstChars)-1).Draw(t, fmt.Sprintf("%s-char-%d", label, 0))
	result[0] = safeFirstChars[firstIdx]
	for i := 1; i < length; i++ {
		idx := rapid.IntRange(0, len(chars)-1).Draw(t, fmt.Sprintf("%s-char-%d", label, i))
		result[i] = chars[idx]
	}
	return string(result)
}

// buildPrintfArg constructs a printf argument that produces N lines of output.
func buildPrintfArg(lines []string) string {
	// Join lines with literal \n (printf interprets \n as newline)
	return strings.Join(lines, "\\n") + "\\n"
}

// TestProperty_StreamSequenceMonotonicity verifies that for any sequence of N tokens
// produced by Execute, sequence numbers are strictly increasing starting from 1.
func TestProperty_StreamSequenceMonotonicity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lineCount := genLineCount(t)

		// Generate random line contents
		lines := make([]string, lineCount)
		for i := 0; i < lineCount; i++ {
			lines[i] = genLineContent(t, fmt.Sprintf("line-%d", i))
		}

		// Build the printf argument that produces lineCount lines
		printfArg := buildPrintfArg(lines)

		e := NewExecutorWithTimeout(10 * time.Second)
		req := ExecutionRequest{
			SessionID:  fmt.Sprintf("prop-test-mono-%d", lineCount),
			BinaryPath: "/usr/bin/printf",
			Content:    printfArg,
		}

		ch, err := e.Execute(context.Background(), req)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		var tokens []StreamToken
		for token := range ch {
			tokens = append(tokens, token)
		}

		// Filter to only content tokens (non-Done, non-Error)
		var contentTokens []StreamToken
		for _, tok := range tokens {
			if !tok.Done && tok.Error == nil {
				contentTokens = append(contentTokens, tok)
			}
		}

		if len(contentTokens) == 0 {
			t.Fatalf("expected at least one content token for %d lines", lineCount)
		}

		// Verify strictly increasing sequence numbers
		prevSeq := 0
		for i, tok := range contentTokens {
			if tok.Seq <= prevSeq {
				t.Fatalf("sequence not strictly increasing at position %d: prev=%d, current=%d", i, prevSeq, tok.Seq)
			}
			prevSeq = tok.Seq
		}

		// Verify sequence starts from 1
		if contentTokens[0].Seq != 1 {
			t.Fatalf("sequence should start from 1, got %d", contentTokens[0].Seq)
		}
	})
}

// TestProperty_StreamSequencePositionEquality verifies that for any token at
// position i (0-indexed), token.Seq == i+1 (1-indexed).
func TestProperty_StreamSequencePositionEquality(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lineCount := genLineCount(t)

		// Generate random line contents
		lines := make([]string, lineCount)
		for i := 0; i < lineCount; i++ {
			lines[i] = genLineContent(t, fmt.Sprintf("line-%d", i))
		}

		// Build the printf argument
		printfArg := buildPrintfArg(lines)

		e := NewExecutorWithTimeout(10 * time.Second)
		req := ExecutionRequest{
			SessionID:  fmt.Sprintf("prop-test-pos-%d", lineCount),
			BinaryPath: "/usr/bin/printf",
			Content:    printfArg,
		}

		ch, err := e.Execute(context.Background(), req)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		var tokens []StreamToken
		for token := range ch {
			tokens = append(tokens, token)
		}

		// Filter to only content tokens (non-Done, non-Error)
		var contentTokens []StreamToken
		for _, tok := range tokens {
			if !tok.Done && tok.Error == nil {
				contentTokens = append(contentTokens, tok)
			}
		}

		if len(contentTokens) != lineCount {
			t.Fatalf("expected %d content tokens, got %d", lineCount, len(contentTokens))
		}

		// Verify token at position i has Seq == i+1
		for i, tok := range contentTokens {
			expectedSeq := i + 1
			if tok.Seq != expectedSeq {
				t.Fatalf("token at position %d has Seq=%d, expected %d", i, tok.Seq, expectedSeq)
			}
		}
	})
}

// TestProperty_StreamSequenceNoDuplicates verifies that no two tokens in a
// single execution have the same Seq value.
func TestProperty_StreamSequenceNoDuplicates(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lineCount := genLineCount(t)

		// Generate random line contents
		lines := make([]string, lineCount)
		for i := 0; i < lineCount; i++ {
			lines[i] = genLineContent(t, fmt.Sprintf("line-%d", i))
		}

		// Build the printf argument
		printfArg := buildPrintfArg(lines)

		e := NewExecutorWithTimeout(10 * time.Second)
		req := ExecutionRequest{
			SessionID:  fmt.Sprintf("prop-test-dup-%d", lineCount),
			BinaryPath: "/usr/bin/printf",
			Content:    printfArg,
		}

		ch, err := e.Execute(context.Background(), req)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		var tokens []StreamToken
		for token := range ch {
			tokens = append(tokens, token)
		}

		// Check all tokens (including Done/Error) for duplicate Seq values
		seenSeqs := make(map[int]bool)
		for _, tok := range tokens {
			if seenSeqs[tok.Seq] {
				t.Fatalf("duplicate sequence number found: Seq=%d", tok.Seq)
			}
			seenSeqs[tok.Seq] = true
		}
	})
}

// **Validates: Requirements 6.5**
// Property 14: Stream Concatenation Integrity
// For any sequence of stream tokens produced during a single agent response,
// the ChatDone message's content field SHALL equal the concatenation of all
// token contents in sequence-number order.
//
// Since the current implementation's Done token does not carry a content field,
// we verify the equivalent property: concatenating all non-Done/non-Error tokens'
// Content (joined with newlines, since the executor reads stdout line-by-line)
// in sequence-number order SHALL equal the expected command output.

// TestProperty_StreamConcatenationIntegrity verifies that for any multi-line
// output produced by a command, the concatenation of all content tokens (ordered
// by sequence number) equals the original expected output.
func TestProperty_StreamConcatenationIntegrity(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lineCount := rapid.IntRange(1, 30).Draw(t, "lineCount")

		// Generate random line contents (no newlines within lines)
		lines := make([]string, lineCount)
		for i := 0; i < lineCount; i++ {
			lines[i] = genLineContent(t, fmt.Sprintf("concat-line-%d", i))
		}

		// Build the printf argument that produces lineCount lines
		printfArg := buildPrintfArg(lines)

		e := NewExecutorWithTimeout(10 * time.Second)
		req := ExecutionRequest{
			SessionID:  fmt.Sprintf("prop-test-concat-%d", lineCount),
			BinaryPath: "/usr/bin/printf",
			Content:    printfArg,
		}

		ch, err := e.Execute(context.Background(), req)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		var tokens []StreamToken
		for token := range ch {
			tokens = append(tokens, token)
		}

		// Filter to only content tokens (non-Done, non-Error)
		var contentTokens []StreamToken
		for _, tok := range tokens {
			if !tok.Done && tok.Error == nil {
				contentTokens = append(contentTokens, tok)
			}
		}

		if len(contentTokens) == 0 {
			t.Fatalf("expected at least one content token for %d lines", lineCount)
		}

		// Sort content tokens by sequence number to ensure correct order
		sort.Slice(contentTokens, func(i, j int) bool {
			return contentTokens[i].Seq < contentTokens[j].Seq
		})

		// Concatenate token contents with newlines (since scanner splits on newlines)
		var concatenated []string
		for _, tok := range contentTokens {
			concatenated = append(concatenated, tok.Content)
		}
		result := strings.Join(concatenated, "\n")

		// The expected output is the original lines joined with newlines
		expected := strings.Join(lines, "\n")

		if result != expected {
			t.Fatalf("stream concatenation mismatch:\n  expected: %q\n  got:      %q", expected, result)
		}
	})
}

// TestProperty_StreamConcatenationTokenCount verifies that the number of
// content tokens equals the number of lines in the command output.
func TestProperty_StreamConcatenationTokenCount(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lineCount := rapid.IntRange(1, 30).Draw(t, "lineCount")

		// Generate random line contents
		lines := make([]string, lineCount)
		for i := 0; i < lineCount; i++ {
			lines[i] = genLineContent(t, fmt.Sprintf("count-line-%d", i))
		}

		// Build the printf argument
		printfArg := buildPrintfArg(lines)

		e := NewExecutorWithTimeout(10 * time.Second)
		req := ExecutionRequest{
			SessionID:  fmt.Sprintf("prop-test-count-%d", lineCount),
			BinaryPath: "/usr/bin/printf",
			Content:    printfArg,
		}

		ch, err := e.Execute(context.Background(), req)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		var tokens []StreamToken
		for token := range ch {
			tokens = append(tokens, token)
		}

		// Filter to only content tokens (non-Done, non-Error)
		var contentTokens []StreamToken
		for _, tok := range tokens {
			if !tok.Done && tok.Error == nil {
				contentTokens = append(contentTokens, tok)
			}
		}

		// Each line of output should produce exactly one content token
		if len(contentTokens) != lineCount {
			t.Fatalf("expected %d content tokens (one per line), got %d", lineCount, len(contentTokens))
		}
	})
}

// TestProperty_StreamConcatenationPerTokenContent verifies that each individual
// content token's Content field matches the corresponding line of expected output
// when tokens are ordered by sequence number.
func TestProperty_StreamConcatenationPerTokenContent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lineCount := rapid.IntRange(1, 20).Draw(t, "lineCount")

		// Generate random line contents
		lines := make([]string, lineCount)
		for i := 0; i < lineCount; i++ {
			lines[i] = genLineContent(t, fmt.Sprintf("per-tok-line-%d", i))
		}

		// Build the printf argument
		printfArg := buildPrintfArg(lines)

		e := NewExecutorWithTimeout(10 * time.Second)
		req := ExecutionRequest{
			SessionID:  fmt.Sprintf("prop-test-pertok-%d", lineCount),
			BinaryPath: "/usr/bin/printf",
			Content:    printfArg,
		}

		ch, err := e.Execute(context.Background(), req)
		if err != nil {
			t.Fatalf("Execute failed: %v", err)
		}

		var tokens []StreamToken
		for token := range ch {
			tokens = append(tokens, token)
		}

		// Filter to only content tokens (non-Done, non-Error)
		var contentTokens []StreamToken
		for _, tok := range tokens {
			if !tok.Done && tok.Error == nil {
				contentTokens = append(contentTokens, tok)
			}
		}

		// Sort by sequence number
		sort.Slice(contentTokens, func(i, j int) bool {
			return contentTokens[i].Seq < contentTokens[j].Seq
		})

		if len(contentTokens) != lineCount {
			t.Fatalf("expected %d content tokens, got %d", lineCount, len(contentTokens))
		}

		// Each token's content should match the corresponding input line
		for i, tok := range contentTokens {
			if tok.Content != lines[i] {
				t.Fatalf("token %d content mismatch:\n  expected: %q\n  got:      %q", i, lines[i], tok.Content)
			}
		}
	})
}

// **Validates: Requirements 6.5**
// Property 14: Stream Concatenation Integrity (End-to-End via TaskHandler)
// For any sequence of stream tokens produced during a single agent response,
// the ChatDone message's content field SHALL equal the concatenation of all
// token contents in sequence-number order.
//
// This test exercises the full TaskHandler flow: it sends a chat:task message,
// collects all chat:stream and chat:done protocol messages, and verifies that
// the ChatDonePayload.Content equals the concatenation of all ChatStreamPayload.Content
// values ordered by their Seq field.

// TestProperty_StreamConcatenationIntegrity_TaskHandler verifies Property 14 end-to-end
// through the TaskHandler, ensuring the ChatDone message's content field equals the
// concatenation of all stream token contents in sequence-number order.
func TestProperty_StreamConcatenationIntegrity_TaskHandler(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lineCount := rapid.IntRange(1, 30).Draw(t, "lineCount")

		// Generate random line contents (no newlines within lines)
		lines := make([]string, lineCount)
		for i := 0; i < lineCount; i++ {
			lines[i] = genLineContent(t, fmt.Sprintf("th-concat-line-%d", i))
		}

		// Build the printf argument that produces lineCount lines
		printfArg := buildPrintfArg(lines)

		// Set up the TaskHandler with a mock sender to capture protocol messages.
		executor := NewExecutorWithTimeout(10 * time.Second)
		sender := &messageSink{}
		resolver := func(runtimeID string) (string, bool) {
			if runtimeID == "printf-runtime" {
				return "/usr/bin/printf", true
			}
			return "", false
		}
		handler := NewTaskHandler(executor, sender.send, resolver)

		// Build and send a chat:task message.
		taskPayload := protocol.ChatTaskPayload{
			SessionID: fmt.Sprintf("prop-th-concat-%d", lineCount),
			MessageID: fmt.Sprintf("msg-th-concat-%d", lineCount),
			Content:   printfArg,
			RuntimeID: "printf-runtime",
			History:   []protocol.HistoryItem{},
		}
		payloadData, err := json.Marshal(taskPayload)
		if err != nil {
			t.Fatalf("failed to marshal task payload: %v", err)
		}

		msg := protocol.Message{
			Type:    protocol.TypeChatTask,
			Payload: payloadData,
		}
		handler.HandleMessage(msg)

		// Wait for the streaming goroutine to complete.
		// Poll until we see a chat:done or chat:error message.
		var messages []protocol.Message
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			messages = sender.getMessages()
			for _, m := range messages {
				if m.Type == protocol.TypeChatDone || m.Type == protocol.TypeChatError {
					goto done
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
	done:

		messages = sender.getMessages()

		// Collect stream payloads and the done payload.
		type streamEntry struct {
			Seq     int
			Content string
		}
		var streamEntries []streamEntry
		var doneContent string
		var foundDone bool

		for _, m := range messages {
			switch m.Type {
			case protocol.TypeChatStream:
				var sp protocol.ChatStreamPayload
				if err := json.Unmarshal(m.Payload, &sp); err != nil {
					t.Fatalf("failed to unmarshal stream payload: %v", err)
				}
				streamEntries = append(streamEntries, streamEntry{Seq: sp.Seq, Content: sp.Content})
			case protocol.TypeChatDone:
				var dp protocol.ChatDonePayload
				if err := json.Unmarshal(m.Payload, &dp); err != nil {
					t.Fatalf("failed to unmarshal done payload: %v", err)
				}
				doneContent = dp.Content
				foundDone = true
			case protocol.TypeChatError:
				var ep protocol.ChatErrorPayload
				if err := json.Unmarshal(m.Payload, &ep); err != nil {
					t.Fatalf("failed to unmarshal error payload: %v", err)
				}
				t.Fatalf("unexpected error during execution: %s (code: %s)", ep.Error, ep.Code)
			}
		}

		if !foundDone {
			t.Fatalf("no chat:done message received for %d lines", lineCount)
		}

		if len(streamEntries) == 0 {
			t.Fatalf("expected at least one chat:stream message for %d lines", lineCount)
		}

		// Sort stream entries by sequence number.
		sort.Slice(streamEntries, func(i, j int) bool {
			return streamEntries[i].Seq < streamEntries[j].Seq
		})

		// Concatenate all stream token contents in sequence order (joined by newline,
		// matching the TaskHandler's implementation which uses strings.Join with "\n").
		var streamContents []string
		for _, entry := range streamEntries {
			streamContents = append(streamContents, entry.Content)
		}
		concatenated := strings.Join(streamContents, "\n")

		// PROPERTY: ChatDone content == concatenation of stream tokens in seq order
		if doneContent != concatenated {
			t.Fatalf("stream concatenation integrity violated:\n  ChatDone content: %q\n  Concatenated tokens: %q\n  Line count: %d",
				doneContent, concatenated, lineCount)
		}
	})
}

// messageSink is a thread-safe collector for protocol messages used in property tests.
type messageSink struct {
	mu       sync.Mutex
	messages []protocol.Message
}

func (s *messageSink) send(msg protocol.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msg)
	return nil
}

func (s *messageSink) getMessages() []protocol.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]protocol.Message, len(s.messages))
	copy(result, s.messages)
	return result
}
