package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version returns the application version.
// It can be overridden at build time via -ldflags "-X github.com/jasonwu/dovetail/internal/version.Version=x.y.z"
var Version = ""

// Get returns the current version, preferring ldflags override, falling back to embedded VERSION file.
func Get() string {
	if Version != "" {
		return Version
	}
	return strings.TrimSpace(versionFile)
}
