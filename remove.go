package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/source"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

// newRemoveCmd builds `depengine remove`.
func newRemoveCmd() *cobra.Command {
	removeAll := new(bool)
	removeDryRun := new(bool)
	removeSchema := new(string)
	removeOnly := new(string)
	removeForce := new(bool)

	cmd := &cobra.Command{
		Use:     "remove [tool...]",
		Short:   ifPT("Remover ferramentas do sistema", "Remove tools from the system"),
		GroupID: groupManage,
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(cmd.Context(), args, removeAll, removeDryRun, removeSchema, removeOnly, removeForce)
		},
	}
	f := cmd.Flags()
	f.BoolVar(removeAll, "all", false, "remove all tools")
	f.BoolVar(removeDryRun, "dry-run", false, "show what would be removed")
	f.StringVar(removeSchema, "schema", "", "path to schema.toml (optional, for validation)")
	f.StringVar(removeOnly, "only", "", "only remove specific tool (alternative to positional arg)")
	f.BoolVar(removeForce, "force", false, "skip confirmation when removing all tools")
	return cmd
}

// removeSession carries per-invocation removal state shared by the phase
// helpers below. State aliases the locked (or unlocked, for dry-run) state
// loaded by runRemove; helpers mutate it in place exactly as the original
// runRemove closures did.
type removeSession struct {
	ctx              context.Context
	runner           run.Runner
	state            *state.State
	schemaTools      map[string]*config.Tool
	dryRun           bool
	requestedRemoval map[string]bool
	removedThisRun   map[string]bool
}

// runRemove removes tools using the adapter that installed them.
// Supports --all, --dry-run, --schema, and --only flags. Thin orchestrator:
// flag validation → state load → schema load → native adapter resolution →
// confirmation → removal loop → state persist. Each phase lives in its own
// helper below; behavior is unchanged from the pre-split version.
func runRemove(ctx context.Context, removeArgs []string, removeAll, removeDryRun *bool, removeSchema, removeOnly *string, removeForce *bool) error {
	if err := validateRemoveFlags(removeAll, removeOnly); err != nil {
		return err
	}

	st, ls, err := loadRemoveState(*removeDryRun)
	if err != nil {
		return err
	}
	if ls != nil {
		defer ls.Close()
	}

	schemaTools, err := loadRemoveSchemaTools(*removeSchema)
	if err != nil {
		return err
	}

	ensureRemoveNativeAdapter()

	sess := &removeSession{
		ctx:              ctx,
		runner:           run.OSExecRunner{},
		state:            st,
		schemaTools:      schemaTools,
		dryRun:           *removeDryRun,
		requestedRemoval: collectRemovalTargets(st, removeAll, removeOnly, removeArgs),
		removedThisRun:   make(map[string]bool),
	}

	proceed, err := confirmRemoveAll(removeAll, removeForce)
	if err != nil || !proceed {
		return err
	}

	hadFailure, err := sess.removeRequestedTools(removeAll, removeOnly, removeArgs)
	if err != nil {
		return err
	}

	if err := persistRemoveState(ls, *removeDryRun); err != nil {
		return err
	}

	if hadFailure {
		return exitWithCode(1)
	}
	return nil
}

// validateRemoveFlags rejects mutually exclusive flag combinations.
func validateRemoveFlags(removeAll *bool, removeOnly *string) error {
	if *removeAll && *removeOnly != "" {
		log.Default.Error("cannot use both --all and --only")
		return exitWithCode(2)
	}
	return nil
}

// loadRemoveState loads removal state. Dry-run takes an unlocked read so
// merely rendering a removal plan never creates a lock file or rewrites
// state; real removals take the lock.
func loadRemoveState(dryRun bool) (*state.State, *state.LockedState, error) {
	if dryRun {
		// Dry-run must not create a state lock file or rewrite state merely to
		// render a removal plan. State writes are atomic, so an unlocked read
		// safely observes either the previous or next complete state file.
		st, err := state.Load()
		if err != nil {
			log.Default.Error("load state", "error", err)
			return nil, nil, exitWithCode(3)
		}
		return st, nil, nil
	}
	ls, err := state.LoadLocked()
	if err != nil {
		log.Default.Error("load state", "error", err)
		return nil, nil, exitWithCode(3)
	}
	return ls.State(), ls, nil
}

