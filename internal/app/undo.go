package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

func relativeTime(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		return "in the future"
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case d < 30*24*time.Hour:
		weeks := int(d.Hours() / (24 * 7))
		if weeks == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", weeks)
	case d < 365*24*time.Hour:
		months := int(d.Hours() / (24 * 30))
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	default:
		years := int(d.Hours() / (24 * 365))
		if years == 1 {
			return "1 year ago"
		}
		return fmt.Sprintf("%d years ago", years)
	}
}

// flags maintained in help.go:printCommandHelp
// newUndoCmd builds `depengine undo`.
func newUndoCmd() *cobra.Command {
	undoList := new(bool)
	undoSpecific := new(string)

	cmd := &cobra.Command{
		Use:     "undo",
		Short:   ifPT("Reverter para um snapshot anterior do estado", "Revert to a previous state snapshot"),
		GroupID: groupManage,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUndo(cmd.Context(), undoList, undoSpecific)
		},
	}
	f := cmd.Flags()
	f.BoolVar(undoList, "list", false, "list available snapshots")
	f.StringVar(undoSpecific, "snapshot", "", "revert to specific snapshot file path")
	return cmd
}

// runUndo reverts to a previous state snapshot. Thin orchestrator over the
// phase helpers below: list/target-selection → load → diff → remove →
// restore/report. Every exit code matches the pre-split behavior.
func runUndo(ctx context.Context, undoList *bool, undoSpecific *string) error {
	if *undoList {
		return runUndoList()
	}

	snapPath, err := resolveUndoSnapshotPath(*undoSpecific)
	if err != nil {
		return err
	}

	snapState, err := state.LoadSnapshot(snapPath)
	if err != nil {
		log.Default.Error("load snapshot", "error", err)
		return exitWithCode(3)
	}

	ls, err := state.LoadLocked()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		return exitWithCode(3)
	}
	defer ls.Close()

	curState := ls.State()
	toRemove := diffUndoTools(curState.Tools, snapState.Tools)
	if len(toRemove) == 0 {
		// Do not restore snapshot state here: tools the user deliberately
		// removed after the snapshot exist in snapState but not in curState;
		// restoring snapState.Tools would reintroduce phantom entries.
		log.Default.Info("nothing to undo (no tools were added since snapshot)")
		return nil
	}

	ensureUndoNativeAdapter()
	originalTools, succeeded, hadFailure := removeUndoTools(ctx, toRemove, curState)
	return finalizeUndo(ls, curState, snapState, toRemove, originalTools, succeeded, hadFailure)
}

// runUndoList prints available snapshots newest-first.
func runUndoList() error {
	snapshots, err := state.ListSnapshots()
	if err != nil {
		log.Default.Error("list snapshots", "error", err)
		return exitWithCode(3)
	}
	if len(snapshots) == 0 {
		fmt.Fprintln(os.Stderr, "No snapshots available.")
		return nil
	}
	c := newCLIStyle(os.Stderr)
	fmt.Fprintln(c.w, c.bold("Available snapshots:"))
	idxW := len(fmt.Sprintf("%d", len(snapshots)))
	for i, s := range snapshots {
		idx := i + 1
		// Column order answers the choosing question left to right: which
		// index do I pass, how old is it, what's inside. The full path is
		// noise in the common case — the filename alone identifies it.
		fmt.Fprintf(c.w, "  %s  %s  %s  %s\n",
			c.cyan(padRight(fmt.Sprintf("%d", idx), idxW)),
			padRight(relativeTime(s.Timestamp), 14),
			c.dim(filepath.Base(s.Path)),
			c.dim(fmt.Sprintf("(%s)", plural(s.ToolCount, "tool"))))
	}
	return nil
}

// selectUndoSnapshotPath resolves a --snapshot spec against an already-listed
// snapshot set (newest-first). A numeric spec is a 1-based index; anything
// else passes through as a file path (backward compat). Pure: no I/O.
func selectUndoSnapshotPath(spec string, snapshots []state.SnapshotInfo) (string, error) {
	if n, err := strconv.Atoi(spec); err == nil {
		if n < 1 || n > len(snapshots) {
			log.Default.Error("invalid snapshot index", "index", n, "max", len(snapshots))
			return "", exitWithCode(2)
		}
		return snapshots[n-1].Path, nil
	}
	return spec, nil
}

// resolveUndoSnapshotPath maps the --snapshot flag to a snapshot file path:
// explicit spec (index or path), otherwise the newest snapshot. An empty set
// without a spec is exit 1; list failures are exit 3.
func resolveUndoSnapshotPath(undoSpecific string) (string, error) {
	if undoSpecific != "" {
		if _, err := strconv.Atoi(undoSpecific); err == nil {
			// Treat as index (1-based)
			snapshots, listErr := state.ListSnapshots()
			if listErr != nil {
				log.Default.Error("list snapshots", "error", listErr)
				return "", exitWithCode(3)
			}
			return selectUndoSnapshotPath(undoSpecific, snapshots)
		}
		// Treat as file path (backward compat)
		return undoSpecific, nil
	}
	snapshots, err := state.ListSnapshots()
	if err != nil {
		log.Default.Error("list snapshots", "error", err)
		return "", exitWithCode(3)
	}
	if len(snapshots) == 0 {
		log.Default.Error("no snapshot available for undo")
		return "", exitWithCode(1)
	}
	return snapshots[0].Path, nil
}

