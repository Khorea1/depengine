package app

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

func TestMain(m *testing.M) {
	InitAdapters()
	os.Exit(m.Run())
}

func TestCommandReturnsTypedExitErrorWithoutTerminatingProcess(t *testing.T) {
	cmd := newGraphCmd()
	cmd.SetArgs([]string{"--format", "invalid"})
	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("Execute() error = %v, want ExitError code 2", err)
	}
}

func TestConfirmationAccepted(t *testing.T) {
	for _, tt := range []struct {
		name  string
		input string
		want  bool
	}{
		{name: "yes", input: "yes\n", want: true},
		{name: "uppercase y", input: " Y \n", want: true},
		{name: "no", input: "no\n", want: false},
		{name: "empty", input: "\n", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := confirmationAccepted(strings.NewReader(tt.input)); got != tt.want {
				t.Fatalf("confirmationAccepted(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestHasLatestPlaceholdersRecognizesRepositoryReleaseMethods(t *testing.T) {
	tests := []struct {
		name   string
		kind   string
		config map[string]any
		want   bool
	}{
		{name: "github implicit latest", kind: "github", config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz"}, want: true},
		{name: "github explicit latest", kind: "github", config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz", "release": "latest"}, want: true},
		{name: "github named release", kind: "github", config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz", "release": "nightly"}},
		{name: "github named branch", kind: "github", config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz", "branch": "edge"}},
		{name: "http repo asset implicit latest", kind: "http", config: map[string]any{"repo": "owner/tool", "asset": "tool.tar.gz"}, want: true},
		{name: "non repository method", kind: "native", config: map[string]any{"pkg": "tool"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &config.Schema{Tools: map[string]*config.Tool{
				"tool": {Methods: []*config.MethodCandidate{{Kind: tt.kind, Config: tt.config}}},
			}}
			if got := hasLatestPlaceholders(s); got != tt.want {
				t.Fatalf("hasLatestPlaceholders() = %v, want %v", got, tt.want)
			}
		})
	}
}