// loadRemoveSchemaTools optionally loads schema tools for validation.
// An empty path disables validation and returns a nil map.
func loadRemoveSchemaTools(schemaPath string) (map[string]*config.Tool, error) {
	if schemaPath == "" {
		return nil, nil
	}
	s, _, _, err := loadSchema(schemaPath)
	if err != nil {
		log.Default.Error("load schema", "error", err)
		return nil, exitWithCode(2)
	}
	return s.Tools, nil
}

// ensureRemoveNativeAdapter resolves the real distro clan from OS facts and
// makes it authoritative for native removal. The global "native" adapter
// (registered in main.go) is constructed with an empty clan and falls back
// to PATH-probing, which is ambiguous for manager binaries shared across
// clans (e.g. "pkg" on both termux and freebsd — same install command,
// different check/remove commands). Same treatment install/upgrade already do.
func ensureRemoveNativeAdapter() {
	if facts, err := engine.GatherFacts(run.OSExecRunner{}); err == nil {
		exec.Replace(exec.NewNativeAdapter(engine.ResolveFamily(facts)))
	} else {
		log.Default.Warn("could not gather OS facts; falling back to PATH-probing for native manager detection", "error", err)
	}
}

// collectRemovalTargets builds the set of tools scheduled for removal from
// --all, --only, or the positional args. Pure: no I/O, no logging.
func collectRemovalTargets(st *state.State, removeAll *bool, removeOnly *string, removeArgs []string) map[string]bool {
	requestedRemoval := make(map[string]bool)
	switch {
	case *removeAll:
		for toolName := range st.Tools {
			requestedRemoval[toolName] = true
		}
	case *removeOnly != "":
		requestedRemoval[*removeOnly] = true
	default:
		for _, toolName := range removeArgs {
			requestedRemoval[toolName] = true
		}
	}
	return requestedRemoval
}

// resolveRemover maps a tool's recorded install method to the adapter that
// can remove it. Falls back to Method when MethodKind is empty (explicitly
// constructed current-format state). Returns removable=false for the
// manual-remove-required paths: unknown adapter or no remove support.
func resolveRemover(toolName string, toolState state.ToolState) (exec.Remover, string, bool) {
	methodKind := toolState.MethodKind
	if methodKind == "" {
		methodKind = toolState.Method // fallback for explicitly constructed current-format state
	}
	adapter := exec.Lookup(methodKind)
	if adapter == nil {
		log.Default.Warn("adapter not found for method", "tool", toolName, "method", toolState.Method, "methodKind", methodKind)
		log.Default.Warn("manual remove required", "tool", toolName)
		return nil, methodKind, false
	}
	if !exec.CanRemove(adapter) {
		log.Default.Warn("manual remove required", "tool", toolName, "method", toolState.Method, "methodKind", methodKind)
		return nil, methodKind, false
	}
	return adapter.(exec.Remover), methodKind, true
}

// findOwnedResource returns the ownership record for a shared resource.
func (s *removeSession) findOwnedResource(resource plan.ResourceIdentity) (plan.OwnedResourceState, bool) {
	for _, owned := range s.state.OwnedResources {
		if owned.Resource == resource {
			return owned, true
		}
	}
	return plan.OwnedResourceState{}, false
}

// prerequisiteDependents returns the tracked tools still depending on the
// prerequisite helper behind toolName, or nil when toolName is not a helper.
func (s *removeSession) prerequisiteDependents(toolName string) ([]string, error) {
	resource, err := plan.PrerequisiteResource(toolName)
	if err != nil {
		return nil, err
	}
	owned, ok := s.findOwnedResource(resource)
	if !ok {
		return nil, nil
	}
	return append([]string(nil), owned.Dependents...), nil
}

// prerequisiteBlockers returns the dependents that block removal: tools still
// depending on toolName that are not themselves scheduled for removal.
func (s *removeSession) prerequisiteBlockers(toolName string) ([]string, error) {
	dependents, err := s.prerequisiteDependents(toolName)
	if err != nil {
		return nil, err
	}
	var blockers []string
	for _, dependent := range dependents {
		if !s.requestedRemoval[dependent] {
			blockers = append(blockers, dependent)
		}
	}
	return blockers, nil
}

