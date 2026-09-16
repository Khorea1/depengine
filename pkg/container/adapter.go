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

// containerRef extracts manager/source/tag from mc.Config. ok is false when
// manager or source is missing — the two fields this method cannot function
// without. tag defaults to "latest", matching every other tag-based
// container tool (docker, podman, skopeo, ...).
func containerRef(mc *config.MethodCandidate) (manager, source, tag string, ok bool) {
	manager, _ = mc.Config["manager"].(string)
	source, _ = mc.Config["source"].(string)
	tag, _ = mc.Config["tag"].(string)
	if tag == "" {
		tag = "latest"
	}
	return manager, source, tag, manager != "" && source != ""
}

// Check reports whether the image is already present in the local image
// store. `<manager> images -q <source>:<tag>` always exits 0 — even when no
// image matches — so presence is decided by non-empty stdout, not exit code.
func (a *ContainerAdapter) Check(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate) bool {
	manager, source, tag, ok := containerRef(mc)
	if !ok {
		return false
	}
	res := rn.Run(ctx, manager, "images", "-q", source+":"+tag)
	if res.Err != nil || res.ExitCode != 0 {
		return false
	}
	return strings.TrimSpace(string(res.Stdout)) != ""
}

// Install pulls the image via `<manager> pull <source>:<tag>`.
func (a *ContainerAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	manager, source, tag, ok := containerRef(mc)
	if !ok {
		return fmt.Errorf("container: tool %q requires both manager and source fields", tool.Name)
	}
	res := rn.Run(ctx, manager, "pull", source+":"+tag)
	return run.CheckResult(res, "container: pull")
}

// CanRemove is always true — `rmi` is a low-risk, well-supported operation
// on both docker and podman.
func (a *ContainerAdapter) CanRemove() bool { return true }

// Remove deletes the local image via `<manager> rmi <source>:<tag>`.
func (a *ContainerAdapter) Remove(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	manager, source, tag, ok := containerRef(mc)
	if !ok {
		return fmt.Errorf("container: tool %q requires both manager and source fields", tool.Name)
	}
	res := rn.Run(ctx, manager, "rmi", source+":"+tag)
	return run.CheckResult(res, "container: rmi")
}

// Compile-time interface checks.
var _ exec.Adapter = (*ContainerAdapter)(nil)
var _ exec.Remover = (*ContainerAdapter)(nil)
