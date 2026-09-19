// Package container provides an adapter for installing tools as container
// images via `docker pull` / `podman pull`. There is no shim, no command
// created on PATH — this method only pulls an image into the local image
// store. `manager` picks which binary to drive (docker and podman can
// coexist on the same host, so it can't be auto-detected the way a single
// native package manager can).
package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/containerref"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/run"
)

// ContainerAdapter implements exec.Adapter for container-image installs.
type ContainerAdapter struct{}

// NewContainerAdapter creates a ContainerAdapter.
func NewContainerAdapter() *ContainerAdapter {
	return &ContainerAdapter{}
}

func (a *ContainerAdapter) Kind() string { return "container" }

// Available reports whether ANY container engine (docker or podman) is on
// PATH. The specific engine a given tool wants is a per-candidate config
// field (`manager`), not something this adapter instance owns — Adapter's
// Available signature has no access to the method candidate — so this is
// necessarily a coarse check. Check/Install/Remove read `manager` from the
// candidate's config and fail with a clear error if that specific binary
// isn't the one actually on PATH.
func (a *ContainerAdapter) Available(ctx context.Context, rn run.Runner) bool {
	return run.LookPath(ctx, rn, "docker") || run.LookPath(ctx, rn, "podman")
}

// containerRef resolves the configured container identity into one canonical
// reference. tag defaults to latest; digest selects immutable identity and is
// mutually exclusive with tag.
func containerRef(mc *config.MethodCandidate) (manager, reference, platform string, err error) {
	if mc == nil {
		return "", "", "", fmt.Errorf("container: missing method configuration")
	}
	manager, _ = mc.Config["manager"].(string)
	source, _ := mc.Config["source"].(string)
	tag, _ := mc.Config["tag"].(string)
	digest, _ := mc.Config["digest"].(string)
	if manager == "" || source == "" {
		return "", "", "", fmt.Errorf("container: requires both manager and source fields")
	}
	ref, err := containerref.Reference(source, tag, digest)
	if err != nil {
		return "", "", "", fmt.Errorf("container: %w", err)
	}
	platform, err = containerref.NormalizePlatform(stringConfig(mc, "platform"))
	if err != nil {
		return "", "", "", fmt.Errorf("container: %w", err)
	}
	return manager, ref, platform, nil
}

// Check reports whether the requested image identity is present locally.
// Mutable tag references use `images -q`, whose exit code alone is not useful
// because Docker/Podman return success even when no image matches. Immutable
// digest references instead use `image inspect source@digest`: success proves
// that exact content identity exists rather than merely some image under the
// same repository/tag.
func (a *ContainerAdapter) Check(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) bool {
	manager, reference, platform, err := containerRef(mc)
	if err != nil {
		return false
	}
	if platform != "" {
		res := rn.Run(ctx, manager, "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}{{if .Variant}}/{{.Variant}}{{end}}", reference)
		if res.Err != nil || res.ExitCode != 0 {
			return false
		}
		observed, normalizeErr := containerref.NormalizePlatform(strings.TrimSpace(string(res.Stdout)))
		return normalizeErr == nil && observed == platform
	}
	if digest, _ := mc.Config["digest"].(string); strings.TrimSpace(digest) != "" {
		res := rn.Run(ctx, manager, "image", "inspect", reference)
		return res.Err == nil && res.ExitCode == 0
	}
	res := rn.Run(ctx, manager, "images", "-q", reference)
	if res.Err != nil || res.ExitCode != 0 {
		return false
	}
	return strings.TrimSpace(string(res.Stdout)) != ""
}

// Install pulls the image via `<manager> pull <reference>`.
func (a *ContainerAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	manager, reference, platform, err := containerRef(mc)
	if err != nil {
		return fmt.Errorf("container: tool %q: %w", tool.Name, err)
	}
	args := []string{"pull"}
	if platform != "" {
		args = append(args, "--platform", platform)
	}
	args = append(args, reference)
	res := rn.Run(ctx, manager, args...)
	return run.CheckResult(res, "container: pull")
}

// CanRemove is always true — `rmi` is a low-risk, well-supported operation
// on both docker and podman.
func (a *ContainerAdapter) CanRemove() bool { return true }

// Remove deletes the local image via `<manager> rmi <reference>`.
func (a *ContainerAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	manager, reference, _, err := containerRef(mc)
	if err != nil {
		return fmt.Errorf("container: tool %q: %w", tool.Name, err)
	}
	res := rn.Run(ctx, manager, "rmi", reference)
	return run.CheckResult(res, "container: rmi")
}

func stringConfig(mc *config.MethodCandidate, key string) string {
	value, _ := mc.Config[key].(string)
	return value
}

// Compile-time interface checks.
var _ exec.Adapter = (*ContainerAdapter)(nil)
var _ exec.Remover = (*ContainerAdapter)(nil)
