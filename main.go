package main


import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"depengine/pkg/engine"
	"depengine/pkg/exec"
	"depengine/pkg/git"
	"depengine/pkg/httpdownload"
	"depengine/pkg/lang"
	"depengine/pkg/lock"
	"depengine/pkg/log"
	"depengine/pkg/graph"
	"depengine/pkg/run"
	"depengine/pkg/schema"
	"depengine/pkg/sbom"
	"depengine/pkg/state"
	"depengine/pkg/validate"
	"depengine/pkg/i18n"
)
var version = "dev"

func main() {
	initAdapters()
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}
	switch os.Args[1] {
	case "install":
		runInstall(os.Args[2:])
	case "update":
		runUpdate(os.Args[2:])
	case "validate":
		runValidate(os.Args[2:])
	case "check":
		runCheck(os.Args[2:])
	case "graph":
		runGraph(os.Args[2:])
	case "status":
		runStatus(os.Args[2:])
	case "forget":
		runForget(os.Args[2:])
	case "remove":
		runRemove(os.Args[2:])
	case "why":
		runWhy(os.Args[2:])
	case "undo":
		runUndo(os.Args[2:])
	case "sbom":
		runSBOM(os.Args[2:])
	case "diff":
		runDiff(os.Args[2:])
    case "version":
        fmt.Println("depengine " + version)
        fmt.Println("Motor distro-agnostic de instalação de dependências")
	case "help", "-h", "--help":
		printUsage()
	case "completion":
		runCompletion(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "erro: comando desconhecido %q\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runInstall(args []string) {
	installCmd := flag.NewFlagSet("install", flag.ExitOnError)
	installSchema := installCmd.String("schema", "schema.toml", "path to schema.toml")
	installDryRun := installCmd.Bool("dry-run", false, "show what would be installed")
	installVerbose := installCmd.Bool("verbose", false, "detailed output")
	installJSON := installCmd.Bool("json", false, "JSON output")
	installOnly := installCmd.String("only", "", "only install specific tool")
	installSkip := installCmd.String("skip", "", "skip specific tools (comma-separated)")
	installProfile := installCmd.String("profile", "", "only install tools with matching tag (e.g. minimal,desktop,server)")
	installFrozen := installCmd.Bool("frozen-lockfile", false, "fail if schema.lock does not exist or needs update")
	installDiagnose := installCmd.Bool("diagnose", false, "diagnostic mode: DEBUG + dry-run + verbose")
	installLogLevel := installCmd.String("log-level", "", "log level: debug, info, warn, error")
	installSortBy := installCmd.String("sort-by", "", "sort output by: name, status, method")
	installJobs := installCmd.Int("jobs", 1, "max concurrent installations (default 1 = sequential)")
	installAllowArbitrary := installCmd.Bool("allow-arbitrary-code", false, "suppress security warnings for build scripts / arbitrary code")
	installCmd.Parse(args)

	// Create root logger. trace_id propagates from env automatically via
	// pkg/log init(); explicit --log-level overrides the default.
	lg := log.Default

	// --diagnose implies --dry-run + verbose + DEBUG level.
	if *installDiagnose {
		*installDryRun = true
		*installVerbose = true
		lg = log.New(os.Stderr, slog.LevelDebug)
	}
	if *installLogLevel != "" {
		lg = log.New(os.Stderr, log.LevelFromString(*installLogLevel))
	}

	ctx := context.Background()

	if *installSortBy != "" {
		if _, ok := exec.ParseSortField(*installSortBy); !ok {
			lg.Error("invalid --sort-by value", "value", *installSortBy, "valid", "name, status, method")
			os.Exit(2)
		}
	}

	s, clan, facts, err := loadSchema(*installSchema)
	if err != nil {
		lg.Error("load schema", "error", err)
		os.Exit(exitCodeForError(err))
	}
	if helper := s.Defaults.AurHelper; helper != "" {
		lang.ReconfigureAUR(helper)
	}

	fmt.Fprintf(os.Stderr, "depengine install: distro=%s clan=%s arch=%s tools=%d\n",
		facts.DistroID, clan, facts.TargetArch, len(s.Tools))

	// Stat schema file for state tracking.
	schemaFile, err := os.Stat(*installSchema)
	if err != nil {
		lg.Error("stat schema", "error", err)
		os.Exit(exitCodeForError(err))
	}

	ex := exec.New()
	exec.WithAdapters(
		git.NewGitAdapter(),
		httpdownload.NewHTTPAdapter(),
		exec.NewNativeAdapter(clan),
	)(ex)
	exec.WithSchemaInfo(*installSchema, schemaFile.ModTime())(ex)
	exec.WithLogger(lg)(ex)
	exec.WithRunner(run.NewLoggingRunner(run.OSExecRunner{}, lg))(ex)
	if *installDryRun {
		exec.WithDryRun()(ex)
	}
	if *installSortBy != "" {
		exec.WithSortBy(exec.SortField(*installSortBy))(ex)
	}
	if *installJobs > 1 {
		exec.WithMaxJobs(*installJobs)(ex)
	}
	if *installAllowArbitrary {
		exec.WithAllowArbitraryCode()(ex)
	}


	// --- Lockfile ---
	lockPath := lock.DefaultPath(*installSchema)
	lk, _ := lock.Load(lockPath)
	if *installFrozen && lk == nil {
		lg.Error("--frozen-lockfile requires schema.lock — run 'depengine update' first")
		os.Exit(2)
	}
	if lk != nil {
		lock.Apply(s, lk)
		if *installDiagnose {
			lg.Debug("lock applied", "pinned", len(lk.Tools))
		}
	}
	// --- end Lockfile ---


	s.Tools = filterTools(s.Tools, *installOnly, *installSkip, *installProfile)

	// Snapshot state before install (best-effort).
	if !*installDryRun {
		if _, err := state.SaveSnapshot(); err != nil {
			lg.Warn("could not save pre-install snapshot", "error", err)
		}
	}

	if *installDiagnose {
		lg.Debug("facts", "facts", facts)
		lg.Debug("schema", "tools", len(s.Tools))
	}


	report, err := ex.Execute(ctx, s, clan)
	if err != nil {
		lg.Error("execute failed", "error", err)
		os.Exit(2)
	}

	if *installJSON {
		fmt.Println(report.JSON())
	} else if *installVerbose || *installDryRun {
		fmt.Fprint(os.Stderr, report.Detail())
	} else {
		fmt.Fprintln(os.Stderr, report.Summary())
	}

	// Save/update lock even on partial failure to pin resolved versions.
	if !*installDryRun {
		if newLock, err := lock.ResolveAll(ctx, s); err != nil {
			lg.Warn("resolve lock", "error", err)
		} else if newLock != nil {
			// Merge with existing lock: keep existing pins for tools that
			// weren't re-resolved (e.g. native-only tools that don't use {latest}).
			if lk != nil {
				for k, v := range lk.Tools {
					if _, exists := newLock.Tools[k]; !exists {
						newLock.Tools[k] = v
					}
				}
			}
			if err := lock.Save(lockPath, newLock); err != nil {
				lg.Warn("save lock", "error", err)
			} else if *installDiagnose {
				lg.Debug("lock saved", "path", lockPath, "pinned", len(newLock.Tools))
			}
		}
	}

	if report.Failed > 0 {
		os.Exit(1)
	}
}


func runUpdate(args []string) {
	updateCmd := flag.NewFlagSet("update", flag.ExitOnError)
	updateSchema := updateCmd.String("schema", "schema.toml", "path to schema.toml")
	updateProfile := updateCmd.String("profile", "", "only resolve & pin tools with matching tag")
	updateVerbose := updateCmd.Bool("v", false, "detailed output")
	updateCmd.Parse(args)

	ctx := context.Background()
	lg := log.Default

	s, clan, facts, err := loadSchema(*updateSchema)
	if err != nil {
		lg.Error("load schema", "error", err)
		os.Exit(exitCodeForError(err))
	}
	if helper := s.Defaults.AurHelper; helper != "" {
		lang.ReconfigureAUR(helper)
	}

	fmt.Fprintf(os.Stderr, "depengine update: distro=%s clan=%s arch=%s tools=%d\n",
		facts.DistroID, clan, facts.TargetArch, len(s.Tools))

	s.Tools = filterTools(s.Tools, "", "", *updateProfile)

	fmt.Fprint(os.Stderr, "Resolving latest versions... ")
	newLock, err := lock.ResolveAll(ctx, s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL")
		lg.Error("resolve lock", "error", err)
		os.Exit(1)
	}

	lockPath := lock.DefaultPath(*updateSchema)
	if err := lock.Save(lockPath, newLock); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL")
		lg.Error("save lock", "error", err)
		os.Exit(1)
	}

	pinned := len(newLock.Tools)
	fmt.Fprintf(os.Stderr, "done (%d pinned)\n", pinned)

	if *updateVerbose {
		for key, pin := range newLock.Tools {
			fmt.Fprintf(os.Stderr, "  %s → %s\n", key, pin.Latest)
		}
	}

	fmt.Fprintln(os.Stderr, "Run 'depengine install' to use the updated lock.")
}


