package version

import (
	"strings"
	"testing"
)

func TestGet(t *testing.T) {
	// Save original value
	originalVersion := Version
	defer func() { Version = originalVersion }()

	t.Run("returns ldflags version when set", func(t *testing.T) {
		Version = "1.2.3"

		got := Get()
		if got != "1.2.3" {
			t.Errorf("Get() = %q, want %q", got, "1.2.3")
		}
	})

	t.Run("returns embedded version when ldflags empty", func(t *testing.T) {
		Version = ""

		got := Get()
		// Should return the embedded version from VERSION file
		if got == "" {
			t.Error("Get() returned empty string, expected embedded version")
		}
		// The embedded version should be trimmed (no whitespace)
		if strings.TrimSpace(got) != got {
			t.Errorf("Get() = %q contains whitespace, expected trimmed version", got)
		}
	})

	t.Run("ldflags version takes precedence over embedded", func(t *testing.T) {
		Version = "override-version"

		got := Get()
		if got != "override-version" {
			t.Errorf("Get() = %q, want %q (ldflags should take precedence)", got, "override-version")
		}
	})
}

func TestGet_Trimmed(t *testing.T) {
	// Save original value
	originalVersion := Version
	defer func() { Version = originalVersion }()

	// When using embedded version (Version = ""), the result should be trimmed
	Version = ""

	got := Get()

	// Verify no leading/trailing whitespace
	if got != strings.TrimSpace(got) {
		t.Errorf("Get() returned untrimmed version: %q", got)
	}
}
