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
	"github.com/Khorea1/depengine/internal/term"
	"github.com/spf13/cobra"
)

// newGraphCmd builds `depengine graph`.
func newGraphCmd() *cobra.Command {
	graphSchema := new(string)
	graphManifest := new(string)
	graphNoManifest := new(bool)
	graphFormat := new(string)
	graphWidth := new(int)
	graphView := new(string)
	graphShowInactive := new(bool)
	graphProfile := new(string)
	graphOnly := new(string)
	graphSkip := new(string)
	graphDirection := new(string)
	graphDepth := new(int)

	cmd := &cobra.Command{
		Use:     "graph",
		Short:   ifPT("Mostrar o grafo de dependências", "Show the dependency graph"),
		GroupID: groupInspect,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGraphView(cmd.Context(), graphSchema, graphManifest, graphNoManifest, graphFormat, graphWidth, graphView, graphShowInactive, graphProfile, graphOnly, graphSkip, graphDirection, graphDepth)
		},
	}
	f := cmd.Flags()
	f.StringVar(graphSchema, "schema", defaultSchemaPath(), "path to schema.toml")
	f.StringVar(graphManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(graphNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	f.StringVar(graphFormat, "format", "text", "output format: mermaid, dot, text, graph")
	f.IntVar(graphWidth, "width", 0, "terminal width for --format graph (0 = detect terminal width)")
	f.StringVar(graphView, "view", "declared", "graph projection: declared, effective, resolved")
	f.BoolVar(graphShowInactive, "show-inactive", false, "include guard-inactive edges in effective/resolved views")
	f.StringVar(graphProfile, "profile", "", "only show tools with matching tag")
	f.StringVar(graphOnly, "only", "", "only show subgraph for specific tool")
	f.StringVar(graphSkip, "skip", "", "skip specific tools (comma-separated)")
	f.StringVar(graphDirection, "direction", graph.DirectionDeps.String(), "with --only: traversal direction: deps, dependents, both")
	f.IntVar(graphDepth, "depth", graph.Unbounded, "with --only: maximum traversal depth in edges (-1 = unbounded, 0 = only the tool itself)")
	return cmd
}

func runGraphView(ctx context.Context, graphSchema, graphManifest *string, graphNoManifest *bool, graphFormat *string, graphWidth *int, graphView *string, graphShowInactive *bool, graphProfile, graphOnly, graphSkip, graphDirection *string, graphDepth *int) error {
	switch *graphFormat {
	case "mermaid", "dot", "text", "graph":
	default:
		fmt.Fprintf(os.Stderr, "error: unknown format %q (valid: mermaid, dot, text, graph)\n", *graphFormat)
		return exitWithCode(2)
	}
	if *graphFormat == "graph" && *graphWidth < 0 {
		fmt.Fprintln(os.Stderr, "error: --width must not be negative (0 = detect terminal width)")
		return exitWithCode(2)
	}

	view, err := parseGraphView(*graphView)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitWithCode(2)
	}
	if err := validateGraphProjectionOptions(view, *graphShowInactive); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitWithCode(2)
	}

	direction, err := graph.ParseDirection(*graphDirection)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitWithCode(2)
	}
	if err := validateGraphSliceOptions(*graphOnly, direction, *graphDepth); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
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
	// The historical `--only` closure keeps its schema-level filtering. Any
	// other direction or depth slices the complete typed IR instead, because
	// dependent traversal needs successor information that a pre-filtered
	// schema no longer has. s.Tools stays complete in that case so resolved
	// candidate selection can still see every tool it may probe.
	var declaredGraph graph.Graph
	if graphSliceRequested(direction, *graphDepth) {
		var ok bool
		declaredGraph, ok, err = sliceDeclaredGraph(s.Tools, *graphOnly, *graphSkip, *graphProfile, direction, *graphDepth)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return exitWithCode(2)
		}
		if !ok {
			fmt.Fprintln(os.Stderr, "no tools matching filters")
			return nil
		}
	} else {
		s.Tools = filterTools(s.Tools, *graphOnly, *graphSkip, *graphProfile)
		if len(s.Tools) == 0 {
			fmt.Fprintln(os.Stderr, "no tools matching filters")
			return nil
		}
		declaredGraph = graph.BuildDeclaredGraph(s.Tools)
	}
	visibleGraph, err := projectGraphView(ctx, declaredGraph, s, view, *graphShowInactive)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: project %s graph: %v\n", view, err)
		return exitWithCode(3)
	}
	levels, err := graph.SortGraph(visibleGraph)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return exitWithCode(2)
	}

	if *graphFormat == "text" {
		// Text format is the human-facing one — give it a header naming the
		// working set, so a long level list doesn't start mid-air. Mermaid and
		// dot are machine-consumed; no decoration there.
		c := newCLIStyle(os.Stderr)
		_, _ = fmt.Fprintf(c.w, "%s\n\n", c.dim(fmt.Sprintf("%d tools in %d levels", len(visibleGraph.Nodes), len(levels))))
	}
	switch *graphFormat {
	case "mermaid":
		fmt.Print(graph.RenderMermaidGraph(visibleGraph))
	case "dot":
		fmt.Print(graph.RenderDOTGraph(visibleGraph))
	case "text":
		fmt.Print(graph.RenderTextGraph(levels, visibleGraph))
	case "graph":
		// Unlike text, the terminal diagram carries its own structure, so it
		// gets no stderr header: everything the renderer outputs belongs to
		// the picture.
		width := *graphWidth
		if width == 0 {
			width = term.OutputWidth()
		}
		out, err := graph.RenderTerminalGraph(visibleGraph, width)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return exitWithCode(2)
		}
		fmt.Print(out)
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
		Aliases: []string{"explain"},
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

