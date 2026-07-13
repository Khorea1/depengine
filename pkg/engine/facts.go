package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"depengine/pkg/log"
	"depengine/pkg/run"
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

// locateDetectScript decides where to invoke detect_os.sh, in order:
//  1. the DEPENGINE_DETECT_SCRIPT env var (explicit override)
//  2. a "scripts/detect_os.sh" alongside the engine binary itself
//     (this is how the project ships: binary + scripts/ together)
//  3. "detect_os.sh" on the PATH, for those who installed it loose
func locateDetectScript() (string, error) {
	if p := os.Getenv("DEPENGINE_DETECT_SCRIPT"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("DEPENGINE_DETECT_SCRIPT points to %q but the file does not exist", p)
	}

	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "scripts", "detect_os.sh")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	if p, err := exec.LookPath("detect_os.sh"); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("detect_os.sh not found (try setting DEPENGINE_DETECT_SCRIPT=/path/to/script)")
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
	script, err := locateDetectScript()
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	res := r.Run(ctx, script, "--json", "--no-prompt")

	var facts Facts
	if jsonErr := json.Unmarshal(res.Stdout, &facts); jsonErr != nil {
		if res.Err != nil {
			return nil, fmt.Errorf("detect_os.sh failed (exit %d): %s",
				res.ExitCode, strings.TrimSpace(string(res.Stderr)))
		}
		return nil, fmt.Errorf("detect_os.sh output is not valid JSON: %w\nraw output: %s",
			jsonErr, string(res.Stdout))
	}

	log.Default.Debug("gathered facts",
		"distro_id", facts.DistroID,
		"distro_name", facts.DistroName,
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
