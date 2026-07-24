package ecosystem

import (
	"context"
	"fmt"
	"strings"

	"depengine/pkg/exec"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

// AsdfAdapter implements the Adapter interface for asdf/mise packages.
type AsdfAdapter struct{}

// NewAsdfAdapter creates a new AsdfAdapter.
func NewAsdfAdapter() *AsdfAdapter {
	return &AsdfAdapter{}
}

func (a *AsdfAdapter) Kind() string { return "asdf" }

func (a *AsdfAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "asdf") || run.LookPath(ctx, rn, "mise")
}

func (a *AsdfAdapter) Check(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) bool {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return false
	}
	for _, cmd := range []string{"asdf", "mise"} {
		if run.LookPath(ctx, rn, cmd) {
			res := rn.Run(ctx, cmd, "list", pkg[0])
			if res.Err == nil && res.ExitCode == 0 && hasWord(string(res.Stdout), pkg[0]) {
				return true
			}
		}
	}
	return false
}

func (a *AsdfAdapter) Install(ctx context.Context, rn run.Runner, tool *schema.Tool, mc *schema.MethodCandidate) error {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 || pkg[0] == "" {
		return fmt.Errorf("asdf: no package name")
	}
	for _, cmd := range []string{"asdf", "mise"} {
		if !run.LookPath(ctx, rn, cmd) {
			continue
		}

		if cmd == "asdf" {
			// Check if plugin already exists — `asdf plugin list` lists all plugins.
			plugRes := rn.Run(ctx, cmd, "plugin", "list")
			if plugRes.Err == nil && plugRes.ExitCode == 0 {
				plugins := strings.Split(strings.TrimSpace(string(plugRes.Stdout)), "\n")
				found := false
				for _, p := range plugins {
					if strings.TrimSpace(p) == pkg[0] {
						found = true
						break
					}
				}
				if !found {
					// plugin-add exit code 2 means "plugin already exists" — continue.
					res := rn.Run(ctx, cmd, "plugin-add", pkg[0])
					if res.Err != nil && res.ExitCode != 2 {
						return fmt.Errorf("asdf: plugin-add failed for %s: %w", pkg[0], res.Err)
					}
				}
			} else {
				// `asdf plugin list` failed — try plugin-add directly.
				res := rn.Run(ctx, cmd, "plugin-add", pkg[0])
				if res.Err != nil && res.ExitCode != 2 {
					return fmt.Errorf("asdf: plugin-add failed for %s: %w", pkg[0], res.Err)
				}
			}
		}

		if res := rn.Run(ctx, cmd, "install", pkg[0]); res.Err != nil {
			return fmt.Errorf("%s: install failed: %w", cmd, res.Err)
		}

		if cmd == "mise" {
			if res := rn.Run(ctx, cmd, "use", "-g", pkg[0]+"@latest"); res.Err != nil {
				return fmt.Errorf("mise: global set failed: %w", res.Err)
			}
		} else {
			if res := rn.Run(ctx, cmd, "global", pkg[0], "latest"); res.Err != nil {
				return fmt.Errorf("asdf: global set failed: %w", res.Err)
			}
		}

		return nil
	}
	return fmt.Errorf("asdf: neither asdf nor mise found")
}

var _ exec.Adapter = (*AsdfAdapter)(nil)
