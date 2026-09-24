package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/graph"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/spf13/cobra"
)

// newGraphCmd builds `depengine graph`.
func newGraphCmd() *cobra.Command {
	graphSchema := new(string)
	graphManifest := new(string)
	graphNoManifest := new(bool)
	graphFormat := new(string)
	graphProfile := new(string)
	graphOnly := new(string)
	graphSkip := new(string)

	cmd := &cobra.Command{
		Use:     "graph",
		Short:   ifPT("Mostrar o grafo de dependências", "Show the dependency graph"),
		GroupID: groupInspect,
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			return runGraph(graphSchema, graphManifest, graphNoManifest, graphFormat, graphProfile, graphOnly, graphSkip)
		},
	}
	f := cmd.Flags()
	f.StringVar(graphSchema, "schema", defaultSchemaPath(), "path to schema.toml")
	f.StringVar(graphManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(graphNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	f.StringVar(graphFormat, "format", "text", "output format: mermaid, dot, text")
	f.StringVar(graphProfile, "profile", "", "only show tools with matching tag")
	f.StringVar(graphOnly, "only", "", "only show subgraph for specific tool")
	f.StringVar(graphSkip, "skip", "", "skip specific tools (comma-separated)")
	return cmd
}

func runGraph(graphSchema, graphManifest *string, graphNoManifest *bool, graphFormat, graphProfile, graphOnly, graphSkip *string) error {
	switch *graphFormat {
	case "mermaid", "dot", "text":
	default:
		fmt.Fprintf(os.Stderr, "error: unknown format %q (valid: mermaid, dot, text)\n", *graphFormat)
		return exitWithCode(2)
	}

	s, err := config.ParseProjectSchema(*graphSchema, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitWithCode(exitCodeForError(err))
	}

	noManifest := *graphNoManifest
	manifestPath := *graphManifest
	manifestAuto := false
	if !noManifest && manifestPath == "" {
		manifestPath = config.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}
	if manifestPath != "" {
		var count int
		s, count, err = mergeManifest(s, manifestPath, false)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading manifest: %v\n", err)
			return exitWithCode(2)
		}
		if count > 0 && manifestAuto {
			fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, count)
		}
	}
	s.Tools = filterTools(s.Tools, *graphOnly, *graphSkip, *graphProfile)

	if len(s.Tools) == 0 {
		fmt.Fprintln(os.Stderr, "no tools matching filters")
		return nil
	}

	declaredGraph := graph.BuildDeclaredGraph(s.Tools)
	levels, err := graph.SortGraph(declaredGraph)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitWithCode(2)
	}

	if *graphFormat == "text" {
		// Text format is the human-facing one — give it a header naming the
		// working set, so a long level list doesn't start mid-air. Mermaid and
		// dot are machine-consumed; no decoration there.
		c := newCLIStyle(os.Stderr)
		fmt.Fprintf(c.w, "%s\n\n", c.dim(fmt.Sprintf("%d tools in %d levels", len(s.Tools), len(levels))))
	}
	switch *graphFormat {
	case "mermaid":
		fmt.Print(graph.RenderMermaidGraph(declaredGraph))
	case "dot":
		fmt.Print(graph.RenderDOTGraph(declaredGraph))
	case "text":
		fmt.Print(graph.RenderTextGraph(levels, declaredGraph))
	}
	return nil
}

// newWhyCmd builds `depengine why`.
func newWhyCmd() *cobra.Command {
	whySchema := new(string)
	whyManifest := new(string)
	whyNoManifest := new(bool)
	whyJSON := new(bool)
	whyFields := new(bool)

	cmd := &cobra.Command{
		Use:     "why <tool>",
		Short:   ifPT("Explicar como uma ferramenta seria instalada", "Explain how a tool would be installed"),
		GroupID: groupInspect,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWhy(cmd.Context(), args[0], whySchema, whyManifest, whyNoManifest, whyJSON, whyFields)
		},
	}
	f := cmd.Flags()
	f.StringVar(whySchema, "schema", defaultSchemaPath(), "path to schema.toml")
	f.StringVar(whyManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(whyNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	f.BoolVar(whyJSON, "json", false, "JSON output")
	f.BoolVar(whyFields, "fields", false, "show field-level provenance")
	return cmd
}

func formatWhyIntent(intent map[string]string) string {
	if len(intent) == 0 {
		return ""
	}
	keys := make([]string, 0, len(intent))
	for key := range intent {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+intent[key])
	}
	return strings.Join(parts, ", ")
}

