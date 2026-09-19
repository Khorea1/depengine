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
	"github.com/Khorea1/depengine/pkg/run"
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
		os.Exit(3)
	}

	// Optionally load schema for validation.
	var schemaTools map[string]*config.Tool
	if *removeSchema != "" {
		s, _, _, err := loadSchema(*removeSchema)
		if err != nil {
			log.Default.Error("load schema", "error", err)
			os.Exit(2)
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

	removeTool := func(toolName string) bool {
		// If schema is loaded, validate tool exists (warn but continue).
		if schemaTools != nil {
			if _, ok := schemaTools[toolName]; !ok {
				log.Default.Warn("tool not found in schema, removing from state anyway", "tool", toolName)
			}
		}

		toolState, ok := st.Tools[toolName]
		if !ok {
			log.Default.Warn("tool not installed, nothing to remove", "tool", toolName)
			return false
		}
		methodKind := toolState.MethodKind
		if methodKind == "" {
			methodKind = toolState.Method // fallback for old state files
		}

		adapter := exec.Lookup(methodKind)
		if adapter == nil {
			log.Default.Warn("adapter not found for method", "tool", toolName, "method", toolState.Method, "methodKind", methodKind)
			log.Default.Warn("manual remove required", "tool", toolName)
			return false
		}

		if !exec.CanRemove(adapter) {
			log.Default.Warn("manual remove required", "tool", toolName, "method", toolState.Method, "methodKind", methodKind)
			return false
		}

		remover := adapter.(exec.Remover)
		mc := &config.MethodCandidate{
			Kind:   methodKind,
			Config: toolState.Config,
		}
		tool := &config.Tool{Name: toolName}

		if *removeDryRun {
			log.Default.Info("would remove", "tool", toolName, "method", toolState.Method)
			return true
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := remover.Remove(ctx, run.OSExecRunner{}, tool, mc); err != nil {
			log.Default.Error("remove failed", "tool", toolName, "error", err)
			return false
		}

		log.Default.Info("removed", "tool", toolName, "method", toolState.Method)
		delete(st.Tools, toolName)
		return true
	}

	if *removeAll && !*removeForce {
		if !isInteractive() {
			log.Default.Error("stdin is not a terminal; use --force to confirm, or run in an interactive terminal")
			os.Exit(2)
		}
		fmt.Fprint(os.Stderr, "WARNING: This will remove ALL installed tools tracked by depengine.\nAre you sure? [y/N] ")
		input, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Fprintln(os.Stderr, "Aborted.")
			os.Exit(0)
		}
	}

	hadFailure := false

	if *removeAll {
		for toolName := range st.Tools {
			if !removeTool(toolName) {
				hadFailure = true
			}
		}
	} else if *removeOnly != "" {
		if !removeTool(*removeOnly) {
			hadFailure = true
		}
	} else if len(removeArgs) > 0 {
		for _, toolName := range removeArgs {
			if !removeTool(toolName) {
				hadFailure = true
			}
		}
	} else {
		log.Default.Error("usage: depengine remove [--all | --only=<tool> | <tool>...] [--schema=<path>]")
		if ls != nil {
			_ = ls.Close()
		}
		os.Exit(1)
	}

	if !*removeDryRun {
		if err := ls.Save(); err != nil {
			log.Default.Error("failed to update state", "error", err)
			_ = ls.Close()
			os.Exit(3)
		}
	}

	if hadFailure {
		if ls != nil {
			_ = ls.Close()
		}
		os.Exit(1)
	}
}

func isInteractive() bool {
	fi, _ := os.Stdin.Stat()
	return fi != nil && fi.Mode()&os.ModeCharDevice != 0
}
