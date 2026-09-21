package main

import (
	"os"

	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

// newForgetCmd builds `depengine forget`.
func newForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "forget <tool>",
		Short:   ifPT("Esquecer uma ferramenta sem tentar removê-la do sistema", "Forget a tool from state without removing it from the system"),
		GroupID: groupManage,
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runForget(args[0])
			return nil
		},
	}
}

// runForget removes a tool from state without attempting system removal.
// Body unchanged from the pre-Cobra version — Cobra's cobra.ExactArgs(1)
// now enforces the argument count that the old manual length check did.
func runForget(toolName string) {
	ls, err := state.LoadLocked()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()
	st := ls.State()
	if _, ok := st.Tools[toolName]; !ok {
		log.Default.Error("tool not found in state", "tool", toolName)
		closeStateAndExit(ls, 1)
	}

	delete(st.Tools, toolName)
	if err := ls.Save(); err != nil {
		log.Default.Error("save state", "error", err)
		closeStateAndExit(ls, 3)
	}

	log.Default.Info("forgotten", "tool", toolName)
}