// disambiguatedWhyNames renders one human-facing name per candidate that stays
// unique even when several candidates share the same kind or label. Colliding
// display names gain a `#<ordinal>` suffix (for example `http #0`, `http #1`)
// so `why` text output never collapses two distinct candidates into one row.
// Labeled candidates keep their `label (kind)` form; synthesized candidates
// without a declared ordinal keep the plain display name.
func disambiguatedWhyNames(attempts []exec.MethodAttempt) []string {
	base := make([]string, len(attempts))
	counts := make(map[string]int)
	for i, a := range attempts {
		name := a.DisplayName()
		if a.Label != "" {
			name += " (" + a.Kind + ")"
		}
		base[i] = name
		counts[name]++
	}
	names := make([]string, len(attempts))
	for i, a := range attempts {
		names[i] = base[i]
		if counts[base[i]] > 1 && a.CandidateKnown {
			names[i] = fmt.Sprintf("%s #%d", base[i], a.Candidate)
		}
	}
	return names
}

// ambiguousWhyWarning reports same-kind candidates that durable state, lock,
// upgrade, and remove resolution cannot tell apart: kind-only state with more
// than one matching candidate fails closed. Labeled duplicates are exact but
// still worth naming; unlabeled duplicates need distinct labels.
func ambiguousWhyWarning(toolName string, attempts []exec.MethodAttempt) string {
	byKind := make(map[string][]exec.MethodAttempt)
	for _, a := range attempts {
		if a.Kind == "" {
			continue
		}
		byKind[a.Kind] = append(byKind[a.Kind], a)
	}
	var warnings []string
	for kind, group := range byKind {
		if len(group) < 2 {
			continue
		}
		labels := make([]string, 0, len(group))
		labelCounts := make(map[string]int)
		unlabeled := 0
		for _, a := range group {
			if a.Label != "" {
				labelCounts[a.Label]++
				if a.CandidateKnown {
					labels = append(labels, fmt.Sprintf("#%d %q", a.Candidate, a.Label))
				} else {
					labels = append(labels, fmt.Sprintf("%q", a.Label))
				}
				continue
			}
			unlabeled++
			if a.CandidateKnown {
				labels = append(labels, fmt.Sprintf("#%d %q", a.Candidate, a.Kind))
			} else {
				labels = append(labels, fmt.Sprintf("%q", a.Kind))
			}
		}
		var duplicates []string
		for label, count := range labelCounts {
			if count > 1 {
				duplicates = append(duplicates, fmt.Sprintf("%q", label))
			}
		}
		if unlabeled == 0 && len(duplicates) == 0 {
			continue
		}
		sort.Strings(labels)
		sort.Strings(duplicates)
		switch {
		case unlabeled > 0 && len(duplicates) > 0:
			warnings = append(warnings, fmt.Sprintf("tool %q has ambiguous %q candidate identity (%s); label the unlabeled candidate(s) and make duplicate label(s) %s unique", toolName, kind, strings.Join(labels, ", "), strings.Join(duplicates, ", ")))
		case unlabeled > 0:
			warnings = append(warnings, fmt.Sprintf("tool %q has ambiguous %q candidate identity (%s); label the unlabeled candidate(s) so upgrade/remove/lock can persist an exact candidate", toolName, kind, strings.Join(labels, ", ")))
		default:
			warnings = append(warnings, fmt.Sprintf("tool %q has ambiguous %q candidate identity (%s); duplicate label(s) %s must be unique", toolName, kind, strings.Join(labels, ", "), strings.Join(duplicates, ", ")))
		}
	}
	sort.Strings(warnings)
	return strings.Join(warnings, "; ")
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
			Kind           string                    `json:"kind"`
			Label          string                    `json:"label,omitempty"`
			Candidate      int                       `json:"candidate"`
			CandidateKnown bool                      `json:"candidate_known"`
			Status         string                    `json:"status"`
			Reason         string                    `json:"reason,omitempty"`
			Intent         map[string]string         `json:"intent,omitempty"`
			PlanIntent     *plan.ResolvedInstallPlan `json:"plan_intent,omitempty"`
		}
		out := make([]jsonAttempt, 0, len(attempts))
		for _, a := range attempts {
			out = append(out, jsonAttempt{Kind: a.Kind, Label: a.Label, Candidate: a.Candidate, CandidateKnown: a.CandidateKnown, Status: a.Status, Reason: a.Error, Intent: a.Intent, PlanIntent: a.PlanIntent})
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
	_, _ = fmt.Fprintf(c.w, "%s  %s\n\n", c.bold(fmt.Sprintf("Why %s?", toolName)), c.dim(plural(len(attempts), "candidate method")+", first available wins"))
	kindW := 0
	names := disambiguatedWhyNames(attempts)
	for _, name := range names {
		if len(name) > kindW {
			kindW = len(name)
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
			_, _ = fmt.Fprintf(c.w, "  %s %s  %s\n", c.green("✓"), kind, c.dim("→ "+reason))
		case "already_installed":
			_, _ = fmt.Fprintf(c.w, "  %s %s  %s\n", c.green("✓"), kind, c.dim("already installed: "+reason))
		case "skip_when":
			_, _ = fmt.Fprintf(c.w, "  %s %s  %s\n", c.yellow("–"), c.dim(kind), c.dim("skipped: "+reason))
		case "skip_unavailable":
			_, _ = fmt.Fprintf(c.w, "  %s %s  %s\n", c.red("✗"), c.dim(kind), c.dim("unavailable: "+reason))
		case "skip_policy":
			_, _ = fmt.Fprintf(c.w, "  %s %s  %s\n", c.yellow("–"), c.dim(kind), c.dim("disallowed: "+reason))
		case "skip_capability":
			_, _ = fmt.Fprintf(c.w, "  %s %s  %s\n", c.yellow("–"), c.dim(kind), c.dim("capability mismatch: "+reason))
		default:
			_, _ = fmt.Fprintf(c.w, "  %s %s  %s\n", c.dim("?"), kind, c.dim(reason))
		}
	}
	_, _ = fmt.Fprintln(c.w)
	if warning := ambiguousWhyWarning(toolName, attempts); warning != "" {
		_, _ = fmt.Fprintf(c.w, "  %s\n\n", c.yellow("warning: "+warning))
	}

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
