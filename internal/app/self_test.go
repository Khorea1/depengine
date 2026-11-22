package app

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/selfupdate"
)

func TestWriteSelfUpdateText(t *testing.T) {
	var buf bytes.Buffer
	writeSelfUpdateText(&buf, selfupdate.Result{
		Repo:            selfupdate.DefaultRepo,
		Ref:             "master",
		Revision:        "0123456789abcdef0123456789abcdef01234567",
		Target:          "/home/user/.local/bin/depengine",
		CurrentRevision: "fedcba9876543210fedcba9876543210fedcba98",
		UpdateAvailable: true,
	})
	for _, want := range []string{"Self-update:", "master", "update available", "/home/user/.local/bin/depengine"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("missing %q in:\n%s", want, buf.String())
		}
	}
}

func TestSelfUpdateCheckCommandExitsOneWhenUpdateAvailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("self-update refuses Windows")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "go.mod"), []byte("module example.invalid/selfupdate-fixture\n\ngo 1.27.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "main.go"), []byte("package main\nfunc main() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "."},
		{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-qm", "initial"},
	} {
		cmd := exec.Command("git", args...) // #nosec G204 -- fixed executable with fixture-controlled arguments.
		cmd.Dir = src
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	var stdout, stderr bytes.Buffer
	cmd := newRootCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{"self", "update", "--check", "--repo", src, "--bin-dir", t.TempDir()})
	err := cmd.Execute()
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("error = %v, want exit 1 (update available)\nstdout=%s\nstderr=%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "update available") {
		t.Fatalf("stdout missing status:\n%s", stdout.String())
	}
}
