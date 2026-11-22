package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestHelpCommandRejectsUnknownTopic(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
		next string
	}{
		{name: "unknown command", args: []string{"help", "not-a-command"}, next: "depengine --help"},
		{name: "unknown nested topic", args: []string{"help", "install", "not-a-subcommand"}, next: "depengine install --help"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCmd()
			var stdout, stderr bytes.Buffer
			cmd.SetOut(&stdout)
			cmd.SetErr(&stderr)
			cmd.SetArgs(tt.args)

			err := cmd.Execute()
			var exitErr *ExitError
			if !errors.As(err, &exitErr) || exitErr.Code != 2 {
				t.Fatalf("Execute() = %v, want usage error (2)", err)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
			if got := stderr.String(); !strings.Contains(got, "unknown help topic") || !strings.Contains(got, "Next: "+tt.next) {
				t.Errorf("stderr = %q, want unknown topic and recovery command %q", got, tt.next)
			}
		})
	}
}

func TestHelpCommandShowsKnownTopic(t *testing.T) {
	cmd := newRootCmd()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"help", "install"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() = %v, want success", err)
	}
	if !strings.Contains(stdout.String(), "depengine install") {
		t.Errorf("stdout = %q, want install help", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}