// finalizeRemovedPrerequisite drops the zero-ref depengine-owned ownership
// record left behind for an explicitly removed prerequisite helper.
func (s *removeSession) finalizeRemovedPrerequisite(toolName string) error {
	resource, err := plan.PrerequisiteResource(toolName)
	if err != nil {
		return err
	}
	owned, ok := s.findOwnedResource(resource)
	if !ok || owned.Ownership != plan.OwnershipDepengine || owned.RefCount() != 0 {
		return nil
	}
	next, err := plan.FinalizeReleasedResource(plan.ResourceReleaseDecision{
		Updated:   s.state.OwnedResources,
		Removable: []plan.ResourceIdentity{resource},
	}, resource)
	if err != nil {
		return err
	}
	s.state.OwnedResources = next
	return nil
}

// invokeRemover runs the adapter's Remove for one tool and records success.
func (s *removeSession) invokeRemover(ctx context.Context, toolName string, toolState state.ToolState, remover exec.Remover, methodKind string, automatic bool) bool {
	mc := &config.MethodCandidate{Kind: methodKind, Config: toolState.Config}
	tool := &config.Tool{Name: toolName}
	if err := remover.Remove(ctx, s.runner, tool, mc); err != nil {
		log.Default.Error("remove failed", "tool", toolName, "error", err)
		return false
	}
	if automatic {
		log.Default.Info("removed unreferenced prerequisite", "tool", toolName, "method", toolState.Method)
	} else {
		log.Default.Info("removed", "tool", toolName, "method", toolState.Method)
	}
	s.removedThisRun[toolName] = true
	return true
}

// releaseOwnedResources releases the removed tool's owned resources and
// projects the updated ownership snapshot. On failure the tool entry is
// dropped (removal itself succeeded) and the caller fails closed.
func (s *removeSession) releaseOwnedResources(toolName string) (plan.ResourceReleaseDecision, bool) {
	release, err := plan.ReleaseDependentResources(s.state.OwnedResources, toolName)
	if err != nil {
		log.Default.Error("release owned resources", "tool", toolName, "error", err)
		delete(s.state.Tools, toolName)
		return plan.ResourceReleaseDecision{}, false
	}
	s.state.OwnedResources = release.Updated
	return release, true
}

// cleanupReleasedSources removes owned sources released by a tool removal and
// drops the tool entry. Source cleanup failures retain state for retry.
func (s *removeSession) cleanupReleasedSources(ctx context.Context, toolName string, release plan.ResourceReleaseDecision) bool {
	nextOwned, sourceCleanupErr := source.NewManager(run.OSExecRunner{}, false).CleanupReleasedSources(ctx, release)
	s.state.OwnedResources = nextOwned
	delete(s.state.Tools, toolName)

	if sourceCleanupErr != nil {
		log.Default.Error("owned source cleanup failed; retaining state for retry", "tool", toolName, "error", sourceCleanupErr)
		return false
	}
	return true
}

// reportUnimplementedResourceKinds warns about released resource kinds with
// no automated cleanup; their zero-ref state is retained.
func (s *removeSession) reportUnimplementedResourceKinds(toolName string, release plan.ResourceReleaseDecision) {
	for _, resource := range release.Removable {
		if resource.Kind != plan.ResourceSource && resource.Kind != plan.ResourcePrerequisite {
			log.Default.Warn("owned resource cleanup not implemented; retaining zero-ref state", "tool", toolName, "kind", resource.Kind, "resource", resource.Key)
		}
	}
}

// removeTrackedTool removes one state-tracked tool plus its now-unreferenced
// owned prerequisites. Automatic removals skip root-requested tools (they
// stay until explicitly removed).
func (s *removeSession) removeTrackedTool(ctx context.Context, toolName string, automatic bool) bool {
	toolState, exists := s.state.Tools[toolName]
	if !exists {
		return s.removedThisRun[toolName]
	}
	if automatic && toolState.RootRequested {
		return true
	}

	remover, methodKind, removable := resolveRemover(toolName, toolState)
	if !removable {
		return false
	}
	if !s.invokeRemover(ctx, toolName, toolState, remover, methodKind, automatic) {
		return false
	}

	release, ok := s.releaseOwnedResources(toolName)
	if !ok {
		return false
	}

	ok = s.cleanupReleasedSources(ctx, toolName, release)
	if err := s.finalizeRemovedPrerequisite(toolName); err != nil {
		log.Default.Error("finalize prerequisite ownership", "tool", toolName, "error", err)
		ok = false
	}
	if !s.cleanupReleasedPrerequisites(ctx, toolName, release) {
		ok = false
	}
	s.reportUnimplementedResourceKinds(toolName, release)
	return ok
}

