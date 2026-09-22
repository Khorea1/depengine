package app

import (
	"errors"
	"testing"

	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/log"
)

func TestResolveInstallManifestPath(t *testing.T) {
	cases := []struct {
		name       string
		noManifest bool
		flag       string
		def        string
		wantPath   string
		wantAuto   bool
	}{
		{"no-manifest ignores default", true, "", "/x/manifest.toml", "", false},
		{"explicit flag wins over default", false, "/custom/manifest.toml", "/x/manifest.toml", "/custom/manifest.toml", false},
		{"empty default means no manifest", false, "", "", "", false},
		{"default auto-detected", false, "", "/x/manifest.toml", "/x/manifest.toml", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotPath, gotAuto := resolveInstallManifestPath(tc.noManifest, tc.flag, tc.def)
			if gotPath != tc.wantPath || gotAuto != tc.wantAuto {
				t.Fatalf("got (%q,%v) want (%q,%v)", gotPath, gotAuto, tc.wantPath, tc.wantAuto)
			}
		})
	}
}

func TestValidateInstallSortBy(t *testing.T) {
	lg := log.Default
	if err := validateInstallSortBy("", lg); err != nil {
		t.Fatalf("empty sort must pass, got %v", err)
	}
	for _, valid := range []string{"name", "status", "method"} {
		if err := validateInstallSortBy(valid, lg); err != nil {
			t.Fatalf("sort %q must pass, got %v", valid, err)
		}
	}
	err := validateInstallSortBy("bogus", lg)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("invalid sort must return ExitError, got %T (%v)", err, err)
	}
	if exitErr.Code != 2 {
		t.Fatalf("invalid sort exit code = %d, want 2", exitErr.Code)
	}
}

func TestInstallOutputMode(t *testing.T) {
	cases := []struct {
		name       string
		json, q, v bool
		want       string
	}{
		{"json wins over all", true, true, true, "json"},
		{"quiet selects detail", false, true, false, "detail"},
		{"verbose selects detail", false, false, true, "detail"},
		{"plain selects summary", false, false, false, "summary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := installOutputMode(tc.json, tc.q, tc.v); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestInstallExitForReport(t *testing.T) {
	if err := installExitForReport(&exec.ExecReport{Success: 3}); err != nil {
		t.Fatalf("clean report must exit nil, got %v", err)
	}
	err := installExitForReport(&exec.ExecReport{Success: 1, Failed: 2})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("failed report must return ExitError, got %T (%v)", err, err)
	}
	if exitErr.Code != 1 {
		t.Fatalf("failed report exit code = %d, want 1", exitErr.Code)
	}
}

func TestShouldShareInstallHint(t *testing.T) {
	if !shouldShareInstallHint(&exec.ExecReport{Success: 1}, false) {
		t.Fatal("successful real install should hint")
	}
	if shouldShareInstallHint(&exec.ExecReport{Success: 1}, true) {
		t.Fatal("dry run must not hint")
	}
	if shouldShareInstallHint(&exec.ExecReport{Success: 1, Failed: 1}, false) {
		t.Fatal("failed install must not hint")
	}
	if shouldShareInstallHint(&exec.ExecReport{}, false) {
		t.Fatal("zero successes must not hint")
	}
}
