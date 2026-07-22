package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"depengine/pkg/engine"
	"depengine/pkg/exec"
	"depengine/pkg/graph"
	"depengine/pkg/lang"
	"depengine/pkg/log"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

func runGraph(args []string) {
	graphCmd := flag.NewFlagSet("graph", flag.ExitOnError)
	graphSchema := graphCmd.String("schema", defaultSchemaPath(), "path to schema.toml")
	graphManifest := graphCmd.String("manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
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

	s, err := schema.ParseSchemaNoFacts(*graphSchema)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(exitCodeForError(err))
	}

	manifestPath := *graphManifest
	manifestAuto := false
	if manifestPath == "" {
		manifestPath = schema.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}
	if manifestPath != "" {
		manifestTools, merr := schema.ParseManifest(manifestPath)
		if merr != nil {
			fmt.Fprintf(os.Stderr, "error loading manifest: %v\n", merr)
			os.Exit(2)
		}
		if manifestTools != nil {
			var count int
			s, count = schema.ResolveSchema(s, manifestTools)
			if manifestAuto && count > 0 {
				fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, count)
			}
		}
	}
	fmt.Fprintf(os.Stderr, "depengine graph: tools=%d\n", len(s.Tools))
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
	whyCmd := flag.NewFlagSet("why", flag.ExitOnError)
	whySchema := whyCmd.String("schema", defaultSchemaPath(), "path to schema.toml")
	whyManifest := whyCmd.String("manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	whyJSON := whyCmd.Bool("json", false, "JSON output")
	whyCmd.Parse(args)
	remain := whyCmd.Args()
	if len(remain) < 1 {
		log.Default.Error("usage: depengine why <tool>")
		os.Exit(1)
	}
	toolName := remain[0]

	s, err := schema.ParseSchemaNoFacts(*whySchema)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	manifestPath := *whyManifest
	manifestAuto := false
	if manifestPath == "" {
		manifestPath = schema.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}
	if manifestPath != "" {
		manifestTools, merr := schema.ParseManifest(manifestPath)
		if merr != nil {
			fmt.Fprintf(os.Stderr, "error loading manifest: %v\n", merr)
			os.Exit(2)
		}
		if manifestTools != nil {
			var count int
			s, count = schema.ResolveSchema(s, manifestTools)
			if manifestAuto && count > 0 {
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
		lang.ReconfigureAUR(helper)
	}

	if verr, warnings := schema.Validate(s, exec.RegisteredKinds()); verr != nil {
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

	fmt.Printf("Why %s? (%d methods)\n", toolName, len(tool.Methods))
	for _, a := range attempts {
		statusSymbol := "?"
		switch a.Status {
		case "would_install":
			statusSymbol = "✓"
		case "already_installed":
			statusSymbol = "✓"
		case "skip_when":
			statusSymbol = "–"
		case "skip_unavailable":
			statusSymbol = "✗"
		default:
			statusSymbol = "?"
		}
		reason := a.Error
		if reason == "" {
			reason = "ready to install"
		}
		fmt.Printf("  %s %s — %s\n", statusSymbol, a.Kind, reason)
	}
}
