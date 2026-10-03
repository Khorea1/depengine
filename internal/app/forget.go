package app

import (
	"context"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return runForgetContext(cmd.Context(), args[0])
		},
	}
}

func runForgetContext(ctx context.Context, toolName string) error {
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

	delete(st.Tools, toolName)
	if err := ls.Save(); err != nil {
		log.Default.Error("save state", "error", err)
		return exitWithCode(3)
	}

	log.Default.Info("forgotten", "tool", toolName)
	return nil
}
