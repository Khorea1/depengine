// Package run is the single seam through which the engine executes any
// subprocess. This file provides elevation-aware command construction.
package run

import (
	"os"
	"os/exec"
	"sync"
)

var (
	elevationMu     sync.Mutex
	elevationMethod string // "" = unprobed/not-found, "sudo"|"doas"|"pkexec"|"run0" = detected
	elevationProbed bool   // true once detectElevation has been called
)

// elevationCandidates is the ordered list of elevation binaries to probe.
// Each entry is checked in order; the first that works is cached.
//  1. sudo -n:   works when passwordless sudo is configured
//  2. doas:      simpler alternative, typically configured passwordless
//  3. pkexec:    PolKit-based, no TTY required
//  4. run0:      systemd 256+, PolKit-based via machinectl
var elevationCandidates = []struct {
	name    string
	probeFn func() bool
}{
	{"sudo", func() bool {
		if _, err := exec.LookPath("sudo"); err != nil {
			return false
		}
		// sudo -n runs non-interactively; exits 0 only when no password is needed.
		return exec.Command("sudo", "-n", "true").Run() == nil
	}},
	{"doas", func() bool {
		if _, err := exec.LookPath("doas"); err != nil {
			return false
		}
		return exec.Command("doas", "-n", "true").Run() == nil
	}},
	{"pkexec", func() bool {
		if _, err := exec.LookPath("pkexec"); err != nil {
			return false
		}
		return exec.Command("pkexec", "true").Run() == nil
	}},
	{"run0", func() bool {
		if _, err := exec.LookPath("run0"); err != nil {
			return false
		}
		return exec.Command("run0", "true").Run() == nil
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

// ElevationMethod returns the detected elevation method, probing once
// and caching the result for the lifetime of the process.
func ElevationMethod() string {
	elevationMu.Lock()
	defer elevationMu.Unlock()
	if !elevationProbed {
		elevationMethod = detectElevation()
		elevationProbed = true
	}
	return elevationMethod
}

// ElevationPrefix returns a command prefix to elevate privileges.
// Returns ["sudo"], ["doas"], ["pkexec"], or ["run0"] when elevation is
// available and needed, or nil when already root or no method works.
func ElevationPrefix() []string {
	if os.Geteuid() == 0 {
		return nil
	}
	method := ElevationMethod()
	if method == "" {
		return nil
	}
	return []string{method}
}

// IsElevationPrefix reports whether name is a known privilege-elevation command.
// Used by callers that need to skip the elevation prefix when parsing commands
// (e.g. replaceManagerBinary in the native adapter).
func IsElevationPrefix(name string) bool {
	return name == "sudo" || name == "doas" || name == "pkexec" || name == "run0"
}

// OverrideElevation forces a specific elevation method for testing.
// Pass "sudo", "doas", "pkexec", or "run0" to simulate a particular environment.
// Pass "" to restore auto-detection (re-detects on next call).
func OverrideElevation(method string) {
	elevationMu.Lock()
	elevationMethod = method
	elevationProbed = method != "" // "" means "re-probe on next call"
	elevationMu.Unlock()
}
