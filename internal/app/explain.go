package app

import (
	"fmt"
	"os"
	"strings"

	"github.com/Khorea1/depengine/internal/explain"
	"github.com/spf13/cobra"
)

type explainReport struct {
	Query   string          `json:"query"`
	Matches []explain.Entry `json:"matches"`
}

// newExplainCmd builds `depengine explain`: offline answers about schema
// fields, installation methods, and the error vocabulary.
func newExplainCmd() *cobra.Command {
	format := new(string)
	jsonFlag := new(bool)

	cmd := &cobra.Command{
		Use:   "explain [term...]",
		Short: ifPT("Explica campos, métodos e códigos de erro", "Explain fields, methods, and error codes"),
		Long:  ifPT("Responde o vocabulário do depengine sem sair do terminal: campos do schema, métodos de instalação e códigos de erro, com forma válida, exemplo mínimo e o documento correspondente. Não lê o schema, a máquina nem a rede.", "Answers depengine's vocabulary without leaving the terminal: schema fields, installation methods, and error codes, with the valid form, a minimal example, and the matching document. It reads neither the project schema, the host, nor the network."),
		Example: `  depengine explain                 # list every known term
  depengine explain requires_when   # one field
  depengine explain http            # one method
  depengine explain E_DUPE_TOOL     # one error code
  depengine explain --json http     # machine-readable answer`,
		GroupID: groupInspect,
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := normalizeOutputFormat(format, jsonFlag); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return exitWithCode(2)
			}
			catalog, err := explain.Load()
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: load vocabulary: %v\n", err)
				return exitWithCode(3)
			}
			return runExplain(catalog, args, *format == "json")
		},
	}
	cmd.Flags().StringVar(format, "format", "text", "output format: text or json")
	cmd.Flags().BoolVar(jsonFlag, "json", false, "shorthand for --format=json")
	return cmd
}

// runExplain resolves every requested term and renders the result.
func runExplain(catalog *explain.Catalog, terms []string, jsonOutput bool) error {
	report := explainReport{Matches: catalog.Entries()}
	if len(terms) > 0 {
		report.Matches = nil
		var missing []string
		for _, term := range terms {
			matches := catalog.Lookup(term)
			if len(matches) == 0 {
				missing = append(missing, term)
				continue
			}
			report.Matches = append(report.Matches, matches...)
		}
		if len(missing) > 0 {
			if jsonOutput {
				_ = writeMachineOutput(os.Stdout, "explain", explainReport{Query: strings.Join(missing, " ")})
			}
			for _, term := range missing {
				fmt.Fprintf(os.Stderr, "error: no vocabulary entry for %q\n", term)
				if suggestions := catalog.Suggest(term); len(suggestions) > 0 {
					fmt.Fprintf(os.Stderr, "did you mean: %s\n", strings.Join(suggestions, ", "))
				}
			}
			fmt.Fprintln(os.Stderr, "run `depengine explain` to list every term")
			return exitWithCode(2)
		}
		report.Query = strings.Join(terms, " ")
	}
	if jsonOutput {
		return writeMachineOutput(os.Stdout, "explain", report)
	}
	renderExplainText(report)
	return nil
}

func renderExplainText(report explainReport) {
	var out strings.Builder
	if len(report.Matches) == 0 {
		out.WriteString("No vocabulary entries.\n")
		_, _ = os.Stderr.WriteString(out.String())
		return
	}
	group := ""
	for i, entry := range report.Matches {
		if entry.Group != group {
			if i > 0 {
				out.WriteString("\n")
			}
			group = entry.Group
			out.WriteString(groupTitle(group) + "\n")
		}
		out.WriteString("  " + entry.Term + " — " + entry.Where + "\n")
		out.WriteString("    " + entry.Summary + "\n")
		out.WriteString("    form:    " + entry.Form + "\n")
		if entry.Example != "" {
			lines := strings.Split(entry.Example, "\n")
			out.WriteString("    example: " + lines[0] + "\n")
			for _, line := range lines[1:] {
				out.WriteString("             " + line + "\n")
			}
		}
		out.WriteString("    docs:    " + entry.Doc + "\n")
	}
	_, _ = os.Stderr.WriteString(out.String())
}

func groupTitle(group string) string {
	switch group {
	case explain.GroupField:
		return "fields"
	case explain.GroupMethod:
		return "methods"
	case explain.GroupErrorCode:
		return "error codes"
	case explain.GroupErrorClass:
		return "resolver error classes"
	default:
		return group
	}
}