func runCheck(args []string) {
	checkCmd := flag.NewFlagSet("check", flag.ExitOnError)
	checkSchema := checkCmd.String("schema", "schema.toml", "path to schema.toml")
	checkCmd.Parse(args)
	remain := checkCmd.Args()
	if len(remain) < 1 {
		log.Default.Error("usage: depengine check <tool>")
		os.Exit(1)
	}
	toolName := remain[0]

	s, err := schema.ParseSchemaNoFacts(*checkSchema)
	if err != nil {
		log.Default.Error("load schema", "error", err)
		os.Exit(exitCodeForError(err))
	}

	// Validate schema — warn on non-fatal issues, error on broken tools.
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
		log.Default.Error("tool not found", "tool", toolName)
		os.Exit(1)
	}

	for _, method := range tool.Methods {
		adapter := exec.Lookup(method.Kind)
		if adapter == nil {
			continue
		}
		if adapter.Check(context.Background(), run.OSExecRunner{}, tool, method) {
			fmt.Printf("✓ %s is installed (via %s)\n", toolName, method.Kind)
			os.Exit(0)
		}
	}
	fmt.Printf("✗ %s is not installed\n", toolName)
	os.Exit(1)
}

// runGraph outputs the dependency graph in the requested format.
func runGraph(args []string) {
	graphCmd := flag.NewFlagSet("graph", flag.ExitOnError)
	graphSchema := graphCmd.String("schema", "schema.toml", "path to schema.toml")
	graphFormat := graphCmd.String("format", "text", "output format: mermaid, dot, text")
	graphProfile := graphCmd.String("profile", "", "only show tools with matching tag")
	graphOnly := graphCmd.String("only", "", "only show subgraph for specific tool")
	graphSkip := graphCmd.String("skip", "", "skip specific tools (comma-separated)")
	graphCmd.Parse(args)

	// Validate format before loading schema.
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
	whySchema := whyCmd.String("schema", "schema.toml", "path to schema.toml")
	whyJSON := whyCmd.Bool("json", false, "JSON output")
	whyCmd.Parse(args)
	remain := whyCmd.Args()
	if len(remain) < 1 {
		log.Default.Error("usage: depengine why <tool>")
		os.Exit(1)
	}
	toolName := remain[0]

	s, clan, facts, err := loadSchema(*whySchema)
	if err != nil {
		log.Default.Error("load schema", "error", err)
		os.Exit(exitCodeForError(err))
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
	_ = facts

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
		enc.Encode(out)
		return
	}

	fmt.Printf("Por que %s? (%d métodos)\n", toolName, len(tool.Methods))
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
		reason = "pronto para instalar"
		}
		fmt.Printf("  %s %s — %s\n", statusSymbol, a.Kind, reason)
	}
}

