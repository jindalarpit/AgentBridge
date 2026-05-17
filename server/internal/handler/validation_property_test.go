package handler

import (
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// **Validates: Requirements 4.5, 4.6, 6.8**
// Property 9: Input Validation
// For any string S intended as a chat message, the server SHALL accept it if and only if
// the trimmed length is between 1 and 32,000 characters (inclusive). For any string T
// intended as a session title, the server SHALL accept it if and only if the trimmed length
// is between 1 and 100 characters (inclusive). Invalid inputs SHALL be rejected without
// modifying any state.

// genValidMessageContent generates strings whose trimmed length is between 1 and MaxMessageLength.
func genValidMessageContent(t *rapid.T) string {
	coreLen := rapid.IntRange(1, MaxMessageLength).Draw(t, "coreLen")
	core := strings.Repeat("a", coreLen)
	// Optionally add leading/trailing whitespace
	leadingSpaces := rapid.IntRange(0, 5).Draw(t, "leadingSpaces")
	trailingSpaces := rapid.IntRange(0, 5).Draw(t, "trailingSpaces")
	return strings.Repeat(" ", leadingSpaces) + core + strings.Repeat(" ", trailingSpaces)
}

// genEmptyOrWhitespaceMessage generates strings whose trimmed length is 0.
func genEmptyOrWhitespaceMessage(t *rapid.T) string {
	wsLen := rapid.IntRange(0, 50).Draw(t, "wsLen")
	ws := ""
	for i := 0; i < wsLen; i++ {
		wsChar := rapid.SampledFrom([]string{" ", "\t", "\n", "\r"}).Draw(t, fmt.Sprintf("wsChar_%d", i))
		ws += wsChar
	}
	return ws
}

// genTooLongMessage generates strings whose trimmed length exceeds MaxMessageLength.
func genTooLongMessage(t *rapid.T) string {
	excess := rapid.IntRange(1, 1000).Draw(t, "excess")
	coreLen := MaxMessageLength + excess
	core := strings.Repeat("x", coreLen)
	leadingSpaces := rapid.IntRange(0, 5).Draw(t, "leadingSpaces")
	trailingSpaces := rapid.IntRange(0, 5).Draw(t, "trailingSpaces")
	return strings.Repeat(" ", leadingSpaces) + core + strings.Repeat(" ", trailingSpaces)
}

// genValidSessionTitle generates strings whose trimmed length is between 1 and MaxTitleLength.
func genValidSessionTitle(t *rapid.T) string {
	coreLen := rapid.IntRange(1, MaxTitleLength).Draw(t, "coreLen")
	core := strings.Repeat("t", coreLen)
	leadingSpaces := rapid.IntRange(0, 5).Draw(t, "leadingSpaces")
	trailingSpaces := rapid.IntRange(0, 5).Draw(t, "trailingSpaces")
	return strings.Repeat(" ", leadingSpaces) + core + strings.Repeat(" ", trailingSpaces)
}

// genTooLongTitle generates strings whose trimmed length exceeds MaxTitleLength.
func genTooLongTitle(t *rapid.T) string {
	excess := rapid.IntRange(1, 200).Draw(t, "excess")
	coreLen := MaxTitleLength + excess
	core := strings.Repeat("t", coreLen)
	leadingSpaces := rapid.IntRange(0, 5).Draw(t, "leadingSpaces")
	trailingSpaces := rapid.IntRange(0, 5).Draw(t, "trailingSpaces")
	return strings.Repeat(" ", leadingSpaces) + core + strings.Repeat(" ", trailingSpaces)
}

// TestProperty_ValidMessageContentAccepted verifies that for any string with
// trimmed length between 1 and 32000, ValidateMessageContent returns nil.
func TestProperty_ValidMessageContentAccepted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		content := genValidMessageContent(t)
		err := ValidateMessageContent(content)
		if err != nil {
			trimmed := strings.TrimSpace(content)
			t.Fatalf("ValidateMessageContent should accept valid content (trimmed len=%d), got error: %v",
				len(trimmed), err)
		}
	})
}

// TestProperty_EmptyMessageContentRejected verifies that for any string with
// trimmed length 0, ValidateMessageContent returns an error.
func TestProperty_EmptyMessageContentRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		content := genEmptyOrWhitespaceMessage(t)
		err := ValidateMessageContent(content)
		if err == nil {
			t.Fatalf("ValidateMessageContent should reject empty/whitespace content: %q", content)
		}
		ve, ok := err.(*ValidationError)
		if !ok {
			t.Fatalf("expected *ValidationError, got %T", err)
		}
		if ve.Field != "content" {
			t.Fatalf("expected field 'content', got %q", ve.Field)
		}
	})
}

// TestProperty_TooLongMessageContentRejected verifies that for any string with
// trimmed length exceeding 32000, ValidateMessageContent returns an error.
func TestProperty_TooLongMessageContentRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		content := genTooLongMessage(t)
		err := ValidateMessageContent(content)
		if err == nil {
			trimmed := strings.TrimSpace(content)
			t.Fatalf("ValidateMessageContent should reject content exceeding max length (trimmed len=%d)",
				len(trimmed))
		}
		ve, ok := err.(*ValidationError)
		if !ok {
			t.Fatalf("expected *ValidationError, got %T", err)
		}
		if ve.Field != "content" {
			t.Fatalf("expected field 'content', got %q", ve.Field)
		}
	})
}

// TestProperty_ValidSessionTitleAccepted verifies that for any string with
// trimmed length between 1 and 100, ValidateSessionTitle returns nil.
func TestProperty_ValidSessionTitleAccepted(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		title := genValidSessionTitle(t)
		err := ValidateSessionTitle(title)
		if err != nil {
			trimmed := strings.TrimSpace(title)
			t.Fatalf("ValidateSessionTitle should accept valid title (trimmed len=%d), got error: %v",
				len(trimmed), err)
		}
	})
}

// TestProperty_EmptySessionTitleRejected verifies that for any string with
// trimmed length 0, ValidateSessionTitle returns an error.
func TestProperty_EmptySessionTitleRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		title := genEmptyOrWhitespaceMessage(t)
		err := ValidateSessionTitle(title)
		if err == nil {
			t.Fatalf("ValidateSessionTitle should reject empty/whitespace title: %q", title)
		}
		ve, ok := err.(*ValidationError)
		if !ok {
			t.Fatalf("expected *ValidationError, got %T", err)
		}
		if ve.Field != "title" {
			t.Fatalf("expected field 'title', got %q", ve.Field)
		}
	})
}

// TestProperty_TooLongSessionTitleRejected verifies that for any string with
// trimmed length exceeding 100, ValidateSessionTitle returns an error.
func TestProperty_TooLongSessionTitleRejected(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		title := genTooLongTitle(t)
		err := ValidateSessionTitle(title)
		if err == nil {
			trimmed := strings.TrimSpace(title)
			t.Fatalf("ValidateSessionTitle should reject title exceeding max length (trimmed len=%d)",
				len(trimmed))
		}
		ve, ok := err.(*ValidationError)
		if !ok {
			t.Fatalf("expected *ValidationError, got %T", err)
		}
		if ve.Field != "title" {
			t.Fatalf("expected field 'title', got %q", ve.Field)
		}
	})
}