// diffUndoTools returns names of tools present in the current state but absent
// from the snapshot — the set undo must remove. Pure: no I/O.
func diffUndoTools(curTools, snapTools map[string]state.ToolState) []string {
	var toRemove []string
	for name := range curTools {
		if _, ok := snapTools[name]; !ok {
			toRemove = append(toRemove, name)
		}
	}
	return toRemove
}

// resolveUndoMethodKind returns the adapter kind for a tracked tool,
// falling back to the legacy Method field for old state files. Pure.
func resolveUndoMethodKind(toolState state.ToolState) string {
	if toolState.MethodKind != "" {
		return toolState.MethodKind
	}
	return toolState.Method // fallback for old state files
}

// ensureUndoNativeAdapter makes the OS-resolved native adapter authoritative,
// the same way install/upgrade already do, so removal uses the correct
// check/remove commands for this machine instead of PATH-probing.
func ensureUndoNativeAdapter() {
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
}

// removeUndoTools removes each tool via its installing adapter (mirroring
// remove.go's state-driven Remover shape). It returns a copy of the
// pre-removal tool map, the per-tool success set, and whether any removal
// failed. Callers merge state with mergeUndoTools.
func removeUndoTools(ctx context.Context, toRemove []string, curState *state.State) (map[string]state.ToolState, map[string]bool, bool) {
	// Capture original state before removal, so failed tools can be preserved.
	originalTools := make(map[string]state.ToolState, len(curState.Tools))
	for k, v := range curState.Tools {
		originalTools[k] = v
	}
	succeeded := make(map[string]bool)
	hadFailure := false
	for _, name := range toRemove {
		toolState := curState.Tools[name]

		log.Default.Info("removing tool added after snapshot", "tool", name, "method", toolState.Method)

		methodKind := resolveUndoMethodKind(toolState)

		adapter, ok := exec.Lookup(methodKind).(exec.AdapterV2)
		if !ok || adapter == nil {
			log.Default.Warn("adapter not found — manual removal may be needed", "tool", name, "method", toolState.Method, "methodKind", methodKind)
			hadFailure = true
			continue
		}

		if !adapter.CanRemove() {
			log.Default.Warn("adapter does not support automated removal — manual removal needed", "tool", name, "method", toolState.Method, "methodKind", methodKind)
			hadFailure = true
			continue
		}

		mc := &config.MethodCandidate{
			Kind:   methodKind,
			Config: toolState.Config,
		}
		tool := &config.Tool{Name: name}

		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		if err := adapter.Remove(ctx, run.OSExecRunner{}, tool, mc); err != nil {
			log.Default.Error("remove failed during undo", "tool", name, "error", err)
			hadFailure = true
			cancel()
			continue
		}
		cancel()

		log.Default.Info("removed during undo", "tool", name)
		succeeded[name] = true
	}
	return originalTools, succeeded, hadFailure
}

// mergeUndoTools restores tool state after removal: on full success the
// snapshot tools win outright; on partial failure the snapshot tools are kept
// and tools whose removal failed are re-added from the pre-removal copy, so
// failed tools stay tracked for retry. Pure: no I/O.
func mergeUndoTools(curState, snapState *state.State, toRemove []string, succeeded map[string]bool, originalTools map[string]state.ToolState, hadFailure bool) {
	if !hadFailure {
		curState.Tools = snapState.Tools
		return
	}
	// Preserve snapshot tools and re-add tools that failed removal.
	curState.Tools = make(map[string]state.ToolState, len(snapState.Tools)+len(toRemove))
	for k, v := range snapState.Tools {
		curState.Tools[k] = v
	}
	for _, name := range toRemove {
		if !succeeded[name] {
			curState.Tools[name] = originalTools[name]
		}
	}
}

// finalizeUndo merges removal results into state (resource release),
// persists, and reports. Exit 3 on save failure, exit 1 when any removal
// failed, nil on full success.
func finalizeUndo(ls *state.LockedState, curState, snapState *state.State, toRemove []string, originalTools map[string]state.ToolState, succeeded map[string]bool, hadFailure bool) error {
	if hadFailure {
		log.Default.Error("undo: some removals failed — saving partial state")
	}

	mergeUndoTools(curState, snapState, toRemove, succeeded, originalTools, hadFailure)
	curState.SchemaPath = snapState.SchemaPath
	curState.SchemaModifiedAt = snapState.SchemaModifiedAt

	if err := ls.Save(); err != nil {
		log.Default.Error("save state after undo", "error", err)
		return exitWithCode(3)
	}

	if hadFailure {
		log.Default.Error("undo: some removals failed, manual cleanup may be needed")
		return exitWithCode(1)
	}

	log.Default.Info("undo complete", "tools_removed", len(toRemove))
	return nil
}
