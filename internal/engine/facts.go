package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

// Facts is retained as an engine-level compatibility alias while host-fact
// ownership moves to internal/platform. Consumers can migrate to platform.Facts
// independently of the detector implementation change.
type Facts = platform.Facts

// legacyDetectorPath returns the explicitly configured legacy detector.
// Native Go detection is the default; the environment override remains
// temporarily supported so operators relying on a custom detector are not
// broken by the runtime migration.
func legacyDetectorPath() (string, error) {
	p := os.Getenv("DEPENGINE_DETECT_SCRIPT")
	if p == "" {
		return "", nil
	}
	//nolint:gosec // DEPENGINE_DETECT_SCRIPT intentionally trusts an operator-supplied local path.
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("DEPENGINE_DETECT_SCRIPT points to %q but the file does not exist", p)
	}
	return p, nil
}

// gatherFactsGo builds minimal runtime-only Facts for blocked execution and
// as a fallback when an explicitly configured legacy detector cannot run.
func gatherFactsGo(r run.Runner) *Facts {
	tf := "unknown"
	switch runtime.GOOS {
	case "windows":
		tf = "windows"
	case "linux", "darwin":
		tf = "unix"
	}
	facts := &Facts{
		OS:              runtime.GOOS,
		TargetFamily:    tf,
		TargetArch:      runtime.GOARCH,
		Kernel:          "unknown",
		DetectionMethod: "go-builtin",
		Confidence:      "low",
	}

	switch runtime.GOOS {
	case "windows":
		facts.DistroID = "windows"
		facts.DistroName = "Windows"
		if version := detectWindowsVersion(r); version != "" {
			facts.DistroVersion = version
			facts.DistroName = "Windows " + version
			facts.DetectionMethod = "go-builtin+cmd-ver"
			facts.Confidence = "medium"
		}
	case "darwin":
		facts.DistroID = "macos"
		facts.DistroName = "macOS"
		if version := detectDarwinVersion(r); version != "" {
			facts.DistroVersion = version
			facts.DistroName = "macOS " + version
			facts.DetectionMethod = "go-builtin+sw-vers"
			facts.Confidence = "medium"
		}
	}

	return facts
}

func detectDarwinVersion(r run.Runner) string {
	if r == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res := r.Run(ctx, "sw_vers", "-productVersion")
	if res.Err != nil || res.ExitCode != 0 {
		return ""
	}
	return strings.TrimSpace(string(res.Stdout))
}

func detectWindowsVersion(r run.Runner) string {
	if r == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res := r.Run(ctx, "cmd.exe", "/d", "/c", "ver")
	if res.Err != nil || res.ExitCode != 0 {
		return ""
	}
	return parseWindowsVersion(string(res.Stdout))
}

// parseWindowsVersion extracts the numeric NT version/build tuple from the
// output of cmd.exe's built-in `ver` command. The surrounding text is
// localized on some Windows installations, so only the dotted numeric token
// is relied upon (for example 10.0.26100.4652).
func parseWindowsVersion(output string) string {
	for i := 0; i < len(output); i++ {
		if output[i] < '0' || output[i] > '9' {
			continue
		}
		start := i
		dots := 0
		for i < len(output) {
			c := output[i]
			if c >= '0' && c <= '9' {
				i++
				continue
			}
			if c == '.' {
				dots++
				i++
				continue
			}
			break
		}
		candidate := strings.TrimSuffix(output[start:i], ".")
		if dots >= 2 && validWindowsVersion(candidate) {
			return candidate
		}
	}
	return ""
}

func validWindowsVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for i := 0; i < len(part); i++ {
			if part[i] < '0' || part[i] > '9' {
				return false
			}
		}
	}
	return true
}

// GatherFacts returns host facts through the native Go detector. An explicitly
// configured DEPENGINE_DETECT_SCRIPT still uses the legacy JSON contract during
// the migration window; no bundled or PATH-discovered script is selected here.
func GatherFacts(r run.Runner) (*Facts, error) {
	if !run.ExecutionAllowed(r) {
		// Preserve the observational/dry-run contract: zero runner calls and
		// only runtime facts when subprocess execution is intentionally blocked.
		return gatherFactsGo(nil), nil
	}

	script, legacyErr := legacyDetectorPath()
	if legacyErr != nil {
		log.Default.Warn("legacy OS detector override unavailable, using native Go detection", "error", legacyErr)
	} else if script != "" {
		facts, err := gatherFactsFromLegacyDetector(r, script)
		if err != nil {
			return nil, err
		}
		logFacts(facts)
		return facts, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	facts := platform.Detect(ctx, r)
	logFacts(facts)
	return facts, nil
}

func gatherFactsFromLegacyDetector(r run.Runner, script string) (*Facts, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := r.Run(ctx, script, "--json", "--no-prompt")

	var facts Facts
	if jsonErr := json.Unmarshal(res.Stdout, &facts); jsonErr != nil {
		if len(res.Stdout) == 0 && res.Err != nil {
			log.Default.Warn("legacy OS detection script failed, using Go runtime fallback",
				"error", res.Err, "stderr", string(res.Stderr))
			return gatherFactsGo(r), nil
		}
		if res.Err != nil {
			return nil, fmt.Errorf("legacy detector failed (exit %d): %s",
				res.ExitCode, strings.TrimSpace(string(res.Stderr)))
		}
		return nil, fmt.Errorf("legacy detector output is not valid JSON: %w\nraw output: %s",
			jsonErr, string(res.Stdout))
	}

	if res.Err != nil {
		log.Default.Warn("legacy OS detection script did not complete, using Go runtime fallback",
			"error", res.Err, "stderr", string(res.Stderr))
		return gatherFactsGo(r), nil
	}

	return &facts, nil
}

func logFacts(facts *Facts) {
	if facts == nil {
		return
	}
	log.Default.Debug("gathered facts",
		"distro_id", facts.DistroID,
		"distro_name", facts.DistroName,
		"distro_version", facts.DistroVersion,
		"arch", facts.TargetArch,
		"kernel", facts.Kernel,
		"libc", facts.Libc,
		"init", facts.InitSystem,
		"os", facts.OS,
		"is_wsl", facts.IsWSL,
		"is_container", facts.IsContainer,
		"is_android", facts.IsAndroid,
		"family", facts.TargetFamily,
		"confidence", facts.Confidence,
	)
}