// runStatus shows the installation status of tools by comparing the state
// file against the schema. It reports installed, missing, and outdated tools.
func runStatus(args []string) {
	statusCmd := flag.NewFlagSet("status", flag.ExitOnError)
	statusSchema := statusCmd.String("schema", "", "override schema path")
	statusJSON := statusCmd.Bool("json", false, "JSON output")
	statusOrphans := statusCmd.Bool("orphans", false, "show only orphaned tools")
	statusCmd.Parse(args)

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

	// Load schema for comparison if available.
	var s *schema.Schema
	if schemaPath != "" {
		var err error
		s, err = schema.ParseSchemaNoFacts(schemaPath)
		if err != nil {
			log.Default.Warn("load schema for comparison", "error", err)
			s = nil
		}
	}

	type toolStatus struct {
		Name    string `json:"name"`
		Status  string `json:"status"`
		Method  string `json:"method,omitempty"`
		Updated string `json:"updated,omitempty"`
	}

	var tools []toolStatus

	// Build a set of tools known in the state.
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
		// Check definition hash: if the schema definition changed since install, mark as outdated.
		// Skip if DefinitionHash is empty (state from before this feature was added).
		if status == "installed" && s != nil && !*statusOrphans && ts.DefinitionHash != "" {
			if stTool, inSchema := s.Tools[name]; inSchema {
				if state.DefinitionHash(stTool) != ts.DefinitionHash {
					status = "outdated"
				}
			}
		}
		tools = append(tools, toolStatus{
			Name:    name,
			Status:  status,
			Method:  ts.Method,
			Updated: ts.InstalledAt,
		})
	}

	// Add tools from schema that are missing from state.
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

	if *statusJSON {
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
		fmt.Println("Nenhuma ferramenta órfã.")
		} else {
		fmt.Println("Nenhuma ferramenta no estado. Execute 'depengine install' primeiro.")
		}
		return
	}

	// Table output.
	fmt.Printf("%-30s %-12s %-10s  %s\n", "Ferramenta", "Status", "Método", "Instalada em")
	fmt.Println(strings.Repeat("-", 70))
	for _, t := range tools {
		fmt.Printf("%-30s %-12s %-10s  %s\n", t.Name, t.Status, t.Method, t.Updated)
	}
}

