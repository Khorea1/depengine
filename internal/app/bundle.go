package app

import (
	"fmt"
	"os"

	"github.com/Khorea1/depengine/internal/bundle"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/spf13/cobra"
)

func newBundleCmd() *cobra.Command {
	root := &cobra.Command{
		Use:     "bundle",
		Short:   ifPT("Exportar artefatos fixados para uso offline", "Manage offline artifact bundles"),
		Example: `  depengine bundle export --output ./offline-bundle`,
		GroupID: groupExport,
		RunE:    func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	root.AddCommand(newBundleExportCmd())
	return root
}

func newBundleExportCmd() *cobra.Command {
	lockPath := new(string)
	output := new(string)
	cmd := &cobra.Command{
		Use:     "export",
		Short:   ifPT("Exportar artefatos do lock v2", "Export artifacts from a v2 lockfile"),
		Example: `  depengine bundle export --lock depengine.lock --output ./offline-bundle`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := *lockPath
			if path == "" {
				path = "depengine.lock"
			}
			lk, err := lock.Load(path)
			if err != nil {
				return fmt.Errorf("load lockfile: %w", err)
			}
			if lk == nil || lk.Version != lock.CurrentVersion {
				return fmt.Errorf("bundle export requires a readable v2 lockfile")
			}
			doc, err := lk.ProjectionDocument()
			if err != nil {
				return err
			}
			if *output == "" {
				return fmt.Errorf("--output is required")
			}
			if err := bundle.ExportContext(cmd.Context(), *output, doc); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(os.Stdout, "Bundle exported to %s\n", *output); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(lockPath, "lock", "", "v2 lockfile to read (default depengine.lock)")
	cmd.Flags().StringVarP(output, "output", "o", "", "new bundle directory to create")
	return cmd
}