// cleanupReleasedPrerequisites garbage-collects depengine-owned prerequisites
// left with zero references by a removal. Explicitly scheduled tools are
// left for their own visit so --all ordering never reports a later explicit
// visit as a failure; root-requested helpers are retained.
func (s *removeSession) cleanupReleasedPrerequisites(ctx context.Context, ownerName string, release plan.ResourceReleaseDecision) bool {
	ok := true
	for _, resource := range release.Removable {
		if resource.Kind != plan.ResourcePrerequisite {
			continue
		}
		helperName, err := plan.PrerequisiteToolName(resource)
		if err != nil {
			log.Default.Error("decode owned prerequisite", "tool", ownerName, "resource", resource.Key, "error", err)
			ok = false
			continue
		}

		owned, stillTracked := s.findOwnedResource(resource)
		if !stillTracked {
			continue
		}
		if owned.Ownership != plan.OwnershipDepengine || owned.RefCount() != 0 {
			log.Default.Error("owned prerequisite changed before cleanup", "tool", ownerName, "prerequisite", helperName)
			ok = false
			continue
		}

		if s.removedThisRun[helperName] {
			next, err := plan.FinalizeReleasedResource(plan.ResourceReleaseDecision{
				Updated:   s.state.OwnedResources,
				Removable: []plan.ResourceIdentity{resource},
			}, resource)
			if err != nil {
				log.Default.Error("finalize removed prerequisite", "tool", ownerName, "prerequisite", helperName, "error", err)
				ok = false
				continue
			}
			s.state.OwnedResources = next
			continue
		}

		// Explicit removals own their ordering. In particular, --all must not
		// recursively delete a tool and then report its later explicit visit as
		// a failure merely because map iteration happened to see its owner first.
		if s.requestedRemoval[helperName] {
			continue
		}

		helperState, exists := s.state.Tools[helperName]
		if !exists {
			log.Default.Error("owned prerequisite is not tracked as a tool; retaining zero-ref state", "tool", ownerName, "prerequisite", helperName)
			ok = false
			continue
		}
		if helperState.RootRequested {
			log.Default.Info("retaining prerequisite requested as root", "tool", ownerName, "prerequisite", helperName)
			continue
		}
		if !s.removeTrackedTool(ctx, helperName, true) {
			log.Default.Error("owned prerequisite cleanup failed; retaining state for retry", "tool", ownerName, "prerequisite", helperName)
			ok = false
		}
	}
	return ok
}

// planDryRunRemoval renders what a real removal would do without touching
// the system or state. Read-only: resolver lookup plus ownership planning.
func (s *removeSession) planDryRunRemoval(toolName string, toolState state.ToolState) bool {
	if _, _, removable := resolveRemover(toolName, toolState); !removable {
		return false
	}
	log.Default.Info("would remove", "tool", toolName, "method", toolState.Method)
	release, err := plan.ReleaseDependentResources(s.state.OwnedResources, toolName)
	if err != nil {
		log.Default.Error("plan owned resource release", "tool", toolName, "error", err)
		return false
	}
	for _, resource := range release.Removable {
		switch resource.Kind {
		case plan.ResourceSource:
			sourceConfig, err := source.FromResourceIdentity(resource)
			if err != nil {
				log.Default.Error("decode owned source", "tool", toolName, "resource", resource.Key, "error", err)
				return false
			}
			log.Default.Info("would remove owned source", "tool", toolName, "kind", sourceConfig.Kind, "source", sourceConfig.Name)
		case plan.ResourcePrerequisite:
			helperName, err := plan.PrerequisiteToolName(resource)
			if err != nil {
				log.Default.Error("decode owned prerequisite", "tool", toolName, "resource", resource.Key, "error", err)
				return false
			}
			if helperState, exists := s.state.Tools[helperName]; exists && helperState.RootRequested {
				log.Default.Info("would retain prerequisite requested as root", "tool", toolName, "prerequisite", helperName)
			} else if s.requestedRemoval[helperName] {
				log.Default.Info("prerequisite is also explicitly scheduled for removal", "tool", toolName, "prerequisite", helperName)
			} else {
				log.Default.Info("would remove owned prerequisite", "tool", toolName, "prerequisite", helperName)
			}
		default:
			log.Default.Info("would release owned resource", "tool", toolName, "kind", resource.Kind, "resource", resource.Key, "cleanup", "retained")
		}
	}
	return true
}

