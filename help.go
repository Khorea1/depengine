package main

import (
	"flag"
	"fmt"
	"os"
)

// printCommandHelp shows per-command flags.
// Keep in sync with runXxx functions in the corresponding .go files.
func printCommandHelp(name string) {
	var desc string
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.SetOutput(os.Stdout)

	switch name {
	case "init":
		desc = "Initialize a schema.toml for a new project"
		fs.String("schema", "", "path to write (default: schema.toml)")
		fs.String("add", "", "comma-separated tool names to pre-populate")
		fs.Bool("interactive", false, "interactive wizard mode")
	case "install":
		desc = "Install tools from schema.toml"
		fs.String("schema", "", "path to schema (auto-detected)")
		fs.Bool("dry-run", false, "show what would be installed")
		fs.Bool("verbose", false, "deprecated — output is now verbose by default; use --quiet for summary-only")
		fs.Bool("json", false, "JSON output")
		fs.String("only", "", "install a single tool")
		fs.String("skip", "", "skip comma-separated tools")
		fs.String("sort-by", "", "sort output: name, status, method")
		fs.String("log-level", "info", "log level: debug, info, warn, error")
		fs.Bool("diagnose", false, "debug + dry-run + verbose")
		fs.String("profile", "", "filter tools by tag")
		fs.Int("jobs", 1, "max concurrent installations")
		fs.Bool("allow-arbitrary-code", false, "suppress security warnings")
		fs.Bool("frozen-lockfile", false, "abort if no lockfile")
		fs.String("manifest", "", "path to personal manifest")
		fs.Bool("no-manifest", false, "disable personal manifest")
		fs.Bool("quiet", false, "suppress non-essential output")
	case "validate":
		desc = "Validate schema.toml and environment"
		fs.String("schema", "", "path to schema (auto-detected)")
		fs.String("manifest", "", "path to personal manifest")
		fs.Bool("no-manifest", false, "disable personal manifest")
		fs.Bool("check-env", false, "check required tools are on PATH")
		fs.String("format", "text", "output format: text or json")
		fs.Bool("strict", false, "warnings become errors")
	case "check":
		desc = "Check if a tool is actually installed (runs each method's check command)"
		fs.String("schema", "", "path to schema (auto-detected)")
		fs.String("manifest", "", "path to personal manifest")
		fs.Bool("no-manifest", false, "disable personal manifest")
		fs.Bool("live", false, "check live system instead of state file")
		fs.String("format", "text", "output format: text or json")
	case "status":
		desc = "Show tool installation state vs schema"
		fs.String("schema", "", "override schema path")
		fs.String("manifest", "", "path to personal manifest")
		fs.Bool("no-manifest", false, "disable personal manifest")
		fs.String("format", "text", "output format: text or json")
		fs.Bool("json", false, "JSON output (shorthand for --format=json)")
		fs.Bool("orphans", false, "show only orphaned tools")
	case "remove":
		desc = "Remove a tool from the system"
		fs.Bool("all", false, "remove all tools")
		fs.Bool("dry-run", false, "show what would be removed")
		fs.Bool("force", false, "skip confirmation")
		fs.String("only", "", "remove a specific tool")
	case "forget":
		desc = "Forget a tool from state without uninstalling"
	case "undo":
		desc = "Revert last installation via snapshot"
		fs.Bool("list", false, "list available snapshots")
		fs.String("snapshot", "", "revert to specific snapshot")
	case "update":
		desc = "Resolve {latest} and pin versions in depengine.lock"
		fs.String("schema", "", "path to schema (auto-detected)")
		fs.String("lock", "", "path to lockfile")
		fs.String("profile", "", "filter tools by tag")
		fs.Bool("frozen-lockfile", false, "abort if no lockfile")
		fs.Bool("dry-run", false, "show what would be updated")
		fs.Bool("v", false, "verbose output")
		fs.String("manifest", "", "path to personal manifest")
		fs.Bool("no-manifest", false, "disable personal manifest")
	case "graph":
		desc = "Show dependency graph"
		fs.String("format", "text", "format: text, mermaid, or dot")
		fs.String("only", "", "subgraph for one tool")
		fs.String("skip", "", "comma-separated tools to skip")
		fs.String("profile", "", "filter by tag")
		fs.String("manifest", "", "path to personal manifest")
		fs.Bool("no-manifest", false, "disable personal manifest")
	case "why":
		desc = "Explain how a tool would be installed"
		fs.Bool("json", false, "JSON output")
		fs.Bool("fields", false, "show field-level provenance from schema/manifest merge")
	case "sbom":
		desc = "Export SBOM (CycloneDX or SPDX)"
		fs.String("format", "cyclonedx", "output format: cyclonedx or spdx")
	case "diff":
		desc = "Compare state between machines"
		fs.String("other", "", "path to other state file")
		fs.Bool("json", false, "output as JSON")
	case "completion":
		desc = "Generate shell completion scripts"
	case "upgrade":
		desc = "Upgrade installed tools to pinned versions in depengine.lock"
		fs.String("schema", "", "path to schema (auto-detected)")
		fs.String("manifest", "", "path to personal manifest")
		fs.Bool("no-manifest", false, "disable personal manifest")
		fs.Bool("dry-run", false, "show what would be upgraded")
		fs.String("only", "", "only upgrade a specific tool")
		fs.Bool("force", false, "skip confirmation prompt")
		fs.Bool("json", false, "JSON output")
		fs.Bool("quiet", false, "suppress non-essential output")
		fs.Bool("allow-arbitrary-code", false, "suppress security warnings")
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", name)
		os.Exit(1)
	}

	c := newCLIStyle(os.Stdout)
	if desc != "" {
		fmt.Fprintf(c.w, "%s\n\n", c.bold(fmt.Sprintf("depengine %s — %s", name, desc)))
	}
	printFlags(c, fs)
}

// printFlags renders an aligned one-line-per-flag table: bold flag names on
// the left, placeholders for valued flags (--schema PATH), defaults in dim
// parentheses only when non-zero, so a 14-flag command like install scans as
// a table instead of the two-line-per-flag block flag.PrintDefaults emits.
func printFlags(c *cliStyle, fs *flag.FlagSet) {
	type row struct{ left, usage, def string }
	var rows []row
	leftWidth := 0
	fs.VisitAll(func(f *flag.Flag) {
		name, usage := flag.UnquoteUsage(f)
		left := "--" + f.Name
		if name != "" {
			left += " " + name
		}
		def := ""
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" {
			def = "(default: " + f.DefValue + ")"
		}
		if len(left) > leftWidth {
			leftWidth = len(left)
		}
		rows = append(rows, row{left: left, usage: usage, def: def})
	})

	fmt.Fprintln(c.w, c.dim("Flags"))
	for _, r := range rows {
		line := fmt.Sprintf("  %s  %s", c.bold(padRight(r.left, leftWidth)), r.usage)
		if r.def != "" {
			line += " " + c.dim(r.def)
		}
		fmt.Fprintln(c.w, line)
	}
}
