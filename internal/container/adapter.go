// Package container provides an adapter for installing tools as container
// images via `docker pull` / `podman pull`. There is no shim, no command
// created on PATH — this method only pulls an image into the local image
// store. `manager` picks which binary to drive (docker and podman can
// coexist on the same host, so it can't be auto-detected the way a single
// native package manager can).
package container

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/containerref"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
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
	present, _ := probeImage(ctx, rn, manager, reference, platform)
	return present
}

// probeImage runs the same engine query Check uses and additionally
// distinguishes "the engine answered no" (absent) from "the probe itself
// could not run" (unknown detail). Digest pins are recognizable by the `@`
// separator containerRef produces; tag references never contain one.
func probeImage(ctx context.Context, rn run.Runner, manager, reference, platform string) (present bool, unknown string) {
	if platform != "" {
		res := rn.Run(ctx, manager, "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}{{if .Variant}}/{{.Variant}}{{end}}", reference)
		if res.Err != nil {
			return false, "container: inspect platform: " + res.Err.Error()
		}
		if res.ExitCode != 0 {
			return false, ""
		}
		observed, normalizeErr := containerref.NormalizePlatform(strings.TrimSpace(string(res.Stdout)))
		if normalizeErr != nil {
			return false, "container: inspect platform: " + normalizeErr.Error()
		}
		return observed == platform, ""
	}
	if strings.Contains(reference, "@") {
		res := rn.Run(ctx, manager, "image", "inspect", reference)
		if res.Err != nil {
			return false, "container: inspect image: " + res.Err.Error()
		}
		return res.Err == nil && res.ExitCode == 0, ""
	}
	res := rn.Run(ctx, manager, "images", "-q", reference)
	if res.Err != nil {
		return false, "container: list images: " + res.Err.Error()
	}
	if res.ExitCode != 0 {
		return false, ""
	}
	return strings.TrimSpace(string(res.Stdout)) != "", ""
}

// Install pulls the image via `<manager> pull <reference>`.
func (a *ContainerAdapter) Install(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) error {
	manager, reference, platform, err := containerRef(mc)
	if err != nil {
		return fmt.Errorf("container: tool %q: %w", tool.Name, err)
	}
	return a.pull(ctx, rn, manager, reference, platform)
}

func (a *ContainerAdapter) pull(ctx context.Context, rn run.Runner, manager, reference, platform string) error {
	args := []string{"pull"}
	if platform != "" {
		args = append(args, "--platform", platform)
	}
	args = append(args, reference)
	res := rn.Run(ctx, manager, args...)
	return run.CheckResult(res, "container: pull")
}

// ResolvePlan validates the configured container identity without contacting
// an engine. The planner already projects source, tag/digest intent, and
// platform into the intent, so resolution is validation plus an intent clone.
func (a *ContainerAdapter) ResolvePlan(_ context.Context, _ run.Runner, tool *config.Tool, mc *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	if intent == nil {
		return nil, errors.New("container: nil plan intent")
	}
	if tool == nil || mc == nil {
		return nil, errors.New("container: tool and method are required")
	}
	if _, _, _, err := containerRef(mc); err != nil {
		return nil, err
	}
	resolved := intent.Clone()
	return &resolved, nil
}

// Observe reports whether the requested image identity is present locally,
// using the same probes as Check. A successful probe additionally carries the
// canonical source identity (plus digest for immutable pins and platform when
// a platform is pinned) so reconciliation can tell tag drift from absence.
func (a *ContainerAdapter) Observe(ctx context.Context, rn run.Runner, tool *config.Tool, mc *config.MethodCandidate) (plan.Observation, error) {
	if tool == nil || mc == nil {
		return plan.Observation{}, errors.New("container: tool and method are required")
	}
	manager, reference, platform, err := containerRef(mc)
	if err != nil {
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: err.Error()}, nil
	}
	present, unknown := probeImage(ctx, rn, manager, reference, platform)
	switch {
	case unknown != "":
		return plan.Observation{Presence: plan.PresenceUnknown, Detail: unknown}, nil
	case !present:
		return plan.Observation{Presence: plan.PresenceAbsent}, nil
	}
	source, _ := mc.Config["source"].(string)
	identity := plan.ObservedIdentity{Source: source}
	fields := []plan.IdentityField{plan.FieldSource}
	if _, digest, found := strings.Cut(reference, "@"); found {
		identity.Digest = digest
		fields = append(fields, plan.FieldDigest)
	}
	if platform != "" {
		identity.Platform = platform
		fields = append(fields, plan.FieldPlatform)
	}
	return plan.Observation{Presence: plan.PresencePresent, Identity: identity, KnownFields: fields}, nil
}

// InstallResolved pulls exactly the image described by the resolved plan. The
// engine binary stays a method execution parameter, but the image reference
// (source, tag/digest) and platform come exclusively from resolved identity;
// mc.Config supplies no identity.
func (a *ContainerAdapter) InstallResolved(ctx context.Context, rn run.Runner, _ *config.Tool, mc *config.MethodCandidate, resolved *plan.ResolvedInstallPlan) error {
	if resolved == nil {
		return errors.New("container: nil resolved plan")
	}
	if err := validateResolvedInstallOperation(resolved); err != nil {
		return err
	}
	if mc == nil {
		return errors.New("container: method configuration is required")
	}
	manager := stringConfig(mc, "manager")
	if manager == "" {
		return errors.New("container: requires both manager and source fields")
	}
	reference, platform, err := resolvedReference(resolved)
	if err != nil {
		return err
	}
	return a.pull(ctx, rn, manager, reference, platform)
}

// resolvedReference rebuilds the canonical image reference solely from the
// resolved plan identity, mirroring containerRef's validation so a tampered
// plan fails before any engine invocation.
func resolvedReference(resolved *plan.ResolvedInstallPlan) (reference, platform string, err error) {
	source := resolved.Identity.Source
	digest := resolved.Identity.Digest
	tag := ""
	if requested := resolved.Identity.RequestedVersion; requested != nil {
		switch requested.Mode {
		case plan.VersionDigest:
			if digest == "" {
				digest = requested.Value
			}
		case plan.VersionContainerTag:
			tag = requested.Value
		}
	}
	reference, err = containerref.Reference(source, tag, digest)
	if err != nil {
		return "", "", fmt.Errorf("container: %w", err)
	}
	platform, err = containerref.NormalizePlatform(resolved.Identity.Platform)
	if err != nil {
		return "", "", fmt.Errorf("container: %w", err)
	}
	return reference, platform, nil
}

func validateResolvedInstallOperation(resolved *plan.ResolvedInstallPlan) error {
	if len(resolved.Operations) != 1 {
		return errors.New("container: resolved operations are unsupported")
	}
	op := resolved.Operations[0]
	if op.Kind != "install" || op.Effect != plan.EffectMutation || op.Description != "" || op.Command != nil || op.ArbitraryCode {
		return errors.New("container: resolved operations are unsupported")
	}
	return nil
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
// CheckAvailable assumes availability: image references have no cheap
// local index to probe, so a missing reference surfaces at pull time.
func (a *ContainerAdapter) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}

// CheckHostCompatibility imposes no host constraints: the manager daemon
// itself is the compatibility boundary and is probed via Available.
func (a *ContainerAdapter) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *engine.Facts, string) error {
	return nil
}

var _ exec.Adapter = (*ContainerAdapter)(nil)
var _ exec.AdapterV2 = (*ContainerAdapter)(nil)