// runWhy shows why a tool would be installed via each candidate method,
// without actually installing anything. Useful for debugging complex
// schemas. Body unchanged from the pre-Cobra version — Cobra's
// cobra.ExactArgs(1) now enforces the argument count that the old manual
// length check did, and toolName arrives as a plain argument instead of
// remain[0].
func runWhy(ctx context.Context, toolName string, whySchema, whyManifest *string, whyNoManifest, whyJSON, whyFields *bool) error {
	s, err := config.ParseProjectSchema(*whySchema, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return exitWithCode(1)
	}

	noManifest := *whyNoManifest
	manifestPath := *whyManifest
	manifestAuto := false
	if !noManifest && manifestPath == "" {
		manifestPath = config.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}
	if manifestPath != "" {
		var count int
		s, count, err = mergeManifest(s, manifestPath, *whyFields)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error loading manifest: %v\n", err)
			return exitWithCode(2)
		}
		if count > 0 && manifestAuto {
			fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, count)
		}
	}
	facts, factsErr := engine.GatherFacts(run.OSExecRunner{})
	clan := ""
	if factsErr == nil {
		clan = engine.ResolveFamily(facts)
	}
	if helper := s.Defaults.AurHelper; helper != "" {
		ecosystem.ReconfigureAUR(helper)
	}

	if warnings, verr := config.Validate(s, exec.RegisteredKinds()); verr != nil {
		log.Default.Error("schema validation", "error", verr)
		return exitWithCode(exitCodeForError(verr))
	} else if len(warnings) > 0 {
		for _, w := range warnings {
			log.Default.Warn(w)
		}
	}

	tool, ok := s.Tools[toolName]
	if !ok {
		log.Default.Error("tool not found in schema", "tool", toolName)
		return exitWithCode(1)
	}

	ex := exec.New()
	exec.WithRunner(run.OSExecRunner{})(ex)
	exec.WithFacts(facts)(ex)
	attempts := ex.ExplainTool(ctx, tool, clan)
	if *whyJSON {
		type jsonAttempt struct {
			Kind       string                    `json:"kind"`
			Label      string                    `json:"label,omitempty"`
			Status     string                    `json:"status"`
			Reason     string                    `json:"reason,omitempty"`
			Intent     map[string]string         `json:"intent,omitempty"`
			PlanIntent *plan.ResolvedInstallPlan `json:"plan_intent,omitempty"`
		}
		out := make([]jsonAttempt, 0, len(attempts))
		for _, a := range attempts {
			out = append(out, jsonAttempt{Kind: a.Kind, Label: a.Label, Status: a.Status, Reason: a.Error, Intent: a.Intent, PlanIntent: a.PlanIntent})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			log.Default.Error("JSON encode", "error", err)
			return exitWithCode(3)
		}
		return nil
	}

	c := newCLIStyle(os.Stdout)
	fmt.Fprintf(c.w, "%s  %s\n\n", c.bold(fmt.Sprintf("Why %s?", toolName)), c.dim(plural(len(attempts), "candidate method")+", first available wins"))
	kindW := 0
	names := make([]string, len(attempts))
	for i, a := range attempts {
		names[i] = a.DisplayName()
		if a.Label != "" {
			names[i] += " (" + a.Kind + ")"
		}
		if len(names[i]) > kindW {
			kindW = len(names[i])
		}
	}
	for i, a := range attempts {
		reason := a.Error
		if reason == "" {
			reason = "ready to install"
		}
		if intent := formatWhyIntent(a.Intent); intent != "" {
			reason += " [" + intent + "]"
		}
		kind := padRight(names[i], kindW)
		switch a.Status {
		case "would_install":
			fmt.Fprintf(c.w, "  %s %s  %s\n", c.green("✓"), kind, c.dim("→ "+reason))
		case "already_installed":
			fmt.Fprintf(c.w, "  %s %s  %s\n", c.green("✓"), kind, c.dim("already installed: "+reason))
		case "skip_when":
			fmt.Fprintf(c.w, "  %s %s  %s\n", c.yellow("–"), c.dim(kind), c.dim("skipped: "+reason))
		case "skip_unavailable":
			fmt.Fprintf(c.w, "  %s %s  %s\n", c.red("✗"), c.dim(kind), c.dim("unavailable: "+reason))
		case "skip_policy":
			fmt.Fprintf(c.w, "  %s %s  %s\n", c.yellow("–"), c.dim(kind), c.dim("disallowed: "+reason))
		case "skip_capability":
			fmt.Fprintf(c.w, "  %s %s  %s\n", c.yellow("–"), c.dim(kind), c.dim("capability mismatch: "+reason))
		default:
			fmt.Fprintf(c.w, "  %s %s  %s\n", c.dim("?"), kind, c.dim(reason))
		}
	}
	fmt.Fprintln(c.w)

	if *whyFields {
		if provenance, ok := s.Provenance[toolName]; ok && len(provenance) > 0 {
			fmt.Println("\nField provenance:")
			for _, fs := range provenance {
				var mergedStr string
				switch v := fs.Merged.(type) {
				case string:
					mergedStr = v
				case []string:
					mergedStr = fmt.Sprintf("%v", v)
				case []*config.MethodCandidate:
					mergedStr = fmt.Sprintf("[%d methods]", len(v))
				case bool:
					mergedStr = fmt.Sprintf("%v", v)
				default:
					mergedStr = fmt.Sprintf("%v", v)
				}
				fmt.Printf("  %s: source=%s merged=%v\n", fs.Field, fs.Source, mergedStr)
			}
		}
	}
	return nil
}
