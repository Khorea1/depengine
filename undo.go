package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/state"
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
func runUndo(args []string) {
	undoCmd := flag.NewFlagSet("undo", flag.ExitOnError)
	undoList := undoCmd.Bool("list", false, "list available snapshots")
	undoSpecific := undoCmd.String("snapshot", "", "revert to specific snapshot file path")
	undoCmd.Parse(args)

	if *undoList {
		snapshots, err := state.ListSnapshots()
		if err != nil {
			log.Default.Error("list snapshots", "error", err)
			os.Exit(3)
		}
		if len(snapshots) == 0 {
			fmt.Fprintln(os.Stderr, "No snapshots available.")
			return
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
		return
	}

	var snapPath string
	if *undoSpecific != "" {
		if n, err := strconv.Atoi(*undoSpecific); err == nil {
			// Treat as index (1-based)
			snapshots, listErr := state.ListSnapshots()
			if listErr != nil {
				log.Default.Error("list snapshots", "error", listErr)
				os.Exit(3)
			}
			if n < 1 || n > len(snapshots) {
				log.Default.Error("invalid snapshot index", "index", n, "max", len(snapshots))
				os.Exit(2)
			}
			snapPath = snapshots[n-1].Path
		} else {
			// Treat as file path (backward compat)
			snapPath = *undoSpecific
		}
	} else {
		snapshots, err := state.ListSnapshots()
		if err != nil {
			log.Default.Error("list snapshots", "error", err)
			os.Exit(3)
		}
		if len(snapshots) == 0 {
			log.Default.Error("no snapshot available for undo")
			os.Exit(1)
		}
		snapPath = snapshots[0].Path
	}

	snapState, err := state.LoadSnapshot(snapPath)
	if err != nil {
		log.Default.Error("load snapshot", "error", err)
		os.Exit(3)
	}

	ls, err := state.LoadLocked()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()

	curState := ls.State()

	var toRemove []string
	for name := range curState.Tools {
		if _, ok := snapState.Tools[name]; !ok {
			toRemove = append(toRemove, name)
		}
	}

	if len(toRemove) == 0 {
		// Do not restore snapshot state here: tools the user deliberately
		// removed after the snapshot exist in snapState but not in curState;
		// restoring snapState.Tools would reintroduce phantom entries.
		log.Default.Info("nothing to undo (no tools were added since snapshot)")
		return
	}

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

		methodKind := toolState.MethodKind
		if methodKind == "" {
			methodKind = toolState.Method // fallback for old state files
		}

		adapter := exec.Lookup(methodKind)
		if adapter == nil {
			log.Default.Warn("adapter not found — manual removal may be needed", "tool", name, "method", toolState.Method, "methodKind", methodKind)
			hadFailure = true
			continue
		}

		if !exec.CanRemove(adapter) {
			log.Default.Warn("adapter does not support automated removal — manual removal needed", "tool", name, "method", toolState.Method, "methodKind", methodKind)
			hadFailure = true
			continue
		}

		remover := adapter.(exec.Remover)
		mc := &config.MethodCandidate{
			Kind:   methodKind,
			Config: toolState.Config,
		}
		tool := &config.Tool{Name: name}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if err := remover.Remove(ctx, run.OSExecRunner{}, tool, mc); err != nil {
			log.Default.Error("remove failed during undo", "tool", name, "error", err)
			hadFailure = true
			cancel()
			continue
		}
		cancel()

		log.Default.Info("removed during undo", "tool", name)
		succeeded[name] = true
	}
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
		ls.Close()
		os.Exit(3)
	}

	if hadFailure {
		log.Default.Error("undo: some removals failed, manual cleanup may be needed")
		ls.Close()
		os.Exit(1)
	}

	log.Default.Info("undo complete", "tools_removed", len(toRemove))

}
