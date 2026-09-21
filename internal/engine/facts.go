package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/run"
)

// Facts is a 1:1 mirror of detect_os.sh's --json output. Nothing is
// derived here: this struct is exactly what the fetcher emits, no more.
// Derived notions (clan, native manager) are separate return values or
// local vars at their use sites — keeps Facts cheap to test and honest
// about what the script actually produced.
type Facts struct {
	TargetArch      string `json:"target_arch"`
	DistroID        string `json:"distro_id"`
	DistroName      string `json:"distro_name"`
	DistroVersion   string `json:"distro_version"`
	DistroIDLike    string `json:"distro_id_like"`
	TargetFamily    string `json:"target_family"` // unix | windows | unknown
	DetectionMethod string `json:"detection_method"`
	Confidence      string `json:"confidence"` // high | medium | low | manual | none
	IsWSL           bool   `json:"is_wsl"`
	IsContainer     bool   `json:"is_container"`
	IsAndroid       bool   `json:"is_android"`
	Kernel          string `json:"kernel"`
	Libc            string `json:"libc"`
	InitSystem      string `json:"init_system"`
	OS              string `json:"os"`
}

//  1. the DEPENGINE_DETECT_SCRIPT env var (explicit override)
//  2. embedded content → write to a temp file, return its path
//  3. a "scripts/detect_os.sh" alongside the engine binary itself
//     (this is how the project ships: binary + scripts/ together)
//  4. "detect_os.sh" on the PATH, for those who installed it loose
//
// The second return value, clean, is true when the returned path is a
// temp file that the caller should remove after use.
func locateDetectScript(r run.Runner) (string, bool, error) {
	// 1. DEPENGINE_DETECT_SCRIPT env var (explicit override, highest priority).
	if p := os.Getenv("DEPENGINE_DETECT_SCRIPT"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, false, nil
		}
		return "", false, fmt.Errorf("DEPENGINE_DETECT_SCRIPT points to %q but the file does not exist", p)
	}

	// 2. Embedded content (always available at compile time).
	if len(detectScriptContent) > 0 {
		f, err := os.CreateTemp("", "detect_os.sh.*")
		if err == nil {
			path := f.Name()
			if _, err := f.Write(detectScriptContent); err == nil {
				if err := f.Chmod(0o755); err == nil {
					f.Close()
					return path, true, nil
				}
			}
			f.Close()
			os.Remove(path)
		}
		// Fall through if anything goes wrong with the temp file.
	}

	// 3. scripts/detect_os.sh alongside the binary.
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "scripts", "detect_os.sh")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, false, nil
		}
	}

	// 4. detect_os.sh on PATH. Executable lookup stays behind pkg/run so
	// dry-run/test/remote runners observe the same process boundary.
	if r != nil && run.LookPath(context.Background(), r, "detect_os.sh") {
		return "detect_os.sh", false, nil
	}

	return "", false, fmt.Errorf("detect_os.sh not found (try setting DEPENGINE_DETECT_SCRIPT=/path/to/script)")
}

// gatherFactsGo builds a minimal Facts from Go runtime info.
// Used as fallback when detect_os.sh cannot execute (e.g. on Windows).
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

// GatherFacts runs the fetcher via the injected Runner and returns the
// decoded Facts. It no longer computes the clan here — that's a pure
// function (ResolveFamily) the caller invokes once and reuses, which also
// keeps GatherFacts trivially testable against a fake Runner.
//
// detect_os.sh uses exit code 1 for "partial detection" (low confidence)
// and that's NOT an execution failure — the JSON is still valid. We only
// fail when we cannot parse the stdout; then we prefer the script's own
// stderr as the actionable message.
func GatherFacts(r run.Runner) (*Facts, error) {
	if !run.ExecutionAllowed(r) {
		// In observational/dry-run mode, do not materialize the embedded shell
		// detector or invoke the runner through platform-version fallbacks. A
		// blocked execution policy means zero runner calls, not merely calls that
		// are expected to reject execution.
		return gatherFactsGo(nil), nil
	}
	script, clean, err := locateDetectScript(r)
	if err != nil {
		log.Default.Warn("OS detection script not available, using Go runtime fallback", "error", err)
		return gatherFactsGo(r), nil
	}
	if clean {
		defer os.Remove(script)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := r.Run(ctx, script, "--json", "--no-prompt")

	var facts Facts
	if jsonErr := json.Unmarshal(res.Stdout, &facts); jsonErr != nil {
		if len(res.Stdout) == 0 && res.Err != nil {
			// Script couldn't start (no shell, .sh not executable on Windows, etc.)
			log.Default.Warn("OS detection script failed, using Go runtime fallback",
				"error", res.Err, "stderr", string(res.Stderr))
			return gatherFactsGo(r), nil
		}
		if res.Err != nil {
			return nil, fmt.Errorf("detect_os.sh failed (exit %d): %s",
				res.ExitCode, strings.TrimSpace(string(res.Stderr)))
		}
		return nil, fmt.Errorf("detect_os.sh output is not valid JSON: %w\nraw output: %s",
			jsonErr, string(res.Stdout))
	}

	// A process-level execution failure (timeout, cancellation, signal, spawn
	// failure) means the detector did not complete successfully even if it
	// happened to emit syntactically valid JSON before dying. Normal exit code
	// 1 remains accepted above because detect_os.sh deliberately uses it for
	// partial-but-complete detection and Runner reports that via ExitCode only.
	if res.Err != nil {
		log.Default.Warn("OS detection script did not complete, using Go runtime fallback",
			"error", res.Err, "stderr", string(res.Stderr))
		return gatherFactsGo(r), nil
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

	return &facts, nil
}
