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

func runUndo(ctx context.Context, undoList *bool, undoSpecific *string) error {
	if *undoList {
		return listUndoSnapshots()
	}

	snapPath, err := resolveUndoSnapshot(*undoSpecific)
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

	toRemove := undoRemovals(curState, snapState)

	if len(toRemove) == 0 {
		// Do not restore snapshot state here: tools the user deliberately
		// removed after the snapshot exist in snapState but not in curState;
		// restoring snapState.Tools would reintroduce phantom entries.
		log.Default.Info("nothing to undo (no tools were added since snapshot)")
		return nil
	}

	configureUndoNativeAdapter()
	originalTools := cloneToolStates(curState.Tools)
	succeeded, hadFailure := removeUndoTools(ctx, curState, toRemove)
	if hadFailure {
		log.Default.Error("undo: some removals failed — saving partial state")
	}

	if hadFailure {
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
	} else {
		curState.Tools = snapState.Tools
	}
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

func listUndoSnapshots() error {
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
	for i, snapshot := range snapshots {
		fmt.Fprintf(c.w, "  %s  %s  %s  %s\n",
			c.cyan(padRight(fmt.Sprintf("%d", i+1), idxW)),
			padRight(relativeTime(snapshot.Timestamp), 14),
			c.dim(filepath.Base(snapshot.Path)),
			c.dim(fmt.Sprintf("(%s)", plural(snapshot.ToolCount, "tool"))))
	}
	return nil
}

func resolveUndoSnapshot(request string) (string, error) {
	if request == "" {
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
	index, err := strconv.Atoi(request)
	if err != nil {
		return request, nil // Backward-compatible explicit snapshot path.
	}
	snapshots, err := state.ListSnapshots()
	if err != nil {
		log.Default.Error("list snapshots", "error", err)
		return "", exitWithCode(3)
	}
	if index < 1 || index > len(snapshots) {
		log.Default.Error("invalid snapshot index", "index", index, "max", len(snapshots))
		return "", exitWithCode(2)
	}
	return snapshots[index-1].Path, nil
}

func undoRemovals(current, snapshot *state.State) []string {
	tools := make([]string, 0)
	for name := range current.Tools {
		if _, exists := snapshot.Tools[name]; !exists {
			tools = append(tools, name)
		}
	}
	return tools
}

func configureUndoNativeAdapter() {
	if facts, err := engine.GatherFacts(run.OSExecRunner{}); err == nil {
		exec.Replace(exec.NewNativeAdapter(engine.ResolveFamily(facts)))
		return
	} else {
		log.Default.Warn("could not gather OS facts; falling back to PATH-probing for native manager detection", "error", err)
	}
}

func cloneToolStates(tools map[string]state.ToolState) map[string]state.ToolState {
	clone := make(map[string]state.ToolState, len(tools))
	for name, tool := range tools {
		clone[name] = tool
	}
	return clone
}

func removeUndoTools(ctx context.Context, current *state.State, names []string) (map[string]bool, bool) {
	succeeded := make(map[string]bool, len(names))
	hadFailure := false
	for _, name := range names {
		if !removeUndoTool(ctx, name, current.Tools[name]) {
			hadFailure = true
			continue
		}
		succeeded[name] = true
	}
	return succeeded, hadFailure
}

func removeUndoTool(ctx context.Context, name string, toolState state.ToolState) bool {
	log.Default.Info("removing tool added after snapshot", "tool", name, "method", toolState.Method)
	methodKind := toolState.MethodKind
	if methodKind == "" {
		methodKind = toolState.Method
	}
	adapter := exec.Lookup(methodKind)
	if adapter == nil {
		log.Default.Warn("adapter not found — manual removal may be needed", "tool", name, "method", toolState.Method, "methodKind", methodKind)
		return false
	}
	if !exec.CanRemove(adapter) {
		log.Default.Warn("adapter does not support automated removal — manual removal needed", "tool", name, "method", toolState.Method, "methodKind", methodKind)
		return false
	}
	removeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := adapter.(exec.Remover).Remove(removeCtx, run.OSExecRunner{}, &config.Tool{Name: name}, &config.MethodCandidate{Kind: methodKind, Config: toolState.Config})
	if err != nil {
		log.Default.Error("remove failed during undo", "tool", name, "error", err)
		return false
	}
	log.Default.Info("removed during undo", "tool", name)
	return true
}
