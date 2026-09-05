package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/engine"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/lock"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/state"
	"github.com/spf13/cobra"
)

// newStatusCmd builds `depengine status`.
func newStatusCmd() *cobra.Command {
	statusSchema := new(string)
	statusManifest := new(string)
	statusNoManifest := new(bool)
	statusFormat := new(string)
	statusJSON := new(bool)
	statusOrphans := new(bool)

	cmd := &cobra.Command{
		Use:     "status",
		Short:   ifPT("Mostrar estado das ferramentas em relação ao schema", "Show tool installation state vs schema"),
		GroupID: groupInspect,
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			runStatus(statusSchema, statusManifest, statusNoManifest, statusFormat, statusJSON, statusOrphans)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(statusSchema, "schema", "", "override schema path")
	f.StringVar(statusManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(statusNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	f.StringVar(statusFormat, "format", "text", "output format: text or json")
	f.BoolVar(statusJSON, "json", false, "JSON output (shorthand for --format=json)")
	f.BoolVar(statusOrphans, "orphans", false, "show only orphaned tools")
	return cmd
}

// runStatus shows the installation status of tools by comparing the state
// file against the schema. It reports installed, missing, and outdated tools.
// Body unchanged from the pre-Cobra version — only the flag declarations
// above it moved.
func runStatus(statusSchema, statusManifest *string, statusNoManifest *bool, statusFormat *string, statusJSON, statusOrphans *bool) {
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

	// Actionable states first: outdated tools need attention, missing ones
	// block the schema, orphaned ones are cleanup candidates. Installed and
	// healthy come last — they're the background noise.
	sort.SliceStable(tools, func(i, j int) bool {
		pi, pj := statusRank(tools[i].Status), statusRank(tools[j].Status)
		if pi != pj {
			return pi < pj
		}
		return tools[i].Name < tools[j].Name
	})

	c := newCLIStyle(os.Stderr)
	nameW, stW, methW, verW := len("Tool"), len("Status"), len("Method"), len("Version")
	for _, t := range tools {
		if len(t.Name) > nameW {
			nameW = len(t.Name)
		}
		if len(t.Status) > stW {
			stW = len(t.Status)
		}
		if len(t.Method) > methW {
			methW = len(t.Method)
		}
		if len(t.Version) > verW {
			verW = len(t.Version)
		}
	}

	fmt.Fprintf(c.w, "  %s  %s  %s  %s  %s\n",
		c.dim(padRight("Tool", nameW)), c.dim(padRight("Status", stW)),
		c.dim(padRight("Method", methW)), c.dim(padRight("Version", verW)), c.dim("Installed"))
	counts := map[string]int{}
	for _, t := range tools {
		counts[t.Status]++
		method := t.Method
		if method == "" {
			method = "—"
		}
		version := t.Version
		if version == "" {
			version = "—"
		}
		// Relative time scans faster than an RFC3339 timestamp for a "how old
		// is this install" question the status table answers constantly.
		installed := "—"
		if ts, err := time.Parse(time.RFC3339, t.Updated); err == nil {
			installed = relativeTime(ts)
		} else if t.Updated != "" {
			installed = t.Updated
		}
		fmt.Fprintf(c.w, "  %s  %s  %s  %s  %s\n",
			padRight(t.Name, nameW), statusStyled(c, padRight(t.Status, stW), t.Status),
			padRight(method, methW), c.dim(padRight(version, verW)), c.dim(installed))
	}

	fmt.Fprintln(c.w)
	var parts []string
	for _, st := range []string{"outdated", "missing", "orphaned", "installed"} {
		if n := counts[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, st))
		}
	}
	fmt.Fprintf(c.w, "  %s\n", c.dim(strings.Join(parts, "  ·  ")))
}

func statusRank(s string) int {
	switch s {
	case "outdated":
		return 0
	case "missing":
		return 1
	case "orphaned":
		return 2
	default: // installed
		return 3
	}
}

// statusStyled colors a status word by severity. The string must already be
// padded — the caller pads before colorizing so ANSI escapes don't break
// column alignment.
func statusStyled(c *cliStyle, padded, status string) string {
	switch status {
	case "outdated":
		return c.yellow(padded)
	case "missing":
		return c.red(padded)
	case "orphaned":
		return c.yellow(padded)
	default: // installed
		return c.green(padded)
	}
}

// newRemoveCmd builds `depengine remove`.
func newRemoveCmd() *cobra.Command {
	removeAll := new(bool)
	removeDryRun := new(bool)
	removeSchema := new(string)
	removeOnly := new(string)
	removeForce := new(bool)

	cmd := &cobra.Command{
		Use:     "remove [tool...]",
		Short:   ifPT("Remover ferramentas do sistema", "Remove tools from the system"),
		GroupID: groupManage,
		Args:    cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			runRemove(args, removeAll, removeDryRun, removeSchema, removeOnly, removeForce)
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(removeAll, "all", false, "remove all tools")
	f.BoolVar(removeDryRun, "dry-run", false, "show what would be removed")
	f.StringVar(removeSchema, "schema", "", "path to schema.toml (optional, for validation)")
	f.StringVar(removeOnly, "only", "", "only remove specific tool (alternative to positional arg)")
	f.BoolVar(removeForce, "force", false, "skip confirmation when removing all tools")
	return cmd
}

// runRemove removes a tool using the adapter that installed it.
// Supports --all, --dry-run, --schema, and --only flags. Body unchanged
// from the pre-Cobra version — only the flag declarations above it moved,
// and removeArgs is now the positional args Cobra already separated out.
func runRemove(removeArgs []string, removeAll, removeDryRun *bool, removeSchema, removeOnly *string, removeForce *bool) {
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

	// The global "native" adapter (registered in main.go) is constructed
	// with an empty clan and falls back to PATH-probing, which is ambiguous
	// for manager binaries shared across clans (e.g. "pkg" on both termux
	// and freebsd — same install command, different check/remove commands).
	// Resolve the real clan from OS facts and make it authoritative here,
	// the same way install/upgrade already do, so removal always uses the
	// correct check/remove commands for this machine.
	if facts, err := engine.GatherFacts(run.OSExecRunner{}); err == nil {
		exec.Replace(exec.NewNativeAdapter(engine.ResolveFamily(facts)))
	} else {
		log.Default.Warn("could not gather OS facts; falling back to PATH-probing for native manager detection", "error", err)
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

// newForgetCmd builds `depengine forget`.
func newForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "forget <tool>",
		Short:   ifPT("Esquecer uma ferramenta sem tentar removê-la do sistema", "Forget a tool from state without removing it from the system"),
		GroupID: groupManage,
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runForget(args[0])
			return nil
		},
	}
}

// runForget removes a tool from state without attempting system removal.
// Body unchanged from the pre-Cobra version — Cobra's cobra.ExactArgs(1)
// now enforces the argument count that the old manual length check did.
func runForget(toolName string) {
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
