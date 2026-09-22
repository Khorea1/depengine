package main

import (
	"os"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

func TestMain(m *testing.M) {
	initAdapters()
	os.Exit(m.Run())
}

func TestHasLatestPlaceholdersRecognizesGitHubMethods(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]any
		want   bool
	}{
		{name: "implicit latest", config: map[string]any{}, want: true},
		{name: "explicit latest", config: map[string]any{"release": "latest"}, want: true},
		{name: "named release", config: map[string]any{"release": "nightly"}},
		{name: "named branch", config: map[string]any{"branch": "edge"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &config.Schema{Tools: map[string]*config.Tool{
				"tool": {Methods: []*config.MethodCandidate{{Kind: "github", Config: tt.config}}},
			}}
			if got := hasLatestPlaceholders(s); got != tt.want {
				t.Fatalf("hasLatestPlaceholders() = %v, want %v", got, tt.want)
			}
		})
	}
}
