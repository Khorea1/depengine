package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/plan"
)

// resolveUniversalLockDocument uses the executor's read-only plan resolution
// path. It never invokes an installer and only returns a complete immutable
// projection; unsupported or ambiguous mutable identities fail closed.
func resolveUniversalLockDocument(ctx context.Context, schema *config.Schema, clan string, facts *engine.Facts, schemaPath string, logger *slog.Logger, previous *lock.Lock) (plan.LockDocument, error) {
	if schema == nil {
		return plan.LockDocument{}, fmt.Errorf("universal lock: schema is required")
	}
	resolver := newInstallExecutor(installPlan{schema: schemaPath, dryRun: true}, schema, clan, facts, time.Time{}, logger)
	names := make([]string, 0, len(schema.Tools))
	for name := range schema.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	resolvedPlans := make([]plan.ResolvedInstallPlan, 0, len(names))
	resolvedNames := make(map[string]struct{}, len(names))
	for _, name := range names {
		tool := schema.Tools[name]
		if tool == nil || len(tool.Methods) == 0 {
			continue
		}
		attempts := resolver.ExplainTool(ctx, tool, clan)
		var resolved *plan.ResolvedInstallPlan
		for i := range attempts {
			attempt := &attempts[i]
			if attempt.PlanIntent != nil && (attempt.Status == "would_install" || attempt.Status == "already_installed") {
				resolved = attempt.PlanIntent
				break
			}
		}
		if resolved == nil {
			return plan.LockDocument{}, fmt.Errorf("universal lock: %w: no resolvable install candidate for tool %q", plan.ErrLockUnavailable, name)
		}
		for _, revision := range resolver.SourceRevisions() {
			for _, source := range resolved.Sources {
				if source.Kind == revision.Kind && source.Name == revision.Name {
					if err := resolved.ApplyResolvedSourceRevision(revision.Kind, revision.Name, revision.Revision); err != nil {
						return plan.LockDocument{}, fmt.Errorf("universal lock: source revision for tool %q: %w", name, err)
					}
					break
				}
			}
		}
		resolvedPlans = append(resolvedPlans, resolved.Clone())
		resolvedNames[name] = struct{}{}
	}
	if previous != nil && previous.Version == lock.CurrentVersion {
		old, err := previous.ProjectionDocument()
		if err != nil {
			return plan.LockDocument{}, err
		}
		for _, entry := range old.Entries {
			if _, replaced := resolvedNames[entry.Tool.Name]; replaced {
				continue
			}
			if err := entry.RequireImmutable(); err != nil {
				return plan.LockDocument{}, fmt.Errorf("universal lock: retained tool %q: %w", entry.Tool.Name, err)
			}
			resolvedPlans = append(resolvedPlans, plan.ResolvedInstallPlan{
				Version:   plan.CurrentVersion,
				Tool:      entry.Tool,
				Candidate: entry.Candidate,
				Identity: plan.ResolvedIdentity{
					Package: entry.Identity.Package, RequestedVersion: entry.RequestedIntent,
					Version: entry.Identity.Version, Revision: entry.Identity.Revision,
					Digest: entry.Identity.Digest, Source: entry.Identity.Source,
					Registry: entry.Identity.Registry, Scope: entry.Identity.Scope,
					Architecture: entry.Identity.Architecture, Platform: entry.Identity.Platform,
					Environment: entry.Identity.Environment,
				},
				Artifacts: lockedArtifacts(entry.Identity.Artifacts),
				Sources:   lockedSources(entry.Identity.Sources),
			})
		}
	}
	document, err := plan.BuildLockDocument(resolvedPlans)
	if err != nil {
		return plan.LockDocument{}, fmt.Errorf("universal lock: %w", err)
	}
	return document, nil
}

func lockedArtifacts(entries []plan.LockedArtifact) []plan.Artifact {
	artifacts := make([]plan.Artifact, len(entries))
	for i, entry := range entries {
		artifacts[i] = plan.Artifact(entry)
	}
	return artifacts
}

func lockedSources(entries []plan.LockedSource) []plan.SourceReference {
	sources := make([]plan.SourceReference, len(entries))
	for i, entry := range entries {
		sources[i] = plan.SourceReference{Role: entry.Role, Kind: entry.Kind, Name: entry.Name, URL: entry.URL, Revision: entry.Revision, Owned: entry.Owned, Trust: entry.Trust}
	}
	return sources
}
