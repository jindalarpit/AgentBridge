package handler

import (
	"fmt"
	"strings"
)

// Validation constants defining input length constraints.
const (
	MaxMessageLength  = 32000
	MaxTitleLength    = 100
	MinPasswordLength = 6
)

// ValidationError represents a field-level validation error.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidateMessageContent validates chat message content.
// It trims whitespace and checks that the trimmed length is between 1 and MaxMessageLength characters.
func ValidateMessageContent(content string) error {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) == 0 {
		return &ValidationError{
			Field:   "content",
			Message: "message content must not be empty",
		}
	}
	if len(trimmed) > MaxMessageLength {
		return &ValidationError{
			Field:   "content",
			Message: fmt.Sprintf("message content must not exceed %d characters", MaxMessageLength),
		}
	}
	return nil
}

// ValidateSessionTitle validates a chat session title.
// It trims whitespace and checks that the trimmed length is between 1 and MaxTitleLength characters.
func ValidateSessionTitle(title string) error {
	trimmed := strings.TrimSpace(title)
	if len(trimmed) == 0 {
		return &ValidationError{
			Field:   "title",
			Message: "session title must not be empty",
		}
	}
	if len(trimmed) > MaxTitleLength {
		return &ValidationError{
			Field:   "title",
			Message: fmt.Sprintf("session title must not exceed %d characters", MaxTitleLength),
		}
	}
	return nil
}

// ValidateEmail performs a basic email format check.
// It verifies the email contains an @ symbol with a non-empty local part and a domain containing a dot.
func ValidateEmail(email string) error {
	trimmed := strings.TrimSpace(email)
	if len(trimmed) == 0 {
		return &ValidationError{
			Field:   "email",
			Message: "email must not be empty",
		}
	}

	atIdx := strings.LastIndex(trimmed, "@")
	if atIdx < 1 {
		return &ValidationError{
			Field:   "email",
			Message: "email must contain a valid @ symbol with a local part",
		}
	}

	domain := trimmed[atIdx+1:]
	if len(domain) == 0 || !strings.Contains(domain, ".") {
		return &ValidationError{
			Field:   "email",
			Message: "email must have a valid domain",
		}
	}

	return nil
}

// ValidatePassword checks that a password meets minimum length requirements.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return &ValidationError{
			Field:   "password",
			Message: fmt.Sprintf("password must be at least %d characters", MinPasswordLength),
		}
	}
	return nil
}
