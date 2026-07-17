package validate

import (
	"context"
	"testing"

	"depengine/pkg/run"
)

// fakeRunner returns an error for every command — simulates nothing on PATH.
type fakeRunner struct{}

func (fakeRunner) Run(_ context.Context, name string, args ...string) run.Result {
	return run.Result{
		ExitCode: 1,
		Err:      nil,
	}
}

// fakeRunnerFor returns success only for the specified binary name.
type fakeRunnerFor struct {
	available map[string]bool
}

func (f fakeRunnerFor) Run(_ context.Context, name string, args ...string) run.Result {
	if name == "which" && len(args) == 1 && f.available[args[0]] {
		return run.Result{ExitCode: 0}
	}
	return run.Result{ExitCode: 1}
}

func TestCheckEnv_NoTools(t *testing.T) {
	result := CheckEnv(context.Background(), fakeRunner{})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Checks) == 0 {
		t.Fatal("expected at least some checks")
	}
	for _, ch := range result.Checks {
		if ch.Found {
			t.Errorf("expected all checks to be false with fakeRunner, got %s=true", ch.Name)
		}
	}
}

func TestCheckEnv_AllFound(t *testing.T) {
	all := map[string]bool{}
	for _, entry := range envToolBinaries {
		all[entry.Name] = true
	}
	result := CheckEnv(context.Background(), fakeRunnerFor{available: all})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	for _, ch := range result.Checks {
		if !ch.Found {
			t.Errorf("expected %s to be found, got false", ch.Name)
		}
	}
}

func TestCheckEnv_SomeFound(t *testing.T) {
	result := CheckEnv(context.Background(), fakeRunnerFor{
		available: map[string]bool{
			"git":   true,
			"curl":  true,
			"cargo": true,
		},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Count found checks.
	found := 0
	for _, ch := range result.Checks {
		if ch.Found {
			found++
		}
	}
	if found != 3 {
		t.Errorf("expected exactly 3 found tools, got %d", found)
	}
}

func TestCheckEnv_Deduplicates(t *testing.T) {
	// Verify that duplicate binary names are only checked once.
	result := CheckEnv(context.Background(), fakeRunnerFor{
		available: map[string]bool{
			"pkg":          true, // appears in envToolBinaries for both termux and freebsd
			"apt":          true,
			"pacman":       true,
			"brew":         true,
			"dnf":          true,
			"cargo":        true,
			"git":          true,
			"emerge":       true,
			"opkg":         true,
			"zypper":       true,
			"go":           true,
			"npm":          true,
			"pip":          true,
			"apk":          true,
			"pipx":         true,
			"xbps-install": true,
			"pkg_add":      true,
			"gem":          true,
			"yarn":         true,
			"uv":           true,
		},
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	// Count unique names.
	names := make(map[string]bool)
	for _, ch := range result.Checks {
		if names[ch.Name] {
			t.Errorf("duplicate check for %s", ch.Name)
		}
		names[ch.Name] = true
	}
}

func TestCheckEnv_SortedOrder(t *testing.T) {
	result := CheckEnv(context.Background(), fakeRunner{})
	if len(result.Checks) < 2 {
		t.Skip("need at least 2 checks for order test")
	}
	for i := 1; i < len(result.Checks); i++ {
		prev := result.Checks[i-1]
		cur := result.Checks[i]
		if prev.Kind > cur.Kind || (prev.Kind == cur.Kind && prev.Name > cur.Name) {
			t.Errorf("checks not sorted at index %d: %s/%s > %s/%s",
				i, prev.Kind, prev.Name, cur.Kind, cur.Name)
		}
	}
}