// removeSingleTool removes one explicitly requested tool: schema warning,
// tracked-state check, dependent blockers, then dry-run planning or a real
// timed removal.
func (s *removeSession) removeSingleTool(toolName string) bool {
	// If schema is loaded, validate tool exists (warn but continue).
	if s.schemaTools != nil {
		if _, ok := s.schemaTools[toolName]; !ok {
			log.Default.Warn("tool not found in schema, removing from state anyway", "tool", toolName)
		}
	}

	toolState, ok := s.state.Tools[toolName]
	if !ok {
		if s.removedThisRun[toolName] {
			return true
		}
		log.Default.Warn("tool not installed, nothing to remove", "tool", toolName)
		return false
	}

	blockers, err := s.prerequisiteBlockers(toolName)
	if err != nil {
		log.Default.Error("inspect prerequisite dependents", "tool", toolName, "error", err)
		return false
	}
	if len(blockers) > 0 {
		log.Default.Error("tool is still required by tracked tools", "tool", toolName, "dependents", strings.Join(blockers, ","))
		return false
	}

	if s.dryRun {
		return s.planDryRunRemoval(toolName, toolState)
	}

	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Minute)
	defer cancel()
	return s.removeTrackedTool(ctx, toolName, false)
}

// confirmRemoveAll gates --all removals behind an interactive confirmation
// unless --force is given. Returns proceed=false with a nil error when the
// user aborts at the prompt.
func confirmRemoveAll(removeAll, removeForce *bool) (bool, error) {
	return confirmRemoveAllWith(removeAll, removeForce, isInteractive(), os.Stdin)
}

func confirmRemoveAllWith(removeAll, removeForce *bool, interactive bool, input io.Reader) (bool, error) {
	if !*removeAll || *removeForce {
		return true, nil
	}
	if !interactive {
		log.Default.Error("stdin is not a terminal; use --force to confirm, or run in an interactive terminal")
		return false, exitWithCode(2)
	}
	fmt.Fprint(os.Stderr, "WARNING: This will remove ALL installed tools tracked by depengine.\nAre you sure? [y/N] ")
	answer, _ := bufio.NewReader(input).ReadString('\n')
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(os.Stderr, "Aborted.")
		return false, nil
	}
	return true, nil
}

// removeRequestedTools dispatches the removal loop over --all, --only, or
// the positional args. Returns hadFailure=true when any tool failed; a
// missing target entirely is a usage error.
func (s *removeSession) removeRequestedTools(removeAll *bool, removeOnly *string, removeArgs []string) (bool, error) {
	hadFailure := false

	switch {
	case *removeAll:
		for toolName := range s.state.Tools {
			if !s.removeSingleTool(toolName) {
				hadFailure = true
			}
		}
	case *removeOnly != "":
		if !s.removeSingleTool(*removeOnly) {
			hadFailure = true
		}
	case len(removeArgs) > 0:
		for _, toolName := range removeArgs {
			if !s.removeSingleTool(toolName) {
				hadFailure = true
			}
		}
	default:
		log.Default.Error("usage: depengine remove [--all | --only=<tool> | <tool>...] [--schema=<path>]")
		return false, exitWithCode(1)
	}
	return hadFailure, nil
}

// persistRemoveState saves the locked state after a real removal run.
// Dry-runs never write; the lock is nil there.
func persistRemoveState(ls *state.LockedState, dryRun bool) error {
	if dryRun {
		return nil
	}
	if err := ls.Save(); err != nil {
		log.Default.Error("failed to update state", "error", err)
		return exitWithCode(3)
	}
	return nil
}

func isInteractive() bool {
	fi, _ := os.Stdin.Stat()
	return fi != nil && fi.Mode()&os.ModeCharDevice != 0
}
