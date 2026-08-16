package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/ecosystem"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/git"
	"github.com/Khorea1/depengine/pkg/httpdownload"
	"github.com/Khorea1/depengine/pkg/lock"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/state"
)

// upgradeResult records the outcome of upgrading one tool.
type upgradeResult struct {
	Tool    string `json:"tool"`
	Status  string `json:"status"` // "upgraded", "skipped", "failed", "would_upgrade"
	OldVer  string `json:"old_version,omitempty"`
	NewVer  string `json:"new_version,omitempty"`
	Method  string `json:"method,omitempty"`
	Error   string `json:"error,omitempty"`
}

// runUpgrade upgrades installed tools whose recorded version is outdated
// relative to the pinned version in depengine.lock. For each outdated tool,
// it calls adapter.Remove followed by adapter.Install, then updates state.
func runUpgrade(args []string) {
	upgradeCmd := flag.NewFlagSet("upgrade", flag.ExitOnError)
	upgradeSchema := upgradeCmd.String("schema", defaultSchemaPath(), "path to schema.toml")
	upgradeManifest := upgradeCmd.String("manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	upgradeNoManifest := upgradeCmd.Bool("no-manifest", false, "disable personal manifest (default: auto-detect)")
	upgradeDryRun := upgradeCmd.Bool("dry-run", false, "show what would be upgraded without making changes")
	upgradeOnly := upgradeCmd.String("only", "", "only upgrade specific tool")
	upgradeForce := upgradeCmd.Bool("force", false, "skip confirmation prompt")
	upgradeJSON := upgradeCmd.Bool("json", false, "JSON output")
	upgradeQuiet := upgradeCmd.Bool("quiet", false, "suppress per-tool status lines")
	upgradeAllowArbitrary := upgradeCmd.Bool("allow-arbitrary-code", false, "suppress security warnings for build scripts / arbitrary code")
	upgradeCmd.Parse(args)

	ctx := context.Background()
	lg := log.Default

	noManifest := *upgradeNoManifest
	manifestPath := *upgradeManifest
	manifestAuto := false
	if !noManifest && manifestPath == "" {
		manifestPath = config.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}

	s, clan, facts, manifestCount, err := loadSchemaWithManifest(*upgradeSchema, manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "error: %s not found\n", *upgradeSchema)
			fmt.Fprintf(os.Stderr, "Run 'depengine init' to create one, or point --schema to an existing file.\n")
			os.Exit(1)
		}
		lg.Error("load schema", "error", err)
		os.Exit(exitCodeForError(err))
	}
	if manifestAuto && manifestCount > 0 {
		fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, manifestCount)
	}
	if helper := s.Defaults.AurHelper; helper != "" {
		ecosystem.ReconfigureAUR(helper)
	}

	// Load lockfile.
	lockPath := lock.DefaultPath(*upgradeSchema)
	lk, err := lock.Load(lockPath)
	if err != nil && !os.IsNotExist(err) {
		lg.Warn("load lock", "error", err)
	}
	if lk == nil {
		fmt.Fprintln(os.Stderr, "No lockfile found. Run 'depengine update' first to resolve and pin versions.")
		os.Exit(1)
	}

	// Acquire state lock.
	ls, err := state.LoadLocked()
	if err != nil {
		lg.Error("state lock", "error", err)
		os.Exit(3)
	}
	defer ls.Close()
	st := ls.State()

	// Build executor for Install calls.
	schemaFile, err := os.Stat(*upgradeSchema)
	if err != nil {
		lg.Error("stat schema", "error", err)
		os.Exit(1)
	}
	ex := exec.New()
	exec.WithDefaultMethodOrder(s.Defaults.MethodOrder)(ex)
	exec.WithAdapters(
		git.NewGitAdapter(),
		httpdownload.NewHTTPAdapter(),
		exec.NewNativeAdapter(clan),
	)(ex)
	exec.WithSchemaInfo(*upgradeSchema, schemaFile.ModTime())(ex)
	exec.WithLogger(lg)(ex)
	exec.WithRunner(run.NewLoggingRunner(run.OSExecRunner{}, lg))
	exec.WithFacts(facts)
	if *upgradeDryRun {
		exec.WithDryRun()(ex)
	}
	if *upgradeAllowArbitrary {
		exec.WithAllowArbitraryCode()(ex)
	}
	if *upgradeQuiet {
		exec.WithQuiet()(ex)
	}

	// Apply lock pins to the schema so Install sees resolved versions.
	lock.Apply(s, lk)

	// Identify outdated tools.
	type outdatedTool struct {
		name       string
		ts         state.ToolState
		pinnedVer  string
		tool       *config.Tool
		methodKind string
	}
	var outdated []outdatedTool

	for name, ts := range st.Tools {
		// Filter by --only.
		if *upgradeOnly != "" && name != *upgradeOnly {
			continue
		}

		// Find the tool in the schema — need it for Install.
		tool, ok := s.Tools[name]
		if !ok {
			// Tool in state but not in schema — can't upgrade (no method config).
			continue
		}

		methodKind := ts.MethodKind
		if methodKind == "" {
			methodKind = ts.Method
		}

		// Look up pinned version.
		pin, ok := lockPinFor(lk, name, methodKind)
		if !ok || pin.Latest == "" {
			continue
		}

		if ts.Version == "" {
			// Unknown installed version — can't determine drift. Skip.
			continue
		}

		if !state.VersionOutdated(ts.Version, pin.Latest) {
			continue
		}

		outdated = append(outdated, outdatedTool{
			name:       name,
			ts:         ts,
			pinnedVer:  pin.Latest,
			tool:       tool,
			methodKind: methodKind,
		})
	}

	if len(outdated) == 0 {
		if *upgradeJSON {
			fmt.Println(`{"upgraded":0,"skipped":0,"failed":0,"results":[]}`)
		} else {
			fmt.Fprintln(os.Stderr, "All installed tools are up to date.")
		}
		return
	}

	// Sort for deterministic output.
	sort.Slice(outdated, func(i, j int) bool {
		return outdated[i].name < outdated[j].name
	})

	fmt.Fprintf(os.Stderr, "depengine upgrade: distro=%s clan=%s arch=%s outdated=%d\n",
		facts.DistroID, clan, facts.TargetArch, len(outdated))

	// Confirmation prompt (unless --force or --dry-run or --json).
	if !*upgradeForce && !*upgradeDryRun && !*upgradeJSON && isInteractive() {
		fmt.Fprintln(os.Stderr, "\nThe following tools will be upgraded:")
		for _, ot := range outdated {
			fmt.Fprintf(os.Stderr, "  %s: %s → %s\n", ot.name, ot.ts.Version, ot.pinnedVer)
		}
		fmt.Fprint(os.Stderr, "\nProceed? [y/N] ")
		var input string
		fmt.Fscanln(os.Stdin, &input)
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Fprintln(os.Stderr, "Aborted.")
			os.Exit(0)
		}
	}

	// Upgrade each outdated tool: Remove then Install.
	var results []upgradeResult
	upgraded, failed, skipped := 0, 0, 0

	for _, ot := range outdated {
		res := upgradeResult{
			Tool:   ot.name,
			OldVer: ot.ts.Version,
			Method: ot.ts.Method,
		}

		adapter := ex.LookupAdapter(ot.methodKind)
		if adapter == nil {
			res.Status = "failed"
			res.Error = fmt.Sprintf("no adapter for method %q", ot.methodKind)
			if !*upgradeQuiet || *upgradeJSON {
				fmt.Fprintf(os.Stderr, "  ✗  %s: %s\n", ot.name, res.Error)
			}
			results = append(results, res)
			failed++
			continue
		}

		// Step 1: Remove.
		if *upgradeDryRun {
			res.Status = "would_upgrade"
			res.NewVer = ot.pinnedVer
			if !*upgradeQuiet || *upgradeJSON {
				fmt.Fprintf(os.Stderr, "  →  %s: %s → %s (dry-run)\n", ot.name, ot.ts.Version, ot.pinnedVer)
			}
			results = append(results, res)
			skipped++
			continue
		}

		if !exec.CanRemove(adapter) {
			// Adapter can't remove — skip with a clear message.
			res.Status = "skipped"
			res.Error = fmt.Sprintf("adapter %q does not support removal — remove manually and reinstall", ot.methodKind)
			if !*upgradeQuiet || *upgradeJSON {
				fmt.Fprintf(os.Stderr, "  –  %s: %s\n", ot.name, res.Error)
			}
			results = append(results, res)
			skipped++
			continue
		}

		remover := adapter.(exec.Remover)
		mc := &config.MethodCandidate{
			Kind:   ot.methodKind,
			Config: ot.ts.Config,
		}
		// Recover method config from the schema if state config is empty.
		if ot.ts.Config == nil {
			mc.Config = findMethodConfig(ot.tool, ot.methodKind)
		}

		removeCtx, removeCancel := context.WithTimeout(ctx, 2*time.Minute)
		err := remover.Remove(removeCtx, run.OSExecRunner{}, ot.tool, mc)
		removeCancel()
		if err != nil {
			res.Status = "failed"
			res.Error = fmt.Sprintf("remove failed: %v", err)
			if !*upgradeQuiet || *upgradeJSON {
				fmt.Fprintf(os.Stderr, "  ✗  %s: %s\n", ot.name, res.Error)
			}
			results = append(results, res)
			failed++
			continue
		}

		// Step 2: Install.
		// Find the method candidate from the schema for this kind.
		methodOrder := config.EffectiveMethodOrder(ot.tool, ex.DefaultMethodOrder(), ex.NativeManagerName())
		installMC := findMethodCandidate(ot.tool, ot.methodKind, methodOrder)
		if installMC == nil {
			// Fallback: use the state config.
			installMC = mc
		}

		installCtx, installCancel := context.WithTimeout(ctx, 10*time.Minute)
		err = adapter.Install(installCtx, run.OSExecRunner{}, ot.tool, installMC)
		installCancel()
		if err != nil {
			res.Status = "failed"
			res.Error = fmt.Sprintf("reinstall failed: %v", err)
			if !*upgradeQuiet || *upgradeJSON {
				fmt.Fprintf(os.Stderr, "  ✗  %s: %s\n", ot.name, res.Error)
			}
			results = append(results, res)
			failed++
			// Tool was removed but reinstall failed — update state to reflect removal.
			delete(st.Tools, ot.name)
			continue
		}

		// Step 3: Probe version.
		newVer := probeVersion(adapter, ot.tool, installMC)

		// Step 4: Update state.
		newTS := state.ToolState{
			Method:          ot.ts.Method,
			MethodKind:      ot.methodKind,
			InstalledAt:      time.Now().UTC().Format(time.RFC3339),
			PostinstallDone: ot.ts.PostinstallDone,
			DefinitionHash:  state.DefinitionHash(ot.tool),
			Version:         newVer,
			Config:          installMC.Config,
		}
		if newVer == "" {
			newTS.Version = ot.pinnedVer
		}
		st.Tools[ot.name] = newTS

		res.Status = "upgraded"
		res.NewVer = newVer
		if res.NewVer == "" {
			res.NewVer = ot.pinnedVer
		}
		if !*upgradeQuiet {
			fmt.Fprintf(os.Stderr, "  ✓  %s: %s → %s\n", ot.name, ot.ts.Version, res.NewVer)
		}
		results = append(results, res)
		upgraded++
	}

	// Save state (unless dry-run).
	if !*upgradeDryRun {
		if err := ls.Save(); err != nil {
			lg.Error("state save failed", "error", err)
			os.Exit(3)
		}
	}

	// Output.
	if *upgradeJSON {
		out := map[string]any{
			"upgraded": upgraded,
			"skipped":  skipped,
			"failed":   failed,
			"results":  results,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Fprintf(os.Stderr, "\nUpgrade complete: %d upgraded, %d skipped, %d failed\n", upgraded, skipped, failed)
	}

	if failed > 0 {
		os.Exit(1)
	}
}

// findMethodConfig extracts the config map for the first method matching kind.
func findMethodConfig(tool *config.Tool, kind string) map[string]any {
	for _, m := range tool.Methods {
		if m.Kind == kind {
			return m.Config
		}
	}
	return nil
}

// findMethodCandidate returns the MethodCandidate from the tool's methods
func findMethodCandidate(tool *config.Tool, kind string, methodOrder []string) *config.MethodCandidate {
	ordered := config.OrderMethods(tool.Methods, methodOrder)
	for _, m := range ordered {
		if m.Kind == kind {
			return m
		}
	}
	return nil
}

// probeVersion calls the adapter's InstalledVersion if it implements Versioner.
func probeVersion(adapter exec.Adapter, tool *config.Tool, mc *config.MethodCandidate) string {
	v, ok := adapter.(exec.Versioner)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ver, err := v.InstalledVersion(ctx, run.OSExecRunner{}, tool, mc)
	if err != nil || ver == "" {
		return ""
	}
	return ver
}
