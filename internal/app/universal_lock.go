package app

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/plan"
)

// resolveUniversalLockDocument uses the executor's read-only plan resolution
// path. It never invokes an installer and only returns an immutable projection
// with exact coverage of expectedTools; unsupported identities or incomplete
// retained coverage fail closed.
func resolveUniversalLockDocument(ctx context.Context, schema *config.Schema, clan string, facts *platform.Facts, schemaPath string, logger *slog.Logger, previous *lock.Lock, expectedTools []string, methodsHash map[string]string) (plan.LockDocument, error) {
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
	var previousDocument plan.LockDocument
	if previous != nil && previous.Version == lock.CurrentVersion {
		var err error
		previousDocument, err = previous.ProjectionDocument()
		if err != nil {
			return plan.LockDocument{}, err
		}
	}
	for _, name := range names {
		tool := schema.Tools[name]
		if tool == nil || len(tool.Methods) == 0 {
			continue
		}
		selected, err := resolver.ResolveLockCandidate(ctx, tool, clan)
		if err != nil {
			return plan.LockDocument{}, fmt.Errorf("universal lock: %w: no resolvable install candidate for tool %q: %w", plan.ErrLockUnavailable, name, err)
		}
		resolved := selected.Plan.Clone()
		carryForwardArtifactIntegrity(&resolved, previous, previousDocument)
		resolvedPlans = append(resolvedPlans, resolved)
		resolvedNames[name] = struct{}{}
	}
	if previous != nil && previous.Version == lock.CurrentVersion {
		for _, entry := range previousDocument.Entries {
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

// carryForwardArtifactIntegrity preserves only a checksum for an identical
// artifact configuration in a previous v2 projection. V1 pins lack artifact
// identity, so their checksums cannot be proven safe to reuse. Candidate
// identity is always freshly resolved.
func carryForwardArtifactIntegrity(current *plan.ResolvedInstallPlan, previous *lock.Lock, previousDocument plan.LockDocument) {
	if current == nil || previous == nil || previous.Version != lock.CurrentVersion {
		return
	}
	var prior *plan.LockProjection
	for i := range previousDocument.Entries {
		entry := &previousDocument.Entries[i]
		if entry.Tool.Name == current.Tool.Name {
			prior = entry
			break
		}
	}
	for i := range current.Artifacts {
		artifact := &current.Artifacts[i]
		if artifact.Checksum != "" && artifact.Checksum != "sha256:auto" || prior == nil {
			continue
		}
		for _, old := range prior.Identity.Artifacts {
			if old.Kind == artifact.Kind && old.URL == artifact.URL && old.LocalPath == artifact.LocalPath &&
				old.ChecksumURL == artifact.ChecksumURL && old.ChecksumFileFormat == artifact.ChecksumFileFormat &&
				old.SignatureURL == artifact.SignatureURL && old.SignaturePath == artifact.SignaturePath && old.SigningKey == artifact.SigningKey &&
				old.Checksum != "" && old.Checksum != "sha256:auto" {
				artifact.Checksum = old.Checksum
				break
			}
		}
	}
}

// universalLockIntentDrifted requires refreshed metadata for retained entries.
// A tool absent from the projection is left to the caller's coverage check.
func universalLockIntentDrifted(previous *lock.Lock, previousDocument plan.LockDocument, name string, methodsHash, sourceHash map[string]string) bool {
	if previous == nil {
		return false
	}
	if _, retained := previousDocument.EntryForTool(name); !retained {
		return false
	}
	previousMethods, exists := previous.MethodsHash[name]
	if !exists || previousMethods != methodsHash[name] {
		return true
	}
	for key, current := range sourceHash {
		if lockCandidateToolName(key) == name && previous.SourceHash[key] != current {
			return true
		}
	}
	for key := range previous.SourceHash {
		if lockCandidateToolName(key) == name {
			if _, exists := sourceHash[key]; !exists {
				return true
			}
		}
	}
	return false
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
