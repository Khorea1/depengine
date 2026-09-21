package httpdownload

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultSudoRequired(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("home dir unavailable")
	}
	tests := []struct {
		name string
		dest string
		want bool
	}{
		{"user fonts dir", filepath.Join(home, ".local", "share", "fonts"), false},
		{"user bin", filepath.Join(home, "bin"), false},
		{"home itself", home, false},
		{"system default", "/usr/local/bin", true},
		{"opt", "/opt/tool", true},
		{"tmp not under home", "/tmp/x", true},
		{"dot-dot escape", filepath.Join(home, "..", "..", "usr", "local", "bin"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := defaultSudoRequired(tt.dest); got != tt.want {
				t.Errorf("defaultSudoRequired(%q) = %v, want %v", tt.dest, got, tt.want)
			}
		})
	}
}
