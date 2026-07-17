package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"depengine/pkg/exec"
	"depengine/pkg/log"
	"depengine/pkg/run"
	"depengine/pkg/schema"
	"depengine/pkg/state"
)

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
			fmt.Println("No snapshots available.")
			return
		}
		fmt.Println("Available snapshots:")
		for _, s := range snapshots {
			ts := s.Timestamp.Format("2006-01-02 15:04:05")
			fmt.Printf("  %s  %s  (%d tools)\n", ts, filepath.Base(s.Path), s.ToolCount)
		}
		return
	}

	var snapPath string
	if *undoSpecific != "" {
		snapPath = *undoSpecific
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
		log.Default.Info("nothing to undo (no tools were added since snapshot)")

		curState.Tools = snapState.Tools
		curState.SchemaPath = snapState.SchemaPath
		curState.SchemaModifiedAt = snapState.SchemaModifiedAt
		if err := ls.Save(); err != nil {
			log.Default.Error("save state after undo", "error", err)
			ls.Close()
			os.Exit(3)
		}
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

		adapter := exec.Lookup(toolState.Method)
		if adapter == nil {
			log.Default.Warn("adapter not found — manual removal may be needed", "tool", name, "method", toolState.Method)
			hadFailure = true
			continue
		}

		if !exec.CanRemove(adapter) {
			log.Default.Warn("adapter does not support automated removal — manual removal needed", "tool", name, "method", toolState.Method)
			hadFailure = true
			continue
		}

		remover := adapter.(exec.Remover)
		mc := &schema.MethodCandidate{
			Kind:   toolState.Method,
			Config: toolState.Config,
		}
		tool := &schema.Tool{Name: name}

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
