package config

import (
	"fmt"
	"os"
)

const (
	DefaultStateDir = "/var/lib/dovetail"
)

type Config struct {
	AuthKey  string
	StateDir string
}

func Load() (*Config, error) {
	authKey := os.Getenv("TS_AUTHKEY")
	if authKey == "" {
		return nil, fmt.Errorf("TS_AUTHKEY environment variable is required")
	}

	stateDir := os.Getenv("TS_STATE_DIR")
	if stateDir == "" {
		stateDir = DefaultStateDir
	}

	return &Config{
		AuthKey:  authKey,
		StateDir: stateDir,
	}, nil
}