// runRemove removes a tool using the adapter that installed it.
func runRemove(args []string) {
	removeCmd := flag.NewFlagSet("remove", flag.ExitOnError)
	removeAll := removeCmd.Bool("all", false, "remove all tools")
	removeDryRun := removeCmd.Bool("dry-run", false, "show what would be removed")
	removeCmd.Parse(args)

	ls, err := state.LoadLocked()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()

	st := ls.State()

	removeTool := func(toolName string) bool {
		toolState, ok := st.Tools[toolName]
		if !ok {
			log.Default.Error("tool not found in state", "tool", toolName)
			return false
		}

		adapter := exec.Lookup(toolState.Method)
		if adapter == nil {
			log.Default.Warn("adapter not found for method", "tool", toolName, "method", toolState.Method)
			log.Default.Warn("manual remove required", "tool", toolName)
			return false
		}

		if !exec.CanRemove(adapter) {
			log.Default.Warn("manual remove required", "tool", toolName, "method", toolState.Method)
			return false
		}

		remover := adapter.(exec.Remover)
		mc := &schema.MethodCandidate{
			Kind:   toolState.Method,
			Config: toolState.Config,
		}
		tool := &schema.Tool{Name: toolName}

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

	hadFailure := false
	if *removeAll {
		for toolName := range st.Tools {
			if !removeTool(toolName) {
				hadFailure = true
			}
		}
	} else {
		if len(removeCmd.Args()) != 1 {
			log.Default.Error("usage: depengine remove <tool>")
			os.Exit(1)
		}
		if !removeTool(removeCmd.Arg(0)) {
			hadFailure = true
		}
	}

	if err := ls.Save(); err != nil {
		log.Default.Error("failed to update state", "error", err)
		os.Exit(3)
	}

	if hadFailure {
		os.Exit(1)
	}
}

// runForget removes a tool from state without attempting system removal.
func runForget(args []string) {
	forgetCmd := flag.NewFlagSet("forget", flag.ExitOnError)
	forgetCmd.Parse(args)

	if len(forgetCmd.Args()) != 1 {
		log.Default.Error("usage: depengine forget <tool>")
		os.Exit(1)
	}

	toolName := forgetCmd.Arg(0)
	ls, err := state.LoadLocked()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()

	st := ls.State()

	if _, ok := st.Tools[toolName]; !ok {
		log.Default.Error("tool not found in state", "tool", toolName)
		os.Exit(1)
	}

	delete(st.Tools, toolName)

	if err := ls.Save(); err != nil {
		log.Default.Error("save state", "error", err)
		os.Exit(3)
	}

	log.Default.Info("forgotten", "tool", toolName)
}

func runUndo(args []string) {
	undoCmd := flag.NewFlagSet("undo", flag.ExitOnError)
	undoList := undoCmd.Bool("list", false, "list available snapshots")
	undoSpecific := undoCmd.String("snapshot", "", "revert to specific snapshot file path")
	undoCmd.Parse(args)

	if *undoList {
		snapshots, err := state.ListSnapshots()
		if err != nil {
			log.Default.Error("list snapshots", "error", err)
			os.Exit(3)
		}
		if len(snapshots) == 0 {
			fmt.Println("Nenhum snapshot encontrado.")
			return
		}
		fmt.Println("Snapshots disponíveis:")
		for _, s := range snapshots {
			ts := s.Timestamp.Format("2006-01-02 15:04:05")
			fmt.Printf("  %s  %s  (%d ferramentas)\n", ts, filepath.Base(s.Path), s.ToolCount)
		}
		return
	}

	// Determine which snapshot to restore.
	var snapPath string
	if *undoSpecific != "" {
		snapPath = *undoSpecific
	} else {
		snapshots, err := state.ListSnapshots()
		if err != nil {
			log.Default.Error("list snapshots", "error", err)
			os.Exit(3)
		}
		if len(snapshots) == 0 {
			log.Default.Error("nenhum snapshot disponível para undo")
			os.Exit(1)
		}
		snapPath = snapshots[0].Path // most recent
	}

	// Load snapshot state.
	snapState, err := state.LoadSnapshot(snapPath)
	if err != nil {
		log.Default.Error("load snapshot", "error", err)
		os.Exit(3)
	}

	// Load current state under exclusive lock.
	ls, err := state.LoadLocked()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()

	curState := ls.State()

	// Find tools in current state but not in snapshot (these were added).
	var toRemove []string
	for name := range curState.Tools {
		if _, ok := snapState.Tools[name]; !ok {
			toRemove = append(toRemove, name)
		}
	}

	if len(toRemove) == 0 {
		log.Default.Info("nothing to undo (no tools were added since snapshot)")

		// Still restore the snapshot state (in case of config changes).
		curState.Tools = snapState.Tools
		curState.SchemaPath = snapState.SchemaPath
		curState.SchemaModifiedAt = snapState.SchemaModifiedAt
		if err := ls.Save(); err != nil {
			log.Default.Error("save state after undo", "error", err)
			os.Exit(3)
		}
		return
	}

	// Remove each tool that was added.
	hadFailure := false
	for _, name := range toRemove {
		toolState := curState.Tools[name]

		log.Default.Info("removing tool added after snapshot", "tool", name, "method", toolState.Method)

		adapter := exec.Lookup(toolState.Method)
		if adapter == nil {
			log.Default.Warn("adapter not found — manual removal may be needed", "tool", name, "method", toolState.Method)
			hadFailure = true
			continue
		}

		if !exec.CanRemove(adapter) {
			log.Default.Warn("adapter does not support automated removal — manual removal needed", "tool", name, "method", toolState.Method)
			hadFailure = true
			continue
		}

		remover := adapter.(exec.Remover)
		mc := &schema.MethodCandidate{
			Kind:   toolState.Method,
			Config: toolState.Config,
		}
		tool := &schema.Tool{Name: name}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		if err := remover.Remove(ctx, run.OSExecRunner{}, tool, mc); err != nil {
			log.Default.Error("remove failed during undo", "tool", name, "error", err)
			hadFailure = true
			cancel()
			continue
		}
		cancel()

		log.Default.Info("removed during undo", "tool", name)
	}

	// Restore snapshot as current state (overwrites remaining state).
	curState.Tools = snapState.Tools
	curState.SchemaPath = snapState.SchemaPath
	curState.SchemaModifiedAt = snapState.SchemaModifiedAt

	if err := ls.Save(); err != nil {
		log.Default.Error("save state after undo", "error", err)
		os.Exit(3)
	}

	log.Default.Info("undo concluído", "tools_removed", len(toRemove))

	if hadFailure {
		os.Exit(1)
	}
}

func runSBOM(args []string) {
	sbomCmd := flag.NewFlagSet("sbom", flag.ExitOnError)
	sbomFormat := sbomCmd.String("format", "cyclonedx", "output format: cyclonedx or spdx")
	sbomCmd.Parse(args)

	// Load state with shared lock (read-only).
	ls, err := state.LoadShared()
	if err != nil {
		log.Default.Error("load state", "error", err)
		os.Exit(3)
	}
	defer ls.Close()

	st := ls.State()

	var data []byte
	switch *sbomFormat {
	case "cyclonedx", "cyclonedx-json":
		data, err = sbom.ExportCycloneDX(st)
	case "spdx", "spdx-json":
		data, err = sbom.ExportSPDX(st)
	default:
		log.Default.Error("unsupported format", "format", *sbomFormat)
		fmt.Fprintf(os.Stderr, "Formatos suportados: cyclonedx, spdx\n")
		os.Exit(2)
	}

	if err != nil {
		log.Default.Error("generate sbom", "error", err)
		os.Exit(3)
	}

	fmt.Println(string(data))
}
// runDiff compares two state files and outputs the differences.
// Accepts 0-2 arguments:
//   - 0 args: compare current state (state.LoadShared) with a file from --other flag
//   - 1 arg:  compare current state with the file at the given path
//   - 2 args: compare the two files directly (no current state)
func runDiff(args []string) {
    diffCmd := flag.NewFlagSet("diff", flag.ExitOnError)
    diffOther := diffCmd.String("other", "", "path to other state file (used when no args)")
    diffJSON := diffCmd.Bool("json", false, "output as JSON")
    diffCmd.Parse(args)

    var aPath, bPath string
    var aState, bState *state.State
    var err error

    // Determine which files to compare.
    switch diffCmd.NArg() {
    case 0:
        // Compare current state with --other file.
        if *diffOther == "" {
            fmt.Fprintf(os.Stderr, "error: --other is required when no arguments are given\n")
            os.Exit(2)
        }
        aPath = state.DefaultPath()
        bPath = *diffOther
        ls, err := state.LoadShared()
        if err != nil {
            log.Default.Error("load current state", "error", err)
            os.Exit(3)
        }
        defer ls.Close()
        aState = ls.State()
        bState, err = state.LoadFrom(bPath)
        if err != nil {
            log.Default.Error("load other state", "path", bPath, "error", err)
            os.Exit(3)
        }
    case 1:
        // Compare current state with the given file.
        aPath = state.DefaultPath()
        bPath = diffCmd.Arg(0)
        ls, err := state.LoadShared()
        if err != nil {
            log.Default.Error("load current state", "error", err)
            os.Exit(3)
        }
        defer ls.Close()
        aState = ls.State()
        bState, err = state.LoadFrom(bPath)
        if err != nil {
            log.Default.Error("load other state", "path", bPath, "error", err)
            os.Exit(3)
        }
    case 2:
        // Compare the two given files directly.
        aPath = diffCmd.Arg(0)
        bPath = diffCmd.Arg(1)
        aState, err = state.LoadFrom(aPath)
        if err != nil {
            log.Default.Error("load first state", "path", aPath, "error", err)
            os.Exit(3)
        }
        bState, err = state.LoadFrom(bPath)
        if err != nil {
            log.Default.Error("load second state", "path", bPath, "error", err)
            os.Exit(3)
        }
    default:
        fmt.Fprintf(os.Stderr, "usage: depengine diff [--json] [--other <path>] [<file1> [<file2>]]\n")
        os.Exit(2)
    }

    // Compute the diff.
    items := state.Diff(aState, bState)
    if len(items) == 0 {
        if *diffJSON {
            fmt.Println("[]")
        } else {
            fmt.Println("Nenhuma diferença encontrada.")
        }
        return
    }

    // Output.
    if *diffJSON {
        enc := json.NewEncoder(os.Stdout)
        enc.SetIndent("", "  ")
        if err := enc.Encode(items); err != nil {
            log.Default.Error("encode JSON", "error", err)
            os.Exit(3)
        }
    } else {
        var onlyA, onlyB, diffCount int
        for _, item := range items {
            switch item.Side {
            case "only_a":
                onlyA++
            case "only_b":
                onlyB++
            case "different":
                diffCount++
            }
        }

        if onlyA > 0 {
            fmt.Println("=== Somente no atual ===")
            for _, item := range items {
                if item.Side == "only_a" {
                    fmt.Printf("  %s (%s, instalado %s)\n", item.Name, item.MethodA, item.InstalledAtA)
                }
            }
        }

        if onlyB > 0 {
            fmt.Println("=== Somente no outro ===")
            for _, item := range items {
                if item.Side == "only_b" {
                    fmt.Printf("  %s (%s, instalado %s)\n", item.Name, item.MethodB, item.InstalledAtB)
                }
            }
        }

        if diffCount > 0 {
            fmt.Println("=== Definição diferente ===")
            for _, item := range items {
                if item.Side == "different" {
                    fmt.Printf("  %s\n", item.Name)
                    fmt.Printf("    atual: %s (hash: %s)\n", item.MethodA, item.HashA)
                    fmt.Printf("    outro:  %s (hash: %s)\n", item.MethodB, item.HashB)
                }
            }
        }

        fmt.Printf("\n%d ferramentas diferem.\n", len(items))
    }
}

// loadSchema is the shared bootstrap for install/check: gather facts,
// resolve clan, build placeholder map, parse schema. Returns an error
// suitable for exitCodeForError.
func loadSchema(path string) (*schema.Schema, string, *engine.Facts, error) {
	// If path is a directory, look for schema.toml inside it.
	info, err := os.Stat(path)
	if err != nil {
		return nil, "", nil, err
	}
	if info.IsDir() {
		path = filepath.Join(path, "schema.toml")
	}

	facts, err := engine.GatherFacts(run.OSExecRunner{})
	if err != nil {
		return nil, "", nil, err
	}
	clan := engine.ResolveFamily(facts)
	s, err := schema.ParseSchema(path, schema.BuildMap(facts, clan))
	if err != nil {
		return nil, "", nil, err
	}
	if verr, warnings := schema.Validate(s, exec.RegisteredKinds()); verr != nil {
		return nil, "", nil, verr
	} else if len(warnings) > 0 {
		for _, w := range warnings {
			log.Default.Warn(w)
		}
	}
	return s, clan, facts, nil
}

// exitCodeForError maps common bootstrap errors to exit codes.
func exitCodeForError(err error) int {
	var schemaErr *schema.ParseSchemaError
	if errors.As(err, &schemaErr) {
		return 2 // schema error (malformed TOML, validation, etc.)
	}
	return 3 // runtime error (detect_os.sh not found, etc.)
}
// filteredByTags applies profile filtering: if profile is non-empty,
// only include tools that have no tags (universal) OR have the
// specified profile tag in their Tags slice.
func filteredByTags(tools map[string]*schema.Tool, profile string) map[string]*schema.Tool {
	if profile == "" {
		return tools
	}
	result := make(map[string]*schema.Tool, len(tools))
	for name, tool := range tools {
		if len(tool.Tags) == 0 {
			// Tools without tags are always included.
			result[name] = tool
			continue
		}
		for _, tag := range tool.Tags {
			if strings.EqualFold(tag, profile) {
				result[name] = tool
				break
			}
		}
	}
	return result
}

// filterTools applies --only, --skip, and --profile filters to the tool map.
// All filters are processed; the result is the intersection of all.
func filterTools(tools map[string]*schema.Tool, only, skip, profile string) map[string]*schema.Tool {
	if only == "" && skip == "" && profile == "" {
		return tools
	}
	skipSet := make(map[string]bool)
	for _, name := range strings.Split(skip, ",") {
		skipSet[strings.TrimSpace(name)] = true
	}
	filtered := make(map[string]*schema.Tool, len(tools))
	for name, tool := range tools {
		if skipSet[name] {
			continue
		}
		if only != "" && name != only {
			continue
		}
		filtered[name] = tool
	}

	// If --only was used, add transitive Requires closure so graph.Sort
	// does not fail with "requires X, which is not in schema".
	if only != "" {
		queue := []string{only}
		visited := map[string]bool{only: true}
		for len(queue) > 0 {
			name := queue[0]
			queue = queue[1:]
			if t, ok := tools[name]; ok {
				filtered[name] = t
				for _, req := range t.Requires {
					if !visited[req] {
						visited[req] = true
						queue = append(queue, req)
					}
				}
			}
		}
	}

	filtered = filteredByTags(filtered, profile)

	return filtered
}
func printUsage() {
	locale := i18n.GetLocale()
	switch locale {
	case "en":
		printUsageEN()
	default:
		printUsagePT()
	}
}

func printUsagePT() {
	fmt.Print(`depengine - Motor distro-agnóstico de instalação de dependências

Uso:
  depengine install [flags]        Instala ferramentas do schema.toml
  depengine update [flags]         Resolve versões e cria/atualiza schema.lock
  depengine check <tool>           Verifica se uma ferramenta está instalada
  depengine why <tool>             Mostra por que cada método seria usado/pulado
  depengine status [flags]         Mostra status das ferramentas instaladas
  depengine remove <tool>          Remove uma ferramenta instalada
  depengine forget <tool>         Remove do state sem desinstalar
  depengine graph [flags]         Mostra o grafo de dependências
  depengine validate [flags]       Valida schema.toml e ambiente
  depengine sbom [flags]           Exporta SBOM (CycloneDX/SPDX) do estado atual
  depengine diff [flags] [f1] [f2] Compara arquivos de estado entre máquinas
  depengine completion <shell>      Gera script de autocomplete (bash|zsh|fish)
  depengine version                Mostra a versão
  depengine help                   Mostra esta ajuda
  depengine undo [flags]          Reverte o último install (restaura snapshot anterior)

Flags globais:
  --log-level <nível>    Nível de log: debug, info, warn, error (default: info)
  --diagnose             Saída detalhada para depuração

Flags (install):
  --only <nome>          Instala apenas a ferramenta especificada (e dependências)
  --skip <nomes>         Pula ferramentas (separadas por vírgula)
  --profile <tag>        Filtra ferramentas por tag (minimal, desktop, server)
  --jobs <n>             Máximo de instalações concorrentes (default: 1)
  --allow-arbitrary-code  Suprime avisos de segurança para scripts build
  --frozen-lockfile      Falha se schema.lock estiver ausente ou desatualizado
  --dry-run              Mostra o que seria instalado sem executar
  --sort-by <tipo>       Ordena saída: tool, method, status (default: tool)

Flags (update):
  --schema <caminho>     Caminho para schema.toml (default: schema.toml)
  --only <nome>          Atualiza apenas a ferramenta especificada
  --skip <nomes>         Pula ferramentas (separadas por vírgula)
  --dry-run              Mostra o que seria atualizado sem salvar

Flags (check):
  --schema <caminho>     Caminho para schema.toml (default: schema.toml)

Flags (status):
  --schema <caminho>     Caminho para schema.toml (default: schema.toml)
  --orphans              Mostra apenas ferramentas não rastreadas no schema

Flags (graph):
  --schema <caminho>     Caminho para schema.toml (default: schema.toml)
  --format mermaid|dot|text  Formato de saída (default: text)
  --only <nome>          Mostra apenas a ferramenta especificada e suas dependências
  --skip <nomes>         Pula ferramentas (separadas por vírgula)
  --profile <tag>        Filtra ferramentas por tag

Flags (validate):
  --schema <caminho>     Caminho para schema.toml (default: schema.toml)
  --check-env            Verifica ambiente (comandos necessários para cada adapter)
  --format plain|json    Formato de saída (default: plain)
  --strict               Falha em warnings, não apenas erros

Flags (undo):
  --list                 Lista snapshots disponíveis
  --snapshot <caminho>   Restaura um snapshot específico em vez do mais recente

Flags (sbom):
  --format cyclonedx|spdx  Formato de saída (default: cyclonedx)

Flags (diff):
  --json                 Saída formatada em JSON
  --other <caminho>      Caminho do arquivo a comparar (usado quando sem argumentos)
Códigos de saída:
  0   Sucesso (todas as ferramentas ok)
  1   Alguma ferramenta falhou
  2   Erro de uso (flag inválida, argumento faltando)
  3   Erro de runtime (detect_os.sh não encontrado, etc.)`)
}

func printUsageEN() {
	fmt.Print(`depengine — tool manager

Usage:
  depengine install [flags]        Install tools from schema.toml
  depengine update [flags]         Resolve versions and create/update schema.lock
  depengine check <tool>           Check if a tool is installed
  depengine why <tool>             Show why each method would be used/skipped
  depengine status [flags]         Show installed tool status
  depengine remove <tool>          Remove an installed tool
  depengine forget <tool>          Remove from state without uninstalling
  depengine graph [flags]          Show dependency graph
  depengine validate [flags]       Validate schema.toml and environment
  depengine sbom [flags]           Export SBOM (CycloneDX/SPDX) from state
  depengine undo [flags]           Revert the last install (restore previous snapshot)
  depengine completion <shell>     Generate autocomplete script (bash|zsh|fish)
  depengine version                Show version
  depengine help                   Show this help

Global flags:
  --log-level <level>      Log level: debug, info, warn, error (default: info)
  --diagnose               Verbose output for debugging

Flags (install):
  --only <name>            Install only the specified tool (and dependencies)
  --skip <names>           Skip tools (comma-separated)
  --profile <tag>          Filter tools by tag (minimal, desktop, server)
  --jobs <n>               Max concurrent installations (default: 1)
  --allow-arbitrary-code   Suppress security warnings for build scripts
  --frozen-lockfile        Fail if schema.lock is missing or outdated
  --dry-run                Show what would be installed without installing
  --sort-by <type>         Sort output: tool, method, status (default: tool)

Flags (update):
  --schema <path>          Path to schema.toml (default: schema.toml)
  --only <name>            Update only the specified tool
  --skip <names>           Skip tools (comma-separated)
  --dry-run                Show what would be updated without saving

Flags (check):
  --schema <path>          Path to schema.toml (default: schema.toml)

Flags (status):
  --schema <path>          Path to schema.toml (default: schema.toml)
  --orphans                Show only tools not tracked in schema

Flags (graph):
  --schema <path>          Path to schema.toml (default: schema.toml)
  --format mermaid|dot|text  Output format (default: text)
  --only <name>            Show only the specified tool and its dependencies
  --skip <names>           Skip tools (comma-separated)
  --profile <tag>          Filter tools by tag

Flags (validate):
  --schema <path>          Path to schema.toml (default: schema.toml)
  --check-env              Check environment (commands required by each adapter)
  --format plain|json      Output format (default: plain)
  --strict                 Fail on warnings, not just errors

Flags (undo):
  --list                   List available snapshots
  --snapshot <path>        Restore a specific snapshot instead of the latest

Flags (sbom):
  --format cyclonedx|spdx  Output format (default: cyclonedx)

Exit codes:
  0   Success (all tools OK)
  1   Some tool failed
  2   Usage error (invalid flag, missing argument)
  3   Runtime error (detect_os.sh not found, etc.)`)
}

func initAdapters() {
	// Native adapter (canonical "native" kind, clan empty — resolved at
	// runtime by detectClan). Per-instance WithAdapters in the install
	// command shadows this with the authoritative clan from ResolveFamily.
	exec.Register(exec.NewNativeAdapter(""))
	// Also register manager-name aliases (apt, pacman, dnf, …) so that
	// schema entries like `apt = "fd-find"` resolve to the native adapter.
	exec.RegisterNativeManagerAliases()

	// Language adapters.
	lang.RegisterAll("paru")
	// Git adapter.
	exec.Register(git.NewGitAdapter())
	// HTTP adapter.
	exec.Register(httpdownload.NewHTTPAdapter())
}

func runValidate(args []string) {
	validateCmd := flag.NewFlagSet("validate", flag.ExitOnError)
	validateSchema := validateCmd.String("schema", "schema.toml", "path to schema.toml")
	validateCheckEnv := validateCmd.Bool("check-env", false, "check system environment for required tools")
	validateFormat := validateCmd.String("format", "text", "output format: text or json")
	validateStrict := validateCmd.Bool("strict", false, "treat warnings as errors")
	validateCmd.Parse(args)

	ctx := context.Background()

	// Parse the schema. Use an empty placeholder map so validation
	// doesn't require detect_os.sh to be installed.
	s, err := schema.ParseSchema(*validateSchema, map[string]string{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	// Collect validation results.
	knownKinds := exec.RegisteredKinds()
	result := validate.ValidateSchema(s, knownKinds)

	// Also run the basic schema.Validate checks and merge findings.
	verr, warnings := schema.Validate(s, knownKinds)
	if verr != nil {
		result.Add(validate.ValidationError{
			Code:    "E_UNKNOWN_METHOD",
			Field:   "tools",
			Message: verr.Error(),
		})
	}
	for _, w := range warnings {
		result.Add(validate.ValidationError{
			Code:    "W_UNKNOWN_METHOD",
			Field:   "tools",
			Message: w,
		})
	}

	// Optional environment check.
	if *validateCheckEnv {
		envResult := validate.CheckEnv(ctx, run.OSExecRunner{})
		for _, ch := range envResult.Checks {
			if !ch.Found {
				result.Add(validate.ValidationError{
					Code:    "W_ENV_MISSING",
					Field:   "environment",
					Message: ch.Message,
				})
			}
		}
	}

	// Output.
	if *validateFormat == "json" {
		// Simple JSON output.
		type jsonOutput struct {
			Errors   []validate.ValidationError `json:"errors"`
			Warnings []validate.ValidationError `json:"warnings"`
		}
		out := jsonOutput{
			Errors:   result.Errors,
			Warnings: result.Warnings,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			fmt.Fprintf(os.Stderr, "error encoding JSON: %v\n", err)
			os.Exit(3)
		}
	} else {
		for _, e := range result.Errors {
			fmt.Fprintf(os.Stderr, "error: %v\n", e)
		}
		for _, w := range result.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %v\n", w)
		}
		if len(result.Errors) == 0 && len(result.Warnings) == 0 {
			fmt.Fprintf(os.Stderr, "✓ schema is valid\n")
		}
	}

	exitCode := 0
	if result.HasErrors() {
		exitCode = 2
	} else if *validateStrict && len(result.Warnings) > 0 {
		exitCode = 1
	}
	os.Exit(exitCode)
}

func runCompletion(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: depengine completion bash|zsh|fish")
		os.Exit(2)
	}
	shell := args[0]

	// Try alongside the binary first (shipped together like detect_os.sh).
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "scripts", "depengine-completion."+shell)
		if data, err := os.ReadFile(candidate); err == nil {
			fmt.Print(string(data))
			return
		}
	}

	// Fallback: CWD-relative path (development mode).
	scriptPath := filepath.Join("scripts", "depengine-completion."+shell)
	if data, err := os.ReadFile(scriptPath); err == nil {
		fmt.Print(string(data))
		return
	}

	fmt.Fprintf(os.Stderr, "error: completion script not found for %s\n", shell)
	os.Exit(2)
}
