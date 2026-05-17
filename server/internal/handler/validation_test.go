package handler

import (
	"strings"
	"testing"
)

func TestValidateMessageContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
		field   string
	}{
		{
			name:    "valid message",
			content: "Hello, world!",
			wantErr: false,
		},
		{
			name:    "valid single character",
			content: "a",
			wantErr: false,
		},
		{
			name:    "valid at max length",
			content: strings.Repeat("a", MaxMessageLength),
			wantErr: false,
		},
		{
			name:    "valid with leading/trailing whitespace",
			content: "  hello  ",
			wantErr: false,
		},
		{
			name:    "invalid empty string",
			content: "",
			wantErr: true,
			field:   "content",
		},
		{
			name:    "invalid whitespace only",
			content: "   \t\n  ",
			wantErr: true,
			field:   "content",
		},
		{
			name:    "invalid exceeds max length",
			content: strings.Repeat("a", MaxMessageLength+1),
			wantErr: true,
			field:   "content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMessageContent(tt.content)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				ve, ok := err.(*ValidationError)
				if !ok {
					t.Fatalf("expected *ValidationError, got %T", err)
				}
				if ve.Field != tt.field {
					t.Errorf("expected field %q, got %q", tt.field, ve.Field)
				}
				if ve.Message == "" {
					t.Error("expected non-empty error message")
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestValidateSessionTitle(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		wantErr bool
		field   string
	}{
		{
			name:    "valid title",
			title:   "My Chat Session",
			wantErr: false,
		},
		{
			name:    "valid single character",
			title:   "X",
			wantErr: false,
		},
		{
			name:    "valid at max length",
			title:   strings.Repeat("t", MaxTitleLength),
			wantErr: false,
		},
		{
			name:    "valid with surrounding whitespace",
			title:   "  My Title  ",
			wantErr: false,
		},
		{
			name:    "invalid empty string",
			title:   "",
			wantErr: true,
			field:   "title",
		},
		{
			name:    "invalid whitespace only",
			title:   "   \t\n  ",
			wantErr: true,
			field:   "title",
		},
		{
			name:    "invalid exceeds max length",
			title:   strings.Repeat("t", MaxTitleLength+1),
			wantErr: true,
			field:   "title",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionTitle(tt.title)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				ve, ok := err.(*ValidationError)
				if !ok {
					t.Fatalf("expected *ValidationError, got %T", err)
				}
				if ve.Field != tt.field {
					t.Errorf("expected field %q, got %q", tt.field, ve.Field)
				}
				if ve.Message == "" {
					t.Error("expected non-empty error message")
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestValidateEmail(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr bool
		field   string
	}{
		{
			name:    "valid email",
			email:   "user@example.com",
			wantErr: false,
		},
		{
			name:    "valid email with subdomain",
			email:   "user@mail.example.com",
			wantErr: false,
		},
		{
			name:    "valid email with plus",
			email:   "user+tag@example.com",
			wantErr: false,
		},
		{
			name:    "invalid empty",
			email:   "",
			wantErr: true,
			field:   "email",
		},
		{
			name:    "invalid no at symbol",
			email:   "userexample.com",
			wantErr: true,
			field:   "email",
		},
		{
			name:    "invalid no local part",
			email:   "@example.com",
			wantErr: true,
			field:   "email",
		},
		{
			name:    "invalid no domain",
			email:   "user@",
			wantErr: true,
			field:   "email",
		},
		{
			name:    "invalid domain without dot",
			email:   "user@localhost",
			wantErr: true,
			field:   "email",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEmail(tt.email)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				ve, ok := err.(*ValidationError)
				if !ok {
					t.Fatalf("expected *ValidationError, got %T", err)
				}
				if ve.Field != tt.field {
					t.Errorf("expected field %q, got %q", tt.field, ve.Field)
				}
				if ve.Message == "" {
					t.Error("expected non-empty error message")
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
		field    string
	}{
		{
			name:     "valid password",
			password: "secret123",
			wantErr:  false,
		},
		{
			name:     "valid at minimum length",
			password: strings.Repeat("p", MinPasswordLength),
			wantErr:  false,
		},
		{
			name:     "valid long password",
			password: strings.Repeat("x", 100),
			wantErr:  false,
		},
		{
			name:     "invalid too short",
			password: "abc",
			wantErr:  true,
			field:    "password",
		},
		{
			name:     "invalid empty",
			password: "",
			wantErr:  true,
			field:    "password",
		},
		{
			name:     "invalid one below minimum",
			password: strings.Repeat("p", MinPasswordLength-1),
			wantErr:  true,
			field:    "password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				ve, ok := err.(*ValidationError)
				if !ok {
					t.Fatalf("expected *ValidationError, got %T", err)
				}
				if ve.Field != tt.field {
					t.Errorf("expected field %q, got %q", tt.field, ve.Field)
				}
				if ve.Message == "" {
					t.Error("expected non-empty error message")
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestValidationError_Error(t *testing.T) {
	ve := &ValidationError{Field: "content", Message: "must not be empty"}
	expected := "content: must not be empty"
	if ve.Error() != expected {
		t.Errorf("expected %q, got %q", expected, ve.Error())
	}
}
