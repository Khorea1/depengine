package app

import (
	"context"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSBOMContext(cmd.Context(), sbomFormat)
		},
	}
	cmd.Flags().StringVar(sbomFormat, "format", "cyclonedx", "output format: cyclonedx or spdx")
	return cmd
}

func runSBOM(sbomFormat *string) error { return runSBOMContext(context.Background(), sbomFormat) }

func runSBOMContext(ctx context.Context, sbomFormat *string) error {
	ls, err := state.LoadSharedContext(ctx)
	if err != nil {
		log.Default.Error("load state", "error", err)
		return exitWithCode(3)
	}
	defer func() { _ = ls.Close() }()

	st := ls.State()

	// Fill missing state versions from the authoritative lock format. V2
	// projection data is decoded once and never falls back to its legacy payload.
	if st.SchemaPath != "" {
		if lk, lerr := lock.Load(lock.DefaultPath(st.SchemaPath)); lerr == nil && lk != nil {
			var projection plan.LockDocument
			hasProjection := false
			if lk.Version == lock.CurrentVersion {
				if document, err := lk.ProjectionDocument(); err == nil {
					projection = document
					hasProjection = true
				}
			}
			for name, ts := range st.Tools {
				if ts.Version != "" {
					continue
				}
				if lk.Version == lock.CurrentVersion {
					if hasProjection {
						if entry, ok := projection.EntryForTool(name); ok && entry.Stability == plan.LockImmutable && entry.Identity.Version != "" && ts.MethodKind != "" && entry.Candidate.Method == ts.MethodKind {
							ts.Version = entry.Identity.Version
							st.Tools[name] = ts
						}
					}
					continue
				}
				if pin, ok := legacyV1PinFor(lk, name, ts.MethodKind); ok && pin.Latest != "" {
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
