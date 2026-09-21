package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/engine"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/plan"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/source"
	"github.com/Khorea1/depengine/pkg/state"
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
		RunE: func(_ *cobra.Command, args []string) error {
			runRemove(args, removeAll, removeDryRun, removeSchema, removeOnly, removeForce)
			return nil
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

// runRemove removes a tool using the adapter that installed it.
// Supports --all, --dry-run, --schema, and --only flags. Body unchanged
// from the pre-Cobra version — only the flag declarations above it moved,
// and removeArgs is now the positional args Cobra already separated out.
func runRemove(removeArgs []string, removeAll, removeDryRun *bool, removeSchema, removeOnly *string, removeForce *bool) {
	// Validate mutually exclusive flags.
	if *removeAll && *removeOnly != "" {
		log.Default.Error("cannot use both --all and --only")
		os.Exit(2)
	}

	var (
		st  *state.State
		ls  *state.LockedState
		err error
	)
	if *removeDryRun {
		// Dry-run must not create a state lock file or rewrite state merely to
		// render a removal plan. State writes are atomic, so an unlocked read
		// safely observes either the previous or next complete state file.
		st, err = state.Load()
	} else {
		ls, err = state.LoadLocked()
		if err == nil {
			defer ls.Close()
			st = ls.State()
		}
	}
	if err != nil {
		log.Default.Error("load state", "error", err)
		closeStateAndExit(ls, 3)
	}

	// Optionally load schema for validation.
	var schemaTools map[string]*config.Tool
	if *removeSchema != "" {
		s, _, _, err := loadSchema(*removeSchema)
		if err != nil {
			log.Default.Error("load schema", "error", err)
			closeStateAndExit(ls, 2)
		}
		schemaTools = s.Tools
	}

	// The global "native" adapter (registered in main.go) is constructed
	// with an empty clan and falls back to PATH-probing, which is ambiguous
	// for manager binaries shared across clans (e.g. "pkg" on both termux
	// and freebsd — same install command, different check/remove commands).
	// Resolve the real clan from OS facts and make it authoritative here,
	// the same way install/upgrade already do, so removal always uses the
	// correct check/remove commands for this machine.
	if facts, err := engine.GatherFacts(run.OSExecRunner{}); err == nil {
		exec.Replace(exec.NewNativeAdapter(engine.ResolveFamily(facts)))
	} else {
		log.Default.Warn("could not gather OS facts; falling back to PATH-probing for native manager detection", "error", err)
	}

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
	removedThisRun := make(map[string]bool)

	findOwnedResource := func(resource plan.ResourceIdentity) (plan.OwnedResourceState, bool) {
		for _, owned := range st.OwnedResources {
			if owned.Resource == resource {
				return owned, true
			}
		}
		return plan.OwnedResourceState{}, false
	}

	prerequisiteDependents := func(toolName string) ([]string, error) {
		resource, err := plan.PrerequisiteResource(toolName)
		if err != nil {
			return nil, err
		}
		owned, ok := findOwnedResource(resource)
		if !ok {
			return nil, nil
		}
		return append([]string(nil), owned.Dependents...), nil
	}

	finalizeRemovedPrerequisite := func(toolName string) error {
		resource, err := plan.PrerequisiteResource(toolName)
		if err != nil {
			return err
		}
		owned, ok := findOwnedResource(resource)
		if !ok || owned.Ownership != plan.OwnershipDepengine || owned.RefCount() != 0 {
			return nil
		}
		next, err := plan.FinalizeReleasedResource(plan.ResourceReleaseDecision{
			Updated:   st.OwnedResources,
			Removable: []plan.ResourceIdentity{resource},
		}, resource)
		if err != nil {
			return err
		}
		st.OwnedResources = next
		return nil
	}

	resolveRemover := func(toolName string, toolState state.ToolState) (exec.Remover, string, bool) {
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

	var removeTrackedTool func(context.Context, string, bool) bool
	var cleanupReleasedPrerequisites func(context.Context, string, plan.ResourceReleaseDecision) bool

	cleanupReleasedPrerequisites = func(ctx context.Context, ownerName string, release plan.ResourceReleaseDecision) bool {
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

			owned, stillTracked := findOwnedResource(resource)
			if !stillTracked {
				continue
			}
			if owned.Ownership != plan.OwnershipDepengine || owned.RefCount() != 0 {
				log.Default.Error("owned prerequisite changed before cleanup", "tool", ownerName, "prerequisite", helperName)
				ok = false
				continue
			}

			if removedThisRun[helperName] {
				next, err := plan.FinalizeReleasedResource(plan.ResourceReleaseDecision{
					Updated:   st.OwnedResources,
					Removable: []plan.ResourceIdentity{resource},
				}, resource)
				if err != nil {
					log.Default.Error("finalize removed prerequisite", "tool", ownerName, "prerequisite", helperName, "error", err)
					ok = false
					continue
				}
				st.OwnedResources = next
				continue
			}

			// Explicit removals own their ordering. In particular, --all must not
			// recursively delete a tool and then report its later explicit visit as
			// a failure merely because map iteration happened to see its owner first.
			if requestedRemoval[helperName] {
				continue
			}

			helperState, exists := st.Tools[helperName]
			if !exists {
				log.Default.Error("owned prerequisite is not tracked as a tool; retaining zero-ref state", "tool", ownerName, "prerequisite", helperName)
				ok = false
				continue
			}
			if helperState.RootRequested {
				log.Default.Info("retaining prerequisite requested as root", "tool", ownerName, "prerequisite", helperName)
				continue
			}
			if !removeTrackedTool(ctx, helperName, true) {
				log.Default.Error("owned prerequisite cleanup failed; retaining state for retry", "tool", ownerName, "prerequisite", helperName)
				ok = false
			}
		}
		return ok
	}

	removeTrackedTool = func(ctx context.Context, toolName string, automatic bool) bool {
		toolState, exists := st.Tools[toolName]
		if !exists {
			return removedThisRun[toolName]
		}
		if automatic && toolState.RootRequested {
			return true
		}

		remover, methodKind, removable := resolveRemover(toolName, toolState)
		if !removable {
			return false
		}
		mc := &config.MethodCandidate{Kind: methodKind, Config: toolState.Config}
		tool := &config.Tool{Name: toolName}
		if err := remover.Remove(ctx, run.OSExecRunner{}, tool, mc); err != nil {
			log.Default.Error("remove failed", "tool", toolName, "error", err)
			return false
		}
		if automatic {
			log.Default.Info("removed unreferenced prerequisite", "tool", toolName, "method", toolState.Method)
		} else {
			log.Default.Info("removed", "tool", toolName, "method", toolState.Method)
		}
		removedThisRun[toolName] = true

		release, releaseErr := plan.ReleaseDependentResources(st.OwnedResources, toolName)
		if releaseErr != nil {
			log.Default.Error("release owned resources", "tool", toolName, "error", releaseErr)
			delete(st.Tools, toolName)
			return false
		}
		st.OwnedResources = release.Updated

		nextOwned, sourceCleanupErr := source.NewManager(run.OSExecRunner{}, false).CleanupReleasedSources(ctx, release)
		st.OwnedResources = nextOwned
		delete(st.Tools, toolName)

		ok := true
		if sourceCleanupErr != nil {
			log.Default.Error("owned source cleanup failed; retaining state for retry", "tool", toolName, "error", sourceCleanupErr)
			ok = false
		}
		if err := finalizeRemovedPrerequisite(toolName); err != nil {
			log.Default.Error("finalize prerequisite ownership", "tool", toolName, "error", err)
			ok = false
		}
		if !cleanupReleasedPrerequisites(ctx, toolName, release) {
			ok = false
		}
		for _, resource := range release.Removable {
			if resource.Kind != plan.ResourceSource && resource.Kind != plan.ResourcePrerequisite {
				log.Default.Warn("owned resource cleanup not implemented; retaining zero-ref state", "tool", toolName, "kind", resource.Kind, "resource", resource.Key)
			}
		}
		return ok
	}

	removeTool := func(toolName string) bool {
		// If schema is loaded, validate tool exists (warn but continue).
		if schemaTools != nil {
			if _, ok := schemaTools[toolName]; !ok {
				log.Default.Warn("tool not found in schema, removing from state anyway", "tool", toolName)
			}
		}

		toolState, ok := st.Tools[toolName]
		if !ok {
			if removedThisRun[toolName] {
				return true
			}
			log.Default.Warn("tool not installed, nothing to remove", "tool", toolName)
			return false
		}

		dependents, err := prerequisiteDependents(toolName)
		if err != nil {
			log.Default.Error("inspect prerequisite dependents", "tool", toolName, "error", err)
			return false
		}
		var blockers []string
		for _, dependent := range dependents {
			if !requestedRemoval[dependent] {
				blockers = append(blockers, dependent)
			}
		}
		if len(blockers) > 0 {
			log.Default.Error("tool is still required by tracked tools", "tool", toolName, "dependents", strings.Join(blockers, ","))
			return false
		}

		if *removeDryRun {
			if _, _, removable := resolveRemover(toolName, toolState); !removable {
				return false
			}
			log.Default.Info("would remove", "tool", toolName, "method", toolState.Method)
			release, err := plan.ReleaseDependentResources(st.OwnedResources, toolName)
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
					if helperState, exists := st.Tools[helperName]; exists && helperState.RootRequested {
						log.Default.Info("would retain prerequisite requested as root", "tool", toolName, "prerequisite", helperName)
					} else if requestedRemoval[helperName] {
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

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return removeTrackedTool(ctx, toolName, false)
	}

	if *removeAll && !*removeForce {
		if !isInteractive() {
			log.Default.Error("stdin is not a terminal; use --force to confirm, or run in an interactive terminal")
			closeStateAndExit(ls, 2)
		}
		fmt.Fprint(os.Stderr, "WARNING: This will remove ALL installed tools tracked by depengine.\nAre you sure? [y/N] ")
		input, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Fprintln(os.Stderr, "Aborted.")
			closeStateAndExit(ls, 0)
		}
	}

	hadFailure := false

	switch {
	case *removeAll:
		for toolName := range st.Tools {
			if !removeTool(toolName) {
				hadFailure = true
			}
		}
	case *removeOnly != "":
		if !removeTool(*removeOnly) {
			hadFailure = true
		}
	case len(removeArgs) > 0:
		for _, toolName := range removeArgs {
			if !removeTool(toolName) {
				hadFailure = true
			}
		}
	default:
		log.Default.Error("usage: depengine remove [--all | --only=<tool> | <tool>...] [--schema=<path>]")
		closeStateAndExit(ls, 1)
	}

	if !*removeDryRun {
		if err := ls.Save(); err != nil {
			log.Default.Error("failed to update state", "error", err)
			closeStateAndExit(ls, 3)
		}
	}

	if hadFailure {
		closeStateAndExit(ls, 1)
	}
}

func isInteractive() bool {
	fi, _ := os.Stdin.Stat()
	return fi != nil && fi.Mode()&os.ModeCharDevice != 0
}
