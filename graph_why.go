package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/ecosystem"
	"github.com/Khorea1/depengine/pkg/engine"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/graph"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/run"
)

func runGraph(args []string) {
	// flags maintained in help.go:printCommandHelp
	graphCmd := flag.NewFlagSet("graph", flag.ExitOnError)
	graphSchema := graphCmd.String("schema", defaultSchemaPath(), "path to schema.toml")
	graphManifest := graphCmd.String("manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	graphNoManifest := graphCmd.Bool("no-manifest", false, "disable personal manifest (default: auto-detect)")
	graphFormat := graphCmd.String("format", "text", "output format: mermaid, dot, text")
	graphProfile := graphCmd.String("profile", "", "only show tools with matching tag")
	graphOnly := graphCmd.String("only", "", "only show subgraph for specific tool")
	graphSkip := graphCmd.String("skip", "", "skip specific tools (comma-separated)")
	graphCmd.Parse(args)

	switch *graphFormat {
	case "mermaid", "dot", "text":
	default:
		fmt.Fprintf(os.Stderr, "error: unknown format %q (valid: mermaid, dot, text)\n", *graphFormat)
		os.Exit(2)
	}

	s, err := config.ParseSchema(*graphSchema, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitCodeForError(err))
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
		manifestSchema, merr := config.ParseSchema(manifestPath, nil, "packages")
		if merr != nil {
			fmt.Fprintf(os.Stderr, "error loading manifest: %v\n", merr)
			os.Exit(2)
		}
		config.FilterManifestTools(s, manifestSchema)
		if gerr := config.ValidateManifestLayer(manifestSchema); gerr != nil {
			fmt.Fprintf(os.Stderr, "error validating manifest: %v\n", gerr)
			os.Exit(2)
		}
		count := len(manifestSchema.Tools)
		if count > 0 {
			s = config.MergeLayers(manifestSchema, s)
			if manifestAuto {
				fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, count)
			}
		}
	}
	s.Tools = filterTools(s.Tools, *graphOnly, *graphSkip, *graphProfile)

	if len(s.Tools) == 0 {
		fmt.Fprintln(os.Stderr, "no tools matching filters")
		os.Exit(0)
	}

	levels, err := graph.Sort(s.Tools)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
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
		fmt.Print(graph.RenderMermaid(levels, s.Tools))
	case "dot":
		fmt.Print(graph.RenderDOT(levels, s.Tools))
	case "text":
		fmt.Print(graph.RenderText(levels, s.Tools))
	}
}

// runWhy shows why a tool would be installed via each candidate method,
// without actually installing anything. Useful for debugging complex schemas.
func runWhy(args []string) {
	// flags maintained in help.go:printCommandHelp
	whyCmd := flag.NewFlagSet("why", flag.ExitOnError)
	whySchema := whyCmd.String("schema", defaultSchemaPath(), "path to schema.toml")
	whyManifest := whyCmd.String("manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	whyNoManifest := whyCmd.Bool("no-manifest", false, "disable personal manifest (default: auto-detect)")
	whyJSON := whyCmd.Bool("json", false, "JSON output")
	whyFields := whyCmd.Bool("fields", false, "show field-level provenance")
	remain := parseFlagsInterspersed(whyCmd, args)
	if len(remain) < 1 {
		log.Default.Error("usage: depengine why <tool>")
		os.Exit(1)
	}
	toolName := remain[0]

	s, err := config.ParseSchema(*whySchema, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
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
		manifestSchema, merr := config.ParseSchema(manifestPath, nil, "packages")
		if merr != nil {
			fmt.Fprintf(os.Stderr, "error loading manifest: %v\n", merr)
			os.Exit(2)
		}
		config.FilterManifestTools(s, manifestSchema)
		if gerr := config.ValidateManifestLayer(manifestSchema); gerr != nil {
			fmt.Fprintf(os.Stderr, "error validating manifest: %v\n", gerr)
			os.Exit(2)
		}
		if gerr := config.ValidateManifestNewTools(s, manifestSchema); gerr != nil {
			fmt.Fprintf(os.Stderr, "error validating manifest: %v\n", gerr)
			os.Exit(2)
		}
		count := len(manifestSchema.Tools)
		if count > 0 {
			if *whyFields {
				s = config.MergeLayersWithProvenance(manifestSchema, s)
			} else {
				s = config.MergeLayers(manifestSchema, s)
			}
			if manifestAuto {
				fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, count)
			}
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
		os.Exit(exitCodeForError(verr))
	} else if len(warnings) > 0 {
		for _, w := range warnings {
			log.Default.Warn(w)
		}
	}

	tool, ok := s.Tools[toolName]
	if !ok {
		log.Default.Error("tool not found in schema", "tool", toolName)
		os.Exit(1)
	}

	ex := exec.New()
	exec.WithRunner(run.OSExecRunner{})(ex)
	exec.WithFacts(facts)(ex)
	attempts := ex.ExplainTool(context.Background(), tool, clan)
	if *whyJSON {
		type jsonAttempt struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
			Reason string `json:"reason,omitempty"`
		}
		out := make([]jsonAttempt, 0, len(attempts))
		for _, a := range attempts {
			out = append(out, jsonAttempt{Kind: a.Kind, Status: a.Status, Reason: a.Error})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			log.Default.Error("JSON encode", "error", err)
			os.Exit(3)
		}
		return
	}

	c := newCLIStyle(os.Stdout)
	fmt.Fprintf(c.w, "%s  %s\n\n", c.bold(fmt.Sprintf("Why %s?", toolName)), c.dim(plural(len(attempts), "candidate method")+", first available wins"))
	kindW := 0
	for _, a := range attempts {
		if len(a.Kind) > kindW {
			kindW = len(a.Kind)
		}
	}
	for _, a := range attempts {
		reason := a.Error
		if reason == "" {
			reason = "ready to install"
		}
		kind := padRight(a.Kind, kindW)
		switch a.Status {
		case "would_install":
			fmt.Fprintf(c.w, "  %s %s  %s\n", c.green("✓"), kind, c.dim("→ "+reason))
		case "already_installed":
			fmt.Fprintf(c.w, "  %s %s  %s\n", c.green("✓"), kind, c.dim("already installed"))
		case "skip_when":
			fmt.Fprintf(c.w, "  %s %s  %s\n", c.yellow("–"), c.dim(kind), c.dim("skipped: "+reason))
		case "skip_unavailable":
			fmt.Fprintf(c.w, "  %s %s  %s\n", c.red("✗"), c.dim(kind), c.dim("unavailable: "+reason))
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
}
