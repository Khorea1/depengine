package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/selfupdate"
	"github.com/spf13/cobra"
)

// newSelfCmd groups commands that manage the depengine executable itself.
func newSelfCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "self",
		Short:   ifPT("Gerenciar o próprio executável do depengine", "Manage the depengine executable itself"),
		GroupID: groupManage,
	}
	cmd.AddCommand(newSelfUpdateCmd())
	return cmd
}

func newSelfUpdateCmd() *cobra.Command {
	repo := new(string)
	ref := new(string)
	binDir := new(string)
	recipe := new(string)
	dryRun := new(bool)
	check := new(bool)
	jsonOut := new(bool)
	format := new(string)

	cmd := &cobra.Command{
		Use:   "update",
		Short: ifPT("Recompilar e substituir o executável em uso", "Rebuild and replace the running executable"),
		Long: ifPT(
			"Recompila o depengine a partir de um checkout Git e substitui o executável em uso de forma atômica. O clone e o build acontecem em um diretório temporário; o binário anterior só é trocado depois que o novo compila e responde a 'version'. Use --check para comparar a revisão em uso com a remota sem instalar, e --dry-run para resolver a origem sem compilar.",
			"Rebuild depengine from a Git checkout and atomically replace the running executable. Cloning and building happen in a temporary directory; the previous binary is replaced only after the new build compiles and answers 'version'. Use --check to compare the running revision with the remote without installing, and --dry-run to resolve the source without building.",
		),
		Example: `  depengine self update
  depengine self update --check
  depengine self update --ref master --bin-dir "$HOME/.local/bin"
  depengine self update --recipe recipes/depengine.toml.example --dry-run
  depengine self update --dry-run --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := normalizeOutputFormat(format, jsonOut); err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
				return exitWithCode(2)
			}
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			result, err := selfupdate.Run(ctx, run.OSExecRunner{}, selfupdate.Options{
				Repo:   *repo,
				Ref:    *ref,
				BinDir: *binDir,
				Recipe: *recipe,
				DryRun: *dryRun,
				Check:  *check,
			})
			if err != nil {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
				return exitWithCode(1)
			}
			if *format == "json" {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(result); err != nil {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
					return exitWithCode(1)
				}
			} else {
				writeSelfUpdateText(cmd.OutOrStdout(), result)
			}
			if *check && result.UpdateAvailable {
				return exitWithCode(1)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(repo, "repo", "", ifPT("repositório Git de origem (padrão: receita)", "Git source repository (default: the recipe's source)"))
	f.StringVar(ref, "ref", "", "branch, tag, or commit to build (default: remote default branch)")
	f.StringVar(binDir, "bin-dir", "", "destination directory (default: the running executable's directory)")
	f.StringVar(recipe, "recipe", "", ifPT("fragmento de schema de projeto que declara o método git (padrão: receita embutida)", "project-schema fragment declaring the git method (default: embedded recipe)"))
	f.BoolVar(check, "check", false, ifPT("comparar revisões sem compilar; sai 1 quando há atualização", "compare revisions without building; exits 1 when an update is available"))
	f.BoolVar(dryRun, "dry-run", false, ifPT("resolver a origem sem compilar ou substituir", "resolve the source without building or replacing"))
	f.BoolVar(jsonOut, "json", false, "shorthand for --format=json")
	f.StringVar(format, "format", "text", "output format: text or json")
	return cmd
}

func writeSelfUpdateText(w io.Writer, result selfupdate.Result) {
	ref := result.Ref
	if ref == "" {
		ref = "default branch"
	}
	writef(w, "Self-update: %s (%s)\n", result.Repo, ref)
	if result.Recipe != "" {
		writef(w, "  recipe:   %s\n", result.Recipe)
	}
	writef(w, "  revision: %s\n", valueOrUnknown(result.Revision))
	current := valueOrUnknown(result.CurrentRevision)
	if result.CurrentModified {
		current += " (modified)"
	}
	writef(w, "  current:  %s\n", current)
	writef(w, "  target:   %s\n", result.Target)
	switch {
	case result.Replaced:
		writef(w, "  status:   replaced (%s)\n", result.BuiltVersion)
	case result.UpdateAvailable:
		writef(w, "  status:   update available\n")
	default:
		writef(w, "  status:   up to date\n")
	}
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}
