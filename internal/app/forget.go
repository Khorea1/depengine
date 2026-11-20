package app

import (
	"context"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

// newForgetCmd builds `depengine forget`.
func newForgetCmd() *cobra.Command {
	forgetJSON := new(bool)
	forgetFormat := new(string)
	cmd := &cobra.Command{
		Use:     "forget <tool>",
		Short:   ifPT("Esquecer uma ferramenta sem tentar removê-la do sistema", "Forget a tool from state without removing it from the system"),
		Example: `  depengine forget nvim   # drop install state; the system is untouched`,
		GroupID: groupManage,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := normalizeOutputFormat(forgetFormat, forgetJSON); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return exitWithCode(2)
			}
			return runForgetContext(cmd.Context(), args[0], *forgetJSON)
		},
	}
	cmd.Flags().BoolVar(forgetJSON, "json", false, "shorthand for --format=json")
	cmd.Flags().StringVar(forgetFormat, "format", "text", "output format: text or json")
	return cmd
}

func runForgetContext(ctx context.Context, toolName string, jsonOut bool) error {
	ls, err := state.LoadLockedContext(ctx)
	if err != nil {
		log.Default.Error("state lock", "error", err)
		return exitWithCode(3)
	}
	defer func() { _ = ls.Close() }()
	st := ls.State()
	if _, ok := st.Tools[toolName]; !ok {
		log.Default.Error("tool not found in state", "tool", toolName)
		return exitWithCode(1)
	}

	// Forget drops tracking, not host resources. Release the tool's claims so a
	// shared source or prerequisite can reach zero references; the zero-ref
	// record itself is retained because nothing was uninstalled.
	release, err := plan.ReleaseDependentResources(st.OwnedResources, toolName)
	if err != nil {
		log.Default.Error("release owned resources", "tool", toolName, "error", err)
		return exitWithCode(1)
	}
	st.OwnedResources = release.Updated
	delete(st.Tools, toolName)
	if err := ls.Save(); err != nil {
		log.Default.Error("save state", "error", err)
		return exitWithCode(3)
	}

	log.Default.Info("forgotten", "tool", toolName)
	if jsonOut {
		if err := writeMachineOutput(os.Stdout, "forget", map[string]string{"tool": toolName}); err != nil {
			return exitWithCode(3)
		}
	}
	return nil
}
