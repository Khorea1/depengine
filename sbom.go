package main

import (
	"fmt"
	"os"

	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/sbom"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

// newSBOMCmd builds `depengine sbom`.
func newSBOMCmd() *cobra.Command {
	sbomFormat := new(string)

	cmd := &cobra.Command{
		Use:     "sbom",
		Short:   ifPT("Exportar um SBOM do estado instalado", "Export a software bill of materials of installed state"),
		GroupID: groupExport,
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			return runSBOM(sbomFormat)
		},
	}
	cmd.Flags().StringVar(sbomFormat, "format", "cyclonedx", "output format: cyclonedx or spdx")
	return cmd
}

func runSBOM(sbomFormat *string) error {
	ls, err := state.LoadShared()
	if err != nil {
		log.Default.Error("load state", "error", err)
		return exitWithCode(3)
	}
	defer ls.Close()

	st := ls.State()

	// Fall back to lock pins for tools whose recorded version is empty
	// (e.g. state files written before version tracking): 0.0.0 should only
	// appear when nothing is knowable.
	if st.SchemaPath != "" {
		if lk, lerr := lock.Load(lock.DefaultPath(st.SchemaPath)); lerr == nil && lk != nil {
			for name, ts := range st.Tools {
				if ts.Version != "" {
					continue
				}
				if pin, ok := lockPinFor(lk, name, ts.MethodKind); ok && pin.Latest != "" {
					ts.Version = pin.Latest
					st.Tools[name] = ts
				}
			}
		}
	}

	var data []byte
	switch *sbomFormat {
	case "cyclonedx", "cyclonedx-json":
		data, err = sbom.ExportCycloneDX(st)
	case "spdx", "spdx-json":
		data, err = sbom.ExportSPDX(st)
	default:
		log.Default.Error("unsupported format", "format", *sbomFormat)
		fmt.Fprintf(os.Stderr, "Formatos suportados: cyclonedx, spdx\n")
		return exitWithCode(2)
	}

	if err != nil {
		log.Default.Error("generate sbom", "error", err)
		return exitWithCode(3)
	}

	fmt.Println(string(data))
	return nil
}
