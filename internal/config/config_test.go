package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name        string
		authKey     string
		stateDir    string
		wantErr     bool
		wantStateDir string
	}{
		{
			name:        "valid config with custom state dir",
			authKey:     "tskey-auth-xxx",
			stateDir:    "/custom/state",
			wantErr:     false,
			wantStateDir: "/custom/state",
		},
		{
			name:        "valid config with default state dir",
			authKey:     "tskey-auth-xxx",
			stateDir:    "",
			wantErr:     false,
			wantStateDir: DefaultStateDir,
		},
		{
			name:    "missing auth key",
			authKey: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars
			os.Unsetenv("TS_AUTHKEY")
			os.Unsetenv("TS_STATE_DIR")

			if tt.authKey != "" {
				os.Setenv("TS_AUTHKEY", tt.authKey)
			}
			if tt.stateDir != "" {
				os.Setenv("TS_STATE_DIR", tt.stateDir)
			}

			cfg, err := Load()

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if cfg.AuthKey != tt.authKey {
				t.Errorf("AuthKey = %q, want %q", cfg.AuthKey, tt.authKey)
			}

			if cfg.StateDir != tt.wantStateDir {
				t.Errorf("StateDir = %q, want %q", cfg.StateDir, tt.wantStateDir)
			}
		})
	}
}
