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
