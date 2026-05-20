package auth

import (
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// Feature: daemon-browser-login, Property 3: Token Name Formatting
// **Validates: Requirements 2.1**

// TestProperty_TokenNameStartsWithPrefix verifies that for any hostname string,
// the generated token name always starts with "Daemon (".
func TestProperty_TokenNameStartsWithPrefix(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hostname := rapid.String().Draw(t, "hostname")

		name := FormatTokenName(hostname)

		if !strings.HasPrefix(name, "Daemon (") {
			t.Fatalf("FormatTokenName(%q) = %q, does not start with \"Daemon (\"", hostname, name)
		}
	})
}

// TestProperty_TokenNameEndsWithSuffix verifies that for any hostname string,
// the generated token name always ends with ")".
func TestProperty_TokenNameEndsWithSuffix(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hostname := rapid.String().Draw(t, "hostname")

		name := FormatTokenName(hostname)

		if !strings.HasSuffix(name, ")") {
			t.Fatalf("FormatTokenName(%q) = %q, does not end with \")\"", hostname, name)
		}
	})
}

// TestProperty_TokenNameNeverExceeds100Chars verifies that for any hostname string,
// the total token name length never exceeds 100 characters.
func TestProperty_TokenNameNeverExceeds100Chars(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hostname := rapid.String().Draw(t, "hostname")

		name := FormatTokenName(hostname)

		if len(name) > 100 {
			t.Fatalf("FormatTokenName(%q) = %q (len=%d), exceeds 100 characters",
				hostname, name, len(name))
		}
	})
}

// TestProperty_TokenNameHostnameTruncatedTo91 verifies that for any hostname string,
// the hostname portion within the token name is at most 91 characters
// (to keep total length within 100: 8 prefix + 91 hostname + 1 suffix = 100).
func TestProperty_TokenNameHostnameTruncatedTo91(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		hostname := rapid.String().Draw(t, "hostname")

		name := FormatTokenName(hostname)

		// Extract hostname portion: strip "Daemon (" prefix and ")" suffix
		hostPortion := name[len("Daemon (") : len(name)-1]

		if len(hostPortion) > 91 {
			t.Fatalf("FormatTokenName(%q): hostname portion %q (len=%d) exceeds 91 characters",
				hostname, hostPortion, len(hostPortion))
		}
	})
}
