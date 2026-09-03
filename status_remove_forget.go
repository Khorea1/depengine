package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/lock"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/state"
)

// runStatus shows the installation status of tools by comparing the state
// file against the schema. It reports installed, missing, and outdated tools.
func runStatus(args []string) {
	// flags maintained in help.go:printCommandHelp
	statusCmd := flag.NewFlagSet("status", flag.ExitOnError)
	statusSchema := statusCmd.String("schema", "", "override schema path")
	statusManifest := statusCmd.String("manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	statusNoManifest := statusCmd.Bool("no-manifest", false, "disable personal manifest (default: auto-detect)")
	statusFormat := statusCmd.String("format", "text", "output format: text or json")
	statusJSON := statusCmd.Bool("json", false, "JSON output (shorthand for --format=json)")
	statusOrphans := statusCmd.Bool("orphans", false, "show only orphaned tools")
	statusCmd.Parse(args)
	if *statusJSON {
		if *statusFormat == "text" {
			*statusFormat = "json"
		}
		fmt.Fprintln(os.Stderr, "depengine: --json is deprecated; use --format=json instead")
	}

	ls, err := state.LoadShared()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()

	st := ls.State()

	schemaPath := st.SchemaPath
	if *statusSchema != "" {
		schemaPath = *statusSchema
	}
	if schemaPath == "" {
		if len(st.Tools) == 0 {
			fmt.Fprintln(os.Stderr, "No tools in state (nothing installed yet). Use --schema to compare against a schema.")
			return
		}
	}

	// Load the lockfile alongside the schema for installed-vs-pinned
	// version comparisons (outdated detection). A missing lock is fine.
	var lk *lock.Lock
	if schemaPath != "" {
		lk, _ = lock.Load(lock.DefaultPath(schemaPath))
	}

	var s *config.Schema
	if schemaPath != "" {
		var err error
		s, err = config.ParseSchema(schemaPath, nil)
		if err != nil {
			log.Default.Warn("load schema for comparison", "error", err)
			s = nil
		}

		if s != nil {
			noManifest := *statusNoManifest
			manifestPath := *statusManifest
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
					log.Default.Warn("load manifest", "error", merr)
				} else {
					config.FilterManifestTools(s, manifestSchema)
					if gerr := config.ValidateManifestLayer(manifestSchema); gerr != nil {
						log.Default.Warn("validate manifest", "error", gerr)
					} else if gerr := config.ValidateManifestNewTools(s, manifestSchema); gerr != nil {
						log.Default.Warn("validate manifest new tools", "error", gerr)
					} else {
						count := len(manifestSchema.Tools)
						if count > 0 {
							s = config.MergeLayers(manifestSchema, s)
							if manifestAuto {
								fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, count)
							}
						}
					}
				}
			}
		}
	}

	type toolStatus struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Method  string `json:"method,omitempty"`
		Version string `json:"version,omitempty"`
		Updated string `json:"updated,omitempty"`
	}

	var tools []toolStatus

	for name, ts := range st.Tools {
		status := "installed"
		if s != nil {
			if _, inSchema := s.Tools[name]; !inSchema {
				status = "orphaned"
			}
		}
		if *statusOrphans && status != "orphaned" {
			continue
		}
		if status == "installed" && s != nil && !*statusOrphans {
			outdated := false
			if stTool, inSchema := s.Tools[name]; inSchema {
				// Definition drift: the schema definition changed since install.
				if ts.DefinitionHash != "" && state.DefinitionHash(stTool) != ts.DefinitionHash {
					outdated = true
				}
				// Version drift: the installed version differs from the pinned one.
				if !outdated && ts.Version != "" {
					if pin, ok := lockPinFor(lk, name, ts.MethodKind); ok && state.VersionOutdated(ts.Version, pin.Latest) {
						outdated = true
					}
				}
			}
			if outdated {
				status = "outdated"
			}
		}
		tools = append(tools, toolStatus{
			Name:    name,
			Status:  status,
			Method:  ts.Method,
			Version: ts.Version,
			Updated: ts.InstalledAt,
		})
	}

	if s != nil && !*statusOrphans {
		for name := range s.Tools {
			if _, inState := st.Tools[name]; !inState {
				tools = append(tools, toolStatus{
					Name:   name,
					Status: "missing",
				})
			}
		}
	}

	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})

	if *statusFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(tools); err != nil {
			log.Default.Error("json output", "error", err)
			os.Exit(3)
		}
		return
	}

	if len(tools) == 0 {
		if *statusOrphans {
			fmt.Fprintln(os.Stderr, "No orphan tools.")
		} else {
			fmt.Fprintln(os.Stderr, "No tools in state. Run 'depengine install' first.")
		}
		return
	}

	fmt.Fprintf(os.Stderr, "%-30s %-12s %-10s %-18s  %s\n", "Tool", "Status", "Method", "Version", "Installed At")
	fmt.Fprintln(os.Stderr, strings.Repeat("-", 88))
	for _, t := range tools {
		fmt.Fprintf(os.Stderr, "%-30s %-12s %-10s %-18s  %s\n", t.Name, t.Status, t.Method, t.Version, t.Updated)
	}
}

