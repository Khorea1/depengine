package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

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
		Long:    ifPT("Remove o rastreamento da ferramenta; o sistema não é alterado. Também descarta um registro de ownership de pré-requisito com zero referências deixado após a remoção dos dependentes, e recusa enquanto ferramentas rastreadas ainda o exigirem.", "Drop the tool's tracking; the system is untouched. Also drops a zero-reference prerequisite ownership record left after its dependents were removed, and refuses while tracked tools still require it."),
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
		if err := forgetOrphanedPrerequisite(st, toolName); err != nil {
			log.Default.Error("forget", "tool", toolName, "error", err)
			return exitWithCode(1)
		}
	} else {
		// Forget drops tracking, not host resources. Release the tool's claims so
		// a shared source or prerequisite can reach zero references; the zero-ref
		// record itself is retained because nothing was uninstalled.
		release, err := plan.ReleaseDependentResources(st.OwnedResources, toolName)
		if err != nil {
			log.Default.Error("release owned resources", "tool", toolName, "error", err)
			return exitWithCode(1)
		}
		st.OwnedResources = release.Updated
		if resource, err := plan.PrerequisiteResource(toolName); err == nil {
			for _, owned := range st.OwnedResources {
				if owned.Resource == resource && owned.RefCount() > 0 {
					log.Default.Warn("forgetting a prerequisite still required by tracked tools; its ownership record is retained", "tool", toolName, "dependents", strings.Join(owned.Dependents, ","))
					break
				}
			}
		}
		delete(st.Tools, toolName)
	}
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

// forgetOrphanedPrerequisite drops a zero-reference prerequisite ownership
// record whose tool is no longer tracked. This is the manual-cleanup path for
// a helper retained after its last dependent was removed. It refuses while
// tracked tools still claim the helper and never touches the host.
func forgetOrphanedPrerequisite(st *state.State, toolName string) error {
	resource, err := plan.PrerequisiteResource(toolName)
	if err != nil {
		return errors.New("tool not found in state")
	}
	for _, owned := range st.OwnedResources {
		if owned.Resource != resource {
			continue
		}
		if owned.Ownership != plan.OwnershipDepengine {
			return errors.New("ownership is not depengine-managed")
		}
		if owned.RefCount() != 0 {
			return fmt.Errorf("prerequisite is still required by tracked tools: %s", strings.Join(owned.Dependents, ","))
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
	return errors.New("tool not found in state")
}
