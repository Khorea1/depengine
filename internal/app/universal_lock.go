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
// path. It never invokes an installer and only returns an immutable projection
// with exact coverage of expectedTools; unsupported identities or incomplete
// retained coverage fail closed.
func resolveUniversalLockDocument(ctx context.Context, schema *config.Schema, clan string, facts *engine.Facts, schemaPath string, logger *slog.Logger, previous *lock.Lock, expectedTools []string) (plan.LockDocument, error) {
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
	expectedNames := make(map[string]struct{}, len(expectedTools))
	for _, name := range expectedTools {
		expectedNames[name] = struct{}{}
	}
	for _, name := range names {
		tool := schema.Tools[name]
		if tool == nil || len(tool.Methods) == 0 {
			continue
		}
		attempts, sourceRevisions := resolver.ExplainToolWithSourceRevisions(ctx, tool, clan)
		var resolved *plan.ResolvedInstallPlan
		for i := range attempts {
			attempt := &attempts[i]
			if attempt.PlanIntent != nil && attempt.Error == "" && (attempt.Status == "would_install" || attempt.Status == "already_installed") {
				resolved = attempt.PlanIntent
				break
			}
		}
		if resolved == nil {
			return plan.LockDocument{}, fmt.Errorf("universal lock: %w: no resolvable install candidate for tool %q", plan.ErrLockUnavailable, name)
		}
		for _, revision := range sourceRevisions {
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
			if _, expected := expectedNames[entry.Tool.Name]; !expected {
				continue
			}
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
	if err := document.VerifyCoverage(expectedTools); err != nil {
		return plan.LockDocument{}, fmt.Errorf("universal lock: incomplete install-closure coverage: %w", err)
	}
	return document, nil
}

// universalLockCoverageNames returns the non-virtual tools in an effective
// install closure. Virtual dependency-group tools have no selected candidate
// and therefore no lock projection entry.
func universalLockCoverageNames(tools map[string]*config.Tool) []string {
	names := make([]string, 0, len(tools))
	for name, tool := range tools {
		if tool == nil || len(tool.Methods) == 0 {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// canPersistProjection reports whether this update may build and persist the
// universal projection, the step that promotes depengine.lock to v2.
//
// A v2 lock is consumed through plan.LockDocument.EntryForPlan, which fails
// closed for any tool the projection does not contain. Coverage can only be
// proven in two ways:
//
//   - the previous lock is already v2: its projection may supply entries for
//     tools this run did not resolve, which is what --profile relies on; exact
//     coverage is still verified against the current full install closure; or
//   - this run resolved the whole schema (no --profile filter), so the fresh
//     document covers the install closure by construction.
//
// `update --profile` over a v1 or missing lock has neither: the fresh document
// omits tools outside the profile and a v1 lock carries no projection to
// recover them from. Persisting it would leave legacy pins that the v2
// consumer can no longer look up, so the lock keeps its v1 shape and the next
// full-scope update performs the migration.
func canPersistProjection(previous *lock.Lock, profile string) bool {
	if previous != nil && previous.Version == lock.CurrentVersion {
		return true
	}
	return profile == ""
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
