// Package run is the single seam through which the engine executes any
// subprocess. This file provides elevation-aware command construction.
package run

import (
	"context"
	"os"
	"os/exec"
	"sync"
)

// Elevator owns privilege-elevation detection state: the probed method,
// whether probing happened, and any test override. It replaces the former
// package globals so the state can live in an instance (injected by callers
// in the future) instead of process-wide mutable variables. The
// package-level functions below delegate to Default, preserving behavior
// for existing callers.
//
// An Elevator must not be copied after first use.
type Elevator struct {
	mu        sync.Mutex
	method    string // "" = unprobed/not-found, "sudo"|"doas"|"pkexec"|"run0" = detected
	probed    bool   // true once detectElevation has been called
	overridden bool   // true when Override forced a method, bypassing euid/probing
}

// NewElevator returns an unprobed Elevator.
func NewElevator() *Elevator {
	return &Elevator{}
}

// Default is the process-wide Elevator backing the package-level functions.
// New code that needs isolation should construct its own Elevator.
var Default = NewElevator()

// RunElevated runs one argv command through the selected elevation method.
// It never invokes a shell.
func (e *Elevator) RunElevated(ctx context.Context, rn Runner, name string, args ...string) Result {
	prefix := e.Prefix()
	if len(prefix) == 0 {
		return rn.Run(ctx, name, args...)
	}
	argv := append(append([]string(nil), prefix[1:]...), name)
	argv = append(argv, args...)
	return rn.Run(ctx, prefix[0], argv...)
}

// RunElevated runs one argv command through the selected elevation method.
// It never invokes a shell.
func RunElevated(ctx context.Context, rn Runner, name string, args ...string) Result {
	return Default.RunElevated(ctx, rn, name, args...)
}

// elevationCandidates is the ordered list of elevation binaries to probe.
// Each entry is checked in order; the first that works is cached.
//
// Probing here means "is this binary present and plausibly usable", never
// "run the privileged command for real". pkexec and run0 have no dry-run
// mode — invoking them for real (even with `true`) triggers an actual
// polkit authentication (a real prompt/dialog) as a side effect of a mere
// availability check, which previously fired from Adapter.Available()
// paths before the user had asked to install anything. Only sudo/doas
// support a true side-effect-free probe (`-n`, which fails closed instead
// of prompting), so they alone use it; pkexec/run0 fall back to a plain
// LookPath and are validated for real only when actually invoked to elevate.
//
//  1. sudo:      works when interactive (TTY session, see EnsureSudo/
//     KeepAlive in session.go) or when NOPASSWD is configured
//  2. doas:      simpler alternative, typically configured passwordless
//  3. pkexec:    PolKit-based, no TTY required, kept last: no cache, every
//     call re-prompts
//  4. run0:      systemd 256+, PolKit-based, also no cache
var elevationCandidates = []struct {
	name    string
	probeFn func() bool
}{
	{"sudo", func() bool {
		_, err := exec.LookPath("sudo")
		return err == nil
	}},
	{"doas", func() bool {
		_, err := exec.LookPath("doas")
		return err == nil
	}},
	{"pkexec", func() bool {
		_, err := exec.LookPath("pkexec")
		return err == nil
	}},
	{"run0", func() bool {
		_, err := exec.LookPath("run0")
		return err == nil
	}},
}

// detectElevation probes for a working elevation method.
// Iterates elevationCandidates in priority order; returns the first that works.
// Returns "" if no working elevation method is available.
func detectElevation() string {
	if os.Geteuid() == 0 {
		return "sudo" // already root, sudo works trivially
	}
	for _, c := range elevationCandidates {
		if c.probeFn() {
			return c.name
		}
	}
	return ""
}

// Method returns the detected elevation method, probing once
// and caching the result on the Elevator.
func (e *Elevator) Method() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.probed {
		e.method = detectElevation()
		e.probed = true
	}
	return e.method
}

// ElevationMethod returns the detected elevation method, probing once
// and caching the result for the lifetime of the process.
func ElevationMethod() string {
	return Default.Method()
}

// Prefix returns a command prefix to elevate privileges.
// Returns ["sudo"], ["doas"], ["pkexec"], or ["run0"] when elevation is
// available and needed, or nil when already root or no method works.
//
// A method forced via Override always wins: tests that simulate a
// non-root environment must get the forced prefix back even when the test
// process itself happens to run as root (e.g. inside a container).
func (e *Elevator) Prefix() []string {
	e.mu.Lock()
	forced := e.overridden
	e.mu.Unlock()

	if !forced && os.Geteuid() == 0 {
		return nil
	}
	method := e.Method()
	if method == "" {
		return nil
	}
	return []string{method}
}

// ElevationPrefix returns a command prefix to elevate privileges.
// Returns ["sudo"], ["doas"], ["pkexec"], or ["run0"] when elevation is
// available and needed, or nil when already root or no method works.
//
// A method forced via OverrideElevation always wins: tests that simulate a
// non-root environment must get the forced prefix back even when the test
// process itself happens to run as root (e.g. inside a container).
func ElevationPrefix() []string {
	return Default.Prefix()
}

// IsElevationPrefix reports whether name is a known privilege-elevation command.
// Used by callers that need to skip the elevation prefix when parsing commands
// (e.g. replaceManagerBinary in the native adapter).
func IsElevationPrefix(name string) bool {
	return name == "sudo" || name == "doas" || name == "pkexec" || name == "run0"
}

// Override forces a specific elevation method on the Elevator.
// Pass "sudo", "doas", "pkexec", or "run0" to simulate a particular environment.
// Pass "" to restore auto-detection (re-detects on next call).
func (e *Elevator) Override(method string) {
	e.mu.Lock()
	e.method = method
	e.probed = method != "" // "" means "re-probe on next call"
	e.overridden = method != ""
	e.mu.Unlock()
}

// OverrideElevation forces a specific elevation method for testing.
// Pass "sudo", "doas", "pkexec", or "run0" to simulate a particular environment.
// Pass "" to restore auto-detection (re-detects on next call).
func OverrideElevation(method string) {
	Default.Override(method)
}
