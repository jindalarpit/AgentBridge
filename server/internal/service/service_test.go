package service_test

import (
	"testing"

	"pgregory.net/rapid"
)

func TestPlaceholder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// Placeholder property test to ensure rapid dependency is retained.
		n := rapid.IntRange(0, 100).Draw(t, "n")
		if n < 0 || n > 100 {
			t.Fatal("value out of range")
		}
	})
}