// runRemove removes a tool using the adapter that installed it.
// Supports --all, --dry-run, --schema, and --only flags.
func runRemove(args []string) {
	// flags maintained in help.go:printCommandHelp
	removeCmd := flag.NewFlagSet("remove", flag.ExitOnError)
	removeAll := removeCmd.Bool("all", false, "remove all tools")
	removeDryRun := removeCmd.Bool("dry-run", false, "show what would be removed")
	removeSchema := removeCmd.String("schema", "", "path to schema.toml (optional, for validation)")
	removeOnly := removeCmd.String("only", "", "only remove specific tool (alternative to positional arg)")
	removeForce := removeCmd.Bool("force", false, "skip confirmation when removing all tools")
	removeArgs := parseFlagsInterspersed(removeCmd, args)

	// Validate mutually exclusive flags.
	if *removeAll && *removeOnly != "" {
		log.Default.Error("cannot use both --all and --only")
		os.Exit(2)
	}

	ls, err := state.LoadLocked()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()

	st := ls.State()

	// Optionally load schema for validation.
	var schemaTools map[string]*config.Tool
	if *removeSchema != "" {
		s, _, _, err := loadSchema(*removeSchema)
		if err != nil {
			log.Default.Error("load schema", "error", err)
			os.Exit(2)
		}
		schemaTools = s.Tools
	}

	removeTool := func(toolName string) bool {
		// If schema is loaded, validate tool exists (warn but continue).
		if schemaTools != nil {
			if _, ok := schemaTools[toolName]; !ok {
				log.Default.Warn("tool not found in schema, removing from state anyway", "tool", toolName)
			}
		}

		toolState, ok := st.Tools[toolName]
		if !ok {
			log.Default.Warn("tool not installed, nothing to remove", "tool", toolName)
			return false
		}
		methodKind := toolState.MethodKind
		if methodKind == "" {
			methodKind = toolState.Method // fallback for old state files
		}

		adapter := exec.Lookup(methodKind)
		if adapter == nil {
			log.Default.Warn("adapter not found for method", "tool", toolName, "method", toolState.Method, "methodKind", methodKind)
			log.Default.Warn("manual remove required", "tool", toolName)
			return false
		}

		if !exec.CanRemove(adapter) {
			log.Default.Warn("manual remove required", "tool", toolName, "method", toolState.Method, "methodKind", methodKind)
			return false
		}

		remover := adapter.(exec.Remover)
		mc := &config.MethodCandidate{
			Kind:   methodKind,
			Config: toolState.Config,
		}
		tool := &config.Tool{Name: toolName}

		if *removeDryRun {
			log.Default.Info("would remove", "tool", toolName, "method", toolState.Method)
			return true
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := remover.Remove(ctx, run.OSExecRunner{}, tool, mc); err != nil {
			log.Default.Error("remove failed", "tool", toolName, "error", err)
			return false
		}

		log.Default.Info("removed", "tool", toolName, "method", toolState.Method)
		delete(st.Tools, toolName)
		return true
	}

	if *removeAll && !*removeForce {
		if !isInteractive() {
			log.Default.Error("stdin is not a terminal; use --force to confirm, or run in an interactive terminal")
			os.Exit(2)
		}
		fmt.Fprint(os.Stderr, "WARNING: This will remove ALL installed tools tracked by depengine.\nAre you sure? [y/N] ")
		input, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Fprintln(os.Stderr, "Aborted.")
			os.Exit(0)
		}
	}

	hadFailure := false

	if *removeAll {
		for toolName := range st.Tools {
			if !removeTool(toolName) {
				hadFailure = true
			}
		}
	} else if *removeOnly != "" {
		if !removeTool(*removeOnly) {
			hadFailure = true
		}
	} else if len(removeArgs) > 0 {
		for _, toolName := range removeArgs {
			if !removeTool(toolName) {
				hadFailure = true
			}
		}
	} else {
		log.Default.Error("usage: depengine remove [--all | --only=<tool> | <tool>...] [--schema=<path>]")
		ls.Close()
		os.Exit(1)
	}

	if err := ls.Save(); err != nil {
		log.Default.Error("failed to update state", "error", err)
		ls.Close()
		os.Exit(3)
	}

	if hadFailure {
		ls.Close()
		os.Exit(1)
	}
}

// runForget removes a tool from state without attempting system removal.
func runForget(args []string) {
	// flags maintained in help.go:printCommandHelp
	forgetCmd := flag.NewFlagSet("forget", flag.ExitOnError)
	forgetArgs := parseFlagsInterspersed(forgetCmd, args)

	if len(forgetArgs) != 1 {
		log.Default.Error("usage: depengine forget <tool>")
		os.Exit(1)
	}

	toolName := forgetArgs[0]
	ls, err := state.LoadLocked()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()
	st := ls.State()
	if _, ok := st.Tools[toolName]; !ok {
		log.Default.Error("tool not found in state", "tool", toolName)
		ls.Close()
		os.Exit(1)
	}

	delete(st.Tools, toolName)
	if err := ls.Save(); err != nil {
		log.Default.Error("save state", "error", err)
		ls.Close()
		os.Exit(3)
	}

	log.Default.Info("forgotten", "tool", toolName)
}

func isInteractive() bool {
	fi, _ := os.Stdin.Stat()
	return fi != nil && fi.Mode()&os.ModeCharDevice != 0
}
