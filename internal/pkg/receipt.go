package pkg

import (
	"context"
	"strconv"
	"strings"

	"github.com/Khorea1/depengine/internal/run"
)

// queryReceipt probes the macOS package receipt database for package_id.
// It returns presence and, when pkgutil exposes one, the installed version
// parsed from the receipt output.
func queryReceipt(ctx context.Context, rn run.Runner, packageID string) (present bool, version string, err error) {
	res := rn.Run(ctx, "pkgutil", "--pkg-info", packageID)
	if res.Err != nil {
		return false, "", res.Err
	}
	switch res.ExitCode {
	case 0:
		if res.WaitErr != nil {
			return false, "", res.WaitErr
		}
	case 1:
		// pkgutil exits 1 when the package has no receipt.
		return false, "", nil
	default:
		return false, "", &exitStatusError{code: res.ExitCode, stderr: strings.TrimSpace(string(res.Stderr))}
	}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "version:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "version:"))
		if value != "" && value != "-" {
			version = value
		}
		break
	}
	return true, version, nil
}

type exitStatusError struct {
	code   int
	stderr string
}

func (e *exitStatusError) Error() string {
	if e.stderr != "" {
		return "pkgutil exited " + strconv.Itoa(e.code) + ": " + e.stderr
	}
	return "pkgutil exited " + strconv.Itoa(e.code)
}
