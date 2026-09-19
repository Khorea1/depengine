package ecosystem

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
)

// CondaAdapter implements the Adapter interface for conda packages.
type CondaAdapter struct{}

// NewCondaAdapter creates a new CondaAdapter.
func NewCondaAdapter() *CondaAdapter { return &CondaAdapter{} }

func (a *CondaAdapter) Kind() string { return "conda" }

func (a *CondaAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "conda")
}

type condaPackage struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Build   string `json:"build"`
	Channel string `json:"channel"`
	BaseURL string `json:"base_url"`
}

func condaPackageName(tool *config.Tool, mc *config.MethodCandidate) string {
	pkg := exec.SubstitutePkg([]string{"{pkg}"}, tool, mc)
	if len(pkg) == 0 {
		return ""
	}
	return pkg[0]
}

// condaTargetArgs makes environment selection deterministic. Without an
// explicit environment or prefix depengine targets base rather than inheriting
// whichever environment happens to be active in the invoking shell.
func condaTargetArgs(mc *config.MethodCandidate) []string {
	if mc != nil {
		if env, _ := mc.Config["environment"].(string); env != "" {
			return []string{"-n", env}
		}
		if prefix, _ := mc.Config["prefix"].(string); prefix != "" {
			return []string{"-p", config.ExpandHomeDir(prefix)}
		}
	}
	return []string{"-n", "base"}
}

func condaChannelArgs(mc *config.MethodCandidate) []string {
	channels := condaChannels(mc)
	out := make([]string, 0, len(channels)*2)
	for _, channel := range channels {
		out = append(out, "-c", channel)
	}
	return out
}

func condaChannels(mc *config.MethodCandidate) []string {
	if mc == nil {
		return nil
	}
	var channels []string
	switch values := mc.Config["channels"].(type) {
	case []string:
		for _, channel := range values {
			if channel != "" {
				channels = append(channels, channel)
			}
		}
	case []any:
		for _, value := range values {
			if channel, ok := value.(string); ok && channel != "" {
				channels = append(channels, channel)
			}
		}
	}
	return channels
}

func condaChannelMatches(installed condaPackage, requested string) bool {
	want := strings.TrimRight(requested, "/")
	for _, actual := range []string{installed.Channel, installed.BaseURL} {
		actual = strings.TrimRight(actual, "/")
		if actual == "" {
			continue
		}
		if strings.EqualFold(actual, want) {
			return true
		}
		// Conda may report a canonical channel name for a requested URL, or a
		// fully qualified URL for a requested channel name. Compare the final
		// path component as a stable best-effort identity.
		actualTail := actual[strings.LastIndex(actual, "/")+1:]
		wantTail := want[strings.LastIndex(want, "/")+1:]
		if actualTail != "" && strings.EqualFold(actualTail, wantTail) {
			return true
		}
	}
	return false
}

func condaPackageSpec(tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	pkg := condaPackageName(tool, mc)
	if pkg == "" {
		return "", fmt.Errorf("conda: no package name")
	}
	version, _ := mc.Config["version"].(string)
	build, _ := mc.Config["build"].(string)
	if build != "" && version == "" {
		return "", fmt.Errorf("conda: build requires version")
	}
	if version == "" {
		return pkg, nil
	}
	if build != "" {
		return pkg + "=" + version + "=" + build, nil
	}
	return pkg + "=" + version, nil
}

func (a *CondaAdapter) queryPackage(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (*condaPackage, error) {
	pkg := condaPackageName(tool, mc)
	if pkg == "" {
		return nil, nil
	}
	args := []string{"list", "--json"}
	args = append(args, condaTargetArgs(mc)...)
	args = append(args, pkg)
	res := rn.Run(ctx, "conda", args...)
	if res.Err != nil || res.ExitCode != 0 {
		return nil, nil
	}
	var packages []condaPackage
	if err := json.Unmarshal(res.Stdout, &packages); err != nil {
		return nil, fmt.Errorf("conda: parse list output: %w", err)
	}
	for i := range packages {
		if strings.EqualFold(packages[i].Name, pkg) {
			return &packages[i], nil
		}
	}
	return nil, nil
}

func (a *CondaAdapter) Check(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) bool {
	installed, err := a.queryPackage(ctx, rn, tool, mc)
	if err != nil || installed == nil {
		return false
	}
	if version, _ := mc.Config["version"].(string); version != "" && installed.Version != version {
		return false
	}
	if build, _ := mc.Config["build"].(string); build != "" && installed.Build != build {
		return false
	}
	if channels := condaChannels(mc); len(channels) > 0 {
		matched := false
		for _, channel := range channels {
			if condaChannelMatches(*installed, channel) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func (a *CondaAdapter) InstalledVersion(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (string, error) {
	installed, err := a.queryPackage(ctx, rn, tool, mc)
	if err != nil || installed == nil {
		return "", err
	}
	return installed.Version, nil
}

func (a *CondaAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	spec, err := condaPackageSpec(tool, mc)
	if err != nil {
		return err
	}
	args := []string{"install", "-y"}
	args = append(args, condaTargetArgs(mc)...)
	args = append(args, condaChannelArgs(mc)...)
	args = append(args, spec)
	res := rn.Run(ctx, "conda", args...)
	return run.CheckResult(res, "conda: install")
}

// CanRemove reports whether this adapter supports removal.
func (a *CondaAdapter) CanRemove() bool { return true }

// Remove uninstalls a package from the same deterministic environment/prefix
// used by Install and Check.
func (a *CondaAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	pkg := condaPackageName(tool, mc)
	if pkg == "" {
		return fmt.Errorf("conda: no package name")
	}
	args := []string{"remove", "-y"}
	args = append(args, condaTargetArgs(mc)...)
	args = append(args, pkg)
	res := rn.Run(ctx, "conda", args...)
	return run.CheckResult(res, "conda: remove")
}

var _ exec.Adapter = (*CondaAdapter)(nil)
var _ exec.Remover = (*CondaAdapter)(nil)
var _ exec.Versioner = (*CondaAdapter)(nil)
