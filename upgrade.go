package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/container"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/git"
	"github.com/Khorea1/depengine/internal/httpdownload"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

// upgradeResult records the outcome of upgrading one tool.
type upgradeResult struct {
	Tool   string `json:"tool"`
	Status string `json:"status"` // "upgraded", "skipped", "failed", "would_upgrade"
	OldVer string `json:"old_version,omitempty"`
	NewVer string `json:"new_version,omitempty"`
	Method string `json:"method,omitempty"`
	Error  string `json:"error,omitempty"`
}

// newUpgradeCmd builds `depengine upgrade`.
func newUpgradeCmd() *cobra.Command {
	upgradeSchema := new(string)
	upgradeManifest := new(string)
	upgradeNoManifest := new(bool)
	upgradeDryRun := new(bool)
	upgradeOnly := new(string)
	upgradeForce := new(bool)
	upgradeJSON := new(bool)
	upgradeQuiet := new(bool)
	upgradeAllowArbitrary := new(bool)

	cmd := &cobra.Command{
		Use:     "upgrade",
		Short:   ifPT("Atualizar ferramentas para as versões do depengine.lock", "Upgrade installed tools to pinned versions"),
		GroupID: groupManage,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpgrade(cmd.Context(), upgradeSchema, upgradeManifest, upgradeNoManifest, upgradeDryRun, upgradeOnly, upgradeForce, upgradeJSON, upgradeQuiet, upgradeAllowArbitrary)
		},
	}
	f := cmd.Flags()
	f.StringVar(upgradeSchema, "schema", defaultSchemaPath(), "path to schema.toml")
	f.StringVar(upgradeManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(upgradeNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	f.BoolVar(upgradeDryRun, "dry-run", false, "show what would be upgraded without making changes")
	f.StringVar(upgradeOnly, "only", "", "only upgrade specific tool")
	f.BoolVar(upgradeForce, "force", false, "skip confirmation prompt")
	f.BoolVar(upgradeJSON, "json", false, "JSON output")
	f.BoolVar(upgradeQuiet, "quiet", false, "suppress per-tool status lines")
	f.BoolVar(upgradeAllowArbitrary, "allow-arbitrary-code", false, "permit hooks, build scripts, and other arbitrary code execution")
	return cmd
}

// runUpgrade upgrades installed tools whose recorded version is outdated
// relative to the pinned version in depengine.lock. For each outdated tool,
// it calls adapter.Remove followed by adapter.Install, then updates state.
// Body unchanged from the pre-Cobra version — only the flag declarations
// above it moved.
func runUpgrade(ctx context.Context, upgradeSchema, upgradeManifest *string, upgradeNoManifest, upgradeDryRun *bool, upgradeOnly *string, upgradeForce, upgradeJSON, upgradeQuiet, upgradeAllowArbitrary *bool) error {
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
			return exitWithCode(1)
		}
		lg.Error("load schema", "error", err)
		return exitWithCode(exitCodeForError(err))
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
		return exitWithCode(1)
	}

	// Upgrade dry-run only needs a stable snapshot of state and must not create
	// the state lock file. State saves use atomic rename, so an unlocked read
	// observes a complete old or new file. Real upgrades keep the exclusive
	// lock for the read-modify-write transaction.
	var (
		st *state.State
		ls *state.LockedState
	)
	if *upgradeDryRun {
		st, err = state.Load()
	} else {
		ls, err = state.LoadLocked()
		if err == nil {
			defer ls.Close()
			st = ls.State()
		}
	}
	if err != nil {
		lg.Error("load state", "error", err)
		return exitWithCode(3)
	}

	// Build executor for Install calls.
	schemaFile, err := os.Stat(*upgradeSchema)
	if err != nil {
		lg.Error("stat schema", "error", err)
		return exitWithCode(1)
	}
	ex := exec.New()
	exec.WithDefaultMethodOrder(s.Defaults.MethodOrder)(ex)
	exec.WithAdapters(
		git.NewGitAdapter(),
		httpdownload.NewHTTPAdapter(),
		container.NewContainerAdapter(),
		exec.NewNativeAdapter(clan),
	)(ex)
	exec.WithSchemaInfo(*upgradeSchema, schemaFile.ModTime())(ex)
	exec.WithLogger(lg)(ex)
	runner := run.NewLoggingRunner(run.OSExecRunner{}, lg)
	exec.WithRunner(runner)(ex)
	exec.WithFacts(facts)(ex)
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
		method     *config.MethodCandidate
		methodKind string
	}
	var (
		outdated          []outdatedTool
		discoveryFailures []upgradeResult
	)

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

		// Resolve the exact schema candidate represented by state before looking
		// up its pin. Same-kind candidates have independent lock identities; an
		// arbitrary first match can upgrade from/to the wrong artifact.
		method, methodErr := findTrackedMethodCandidate(tool, ts, ex.DefaultMethodOrder(), ex.NativeManagerName())
		if methodErr != nil {
			if hasPinnedVersionForKind(lk, name, methodKind) {
				discoveryFailures = append(discoveryFailures, upgradeResult{
					Tool: name, Status: "failed", OldVer: ts.Version, Method: ts.Method,
					Error: fmt.Sprintf("cannot resolve tracked candidate: %v", methodErr),
				})
			}
			continue
		}
		pin, ok := lockPinForCandidate(lk, name, tool, method)
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
			method:     method,
			methodKind: methodKind,
		})
	}

	if len(outdated) == 0 && len(discoveryFailures) == 0 {
		if *upgradeJSON {
			fmt.Println(`{"upgraded":0,"skipped":0,"failed":0,"would_upgrade":0,"results":[]}`)
		} else {
			fmt.Fprintln(os.Stderr, "All installed tools are up to date.")
		}
		return nil
	}

	// Sort for deterministic output.
	sort.Slice(outdated, func(i, j int) bool {
		return outdated[i].name < outdated[j].name
	})

	c := newCLIStyle(os.Stderr)

	printKV(c, "depengine upgrade",
		[2]string{"schema", *upgradeSchema},
		[2]string{"target", fmt.Sprintf("%s (%s) · %s", facts.DistroID, clan, facts.TargetArch)},
		[2]string{"outdated", fmt.Sprintf("%d", len(outdated))},
	)

	// Confirmation prompt (unless --force or --dry-run or --json).
	if !*upgradeForce && !*upgradeDryRun && !*upgradeJSON && isInteractive() {
		// Aligned name column plus a dimmed old→new version pair: the list
		// is a risk review ("do I accept these bumps?"), so the decision-
		// relevant data (name, how big the jump is) must line up.
		nameW := 0
		for _, ot := range outdated {
			if len(ot.name) > nameW {
				nameW = len(ot.name)
			}
		}
		fmt.Fprintln(os.Stderr, c.bold("The following tools will be upgraded:"))
		for _, ot := range outdated {
			fmt.Fprintf(os.Stderr, "  %s  %s → %s\n",
				c.cyan(padRight(ot.name, nameW)),
				c.dim(ot.ts.Version), c.green(ot.pinnedVer))
		}
		fmt.Fprint(os.Stderr, "\nProceed? [y/N] ")
		var input string
		fmt.Fscanln(os.Stdin, &input)
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "y" && input != "yes" {
			fmt.Fprintln(os.Stderr, "Aborted.")
			return nil
		}
	}

	// Upgrade each outdated tool: preflight, then Remove and Install. Candidate
	// discovery failures are already terminal and participate in the same report.
	results := append([]upgradeResult(nil), discoveryFailures...)
	upgraded, failed, skipped, wouldUpgrade := 0, len(discoveryFailures), 0, 0
	for _, res := range discoveryFailures {
		if !*upgradeQuiet {
			c.fail("%s: %s", res.Tool, res.Error)
		}
	}

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
			if !*upgradeQuiet {
				c.fail("%s: %s", ot.name, res.Error)
			}
			results = append(results, res)
			failed++
			continue
		}

		// The legacy upgrade path calls Remove/Install directly. Fail closed on
		// semantics it cannot yet preserve instead of removing a working tool and
		// discovering the mismatch during reinstall.
		if err := preflightDirectUpgrade(ctx, runner, facts, ot.tool, ot.method, adapter, *upgradeAllowArbitrary); err != nil {
			res.Status = "failed"
			res.Error = fmt.Sprintf("upgrade preflight failed: %v", err)
			if !*upgradeQuiet {
				c.fail("%s: %s", ot.name, res.Error)
			}
			results = append(results, res)
			failed++
			continue
		}

		// Step 1: Remove.
		if *upgradeDryRun {
			res.Status = "would_upgrade"
			res.NewVer = ot.pinnedVer
			if !*upgradeQuiet {
				c.arrow("%s: %s → %s (dry-run)", ot.name, ot.ts.Version, ot.pinnedVer)
			}
			results = append(results, res)
			wouldUpgrade++
			continue
		}

		if !exec.CanRemove(adapter) {
			// Adapter can't remove — skip with a clear message.
			res.Status = "skipped"
			res.Error = fmt.Sprintf("adapter %q does not support removal — remove manually and reinstall", ot.methodKind)
			if !*upgradeQuiet {
				c.skip("%s: %s", ot.name, res.Error)
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

		tr := runner.WithContext(run.Context{Tool: ot.name, Method: ot.methodKind})
		removeCtx, removeCancel := context.WithTimeout(ctx, 2*time.Minute)
		err := remover.Remove(removeCtx, tr, ot.tool, mc)
		removeCancel()
		if err != nil {
			res.Status = "failed"
			res.Error = fmt.Sprintf("remove failed: %v", err)
			if !*upgradeQuiet {
				c.fail("%s: %s", ot.name, res.Error)
			}
			results = append(results, res)
			failed++
			continue
		}

		// Step 2: Install the exact candidate resolved before the destructive
		// transition. Never fall back to another candidate of the same kind.
		installMC := ot.method

		installCtx, installCancel := context.WithTimeout(ctx, 10*time.Minute)
		err = adapter.Install(installCtx, tr, ot.tool, installMC)
		installCancel()
		if err != nil {
			res.Status = "failed"
			res.Error = fmt.Sprintf("reinstall failed: %v", err)
			if !*upgradeQuiet {
				c.fail("%s: %s", ot.name, res.Error)
			}
			results = append(results, res)
			failed++
			// Tool was removed but reinstall failed. Release its shared-resource
			// claims so state does not pretend a missing tool still holds refs; keep
			// newly zero-ref resources on the host for explicit retry/cleanup.
			if releaseErr := recordFailedUpgradeRemoval(st, ot.name); releaseErr != nil {
				lg.Error("release failed-upgrade resources", "tool", ot.name, "error", releaseErr)
			}
			continue
		}

		// Step 3: Probe version.
		newVer := probeVersion(ctx, adapter, tr, ot.tool, installMC)

		// Step 4: Update state.
		newTS := upgradedToolState(ot.ts, ot.methodKind, ot.tool, installMC, newVer, ot.pinnedVer, time.Now().UTC())
		st.Tools[ot.name] = newTS

		res.Status = "upgraded"
		res.NewVer = newVer
		if res.NewVer == "" {
			res.NewVer = ot.pinnedVer
		}
		if !*upgradeQuiet {
			c.ok("%s: %s → %s", ot.name, ot.ts.Version, res.NewVer)
		}
		results = append(results, res)
		upgraded++
	}

	// Save state (unless dry-run).
	if !*upgradeDryRun {
		if err := ls.Save(); err != nil {
			lg.Error("state save failed", "error", err)
			return exitWithCode(3)
		}
	}

	// Output.
	if *upgradeJSON {
		out := map[string]any{
			"upgraded":      upgraded,
			"skipped":       skipped,
			"failed":        failed,
			"would_upgrade": wouldUpgrade,
			"results":       results,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		// Footer mirrors the ✓/✗/–/→ vocabulary of the per-tool lines: a
		// count only gets a colored marker when it's non-zero, so a clean run
		// is one quiet green line and a failure is impossible to miss.
		fmt.Fprintln(os.Stderr)
		var parts []string
		if *upgradeDryRun {
			if wouldUpgrade > 0 {
				parts = append(parts, c.cyan(fmt.Sprintf("%d would upgrade", wouldUpgrade)))
			}
		} else if upgraded > 0 {
			parts = append(parts, c.green(fmt.Sprintf("%d upgraded", upgraded)))
		}
		if skipped > 0 {
			parts = append(parts, c.yellow(fmt.Sprintf("%d skipped", skipped)))
		}
		if failed > 0 {
			parts = append(parts, c.red(fmt.Sprintf("%d failed", failed)))
		}
		if len(parts) == 0 {
			parts = append(parts, "nothing to do")
		}
		fmt.Fprintf(os.Stderr, "  %s\n", strings.Join(parts, "  ·  "))
	}

	if failed > 0 {
		return exitWithCode(1)
	}
	return nil
}

func hasPinnedVersionForKind(l *lock.Lock, toolName, kind string) bool {
	if l == nil || kind == "" {
		return false
	}
	prefix := toolName + "/" + kind + "/"
	for key, pin := range l.Tools {
		if strings.HasPrefix(key, prefix) && pin.Latest != "" {
			return true
		}
	}
	return false
}

func upgradedToolState(previous state.ToolState, methodKind string, tool *config.Tool, method *config.MethodCandidate, observedVersion, pinnedVersion string, installedAt time.Time) state.ToolState {
	version := observedVersion
	if version == "" {
		version = pinnedVersion
	}
	return state.ToolState{
		Method:          previous.Method,
		MethodKind:      methodKind,
		InstalledAt:     installedAt.UTC().Format(time.RFC3339),
		PostinstallDone: previous.PostinstallDone,
		DefinitionHash:  state.DefinitionHash(tool),
		Version:         version,
		RootRequested:   previous.RootRequested,
		Config:          method.Config,
	}
}

// recordFailedUpgradeRemoval updates durable ownership after a destructive
// upgrade removed the tracked tool but reinstall failed. Shared resources are
// released from the missing dependent, but last-reference resources are kept
// as explicit zero-ref state instead of triggering more host mutation from an
// already-failed upgrade.
func recordFailedUpgradeRemoval(st *state.State, toolName string) error {
	if st == nil {
		return fmt.Errorf("state is required")
	}
	release, err := plan.ReleaseDependentResources(st.OwnedResources, toolName)
	if err != nil {
		return err
	}
	st.OwnedResources = release.Updated
	delete(st.Tools, toolName)
	return nil
}

func preflightDirectUpgrade(ctx context.Context, runner run.Runner, facts *engine.Facts, tool *config.Tool, method *config.MethodCandidate, adapter exec.Adapter, allowArbitrary bool) error {
	if tool == nil || method == nil || adapter == nil {
		return fmt.Errorf("tool, method, and adapter are required")
	}
	if method.When != nil && !method.When.Match(facts) {
		return fmt.Errorf("tracked candidate no longer matches its when condition")
	}
	if _, err := exec.CandidatePlanIntent(tool, method); err != nil {
		return err
	}
	if len(method.Sources) > 0 {
		return fmt.Errorf("candidate declares sources; transactional upgrade preparation is required")
	}
	if len(method.Requires) > 0 {
		return fmt.Errorf("candidate declares method.requires; transactional upgrade preparation is required")
	}
	if len(tool.EffectiveRequires(facts)) > 0 {
		return fmt.Errorf("tool declares requires; transactional upgrade dependency handling is required")
	}
	if len(tool.PreInstall) > 0 || len(tool.PostInstall) > 0 {
		return fmt.Errorf("candidate has lifecycle hooks; direct upgrade cannot preserve hook semantics")
	}
	if !allowArbitrary && exec.CandidateRunsArbitraryCode(tool, method) {
		return fmt.Errorf("candidate may execute arbitrary code; pass --allow-arbitrary-code to permit it")
	}
	probeRunner := runner
	if lr, ok := runner.(*run.LoggingRunner); ok {
		probeRunner = lr.WithContext(run.Context{Tool: tool.Name, Method: method.Kind, Probe: true})
	}
	if !adapter.Available(ctx, probeRunner) {
		return fmt.Errorf("adapter %q is unavailable", method.Kind)
	}
	if !adapter.Check(ctx, probeRunner, tool, method) {
		return fmt.Errorf("tracked installation is not present; run install/repair instead of destructive upgrade")
	}
	if checker, ok := adapter.(exec.AvailabilityChecker); ok && !checker.CheckAvailable(ctx, probeRunner, tool, method) {
		return fmt.Errorf("target is not available from configured repositories")
	}
	if !exec.CanRemove(adapter) {
		return fmt.Errorf("adapter %q does not support removal", method.Kind)
	}
	return nil
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

// findMethodCandidate returns the first selected MethodCandidate of kind. It is
// retained for non-stateful callers; destructive state reconciliation must use
// findTrackedMethodCandidate so duplicate same-kind candidates cannot be picked
// arbitrarily.
func findMethodCandidate(tool *config.Tool, kind string, defaultOrder []string, nativeManagerName string) *config.MethodCandidate {
	ordered := config.SelectMethods(tool, defaultOrder, nativeManagerName)
	for _, m := range ordered {
		if m.Kind == kind {
			return m
		}
	}
	return nil
}

// findTrackedMethodCandidate resolves the exact currently-selected schema
// candidate represented by durable ToolState. Candidate identity comes from
// findStateMethodCandidate; this additional gate ensures the current method
// policy still selects it before a destructive upgrade.
func findTrackedMethodCandidate(tool *config.Tool, ts state.ToolState, defaultOrder []string, nativeManagerName string) (*config.MethodCandidate, error) {
	candidate, err := findStateMethodCandidate(tool, ts)
	if err != nil {
		return nil, err
	}
	for _, selected := range config.SelectMethods(tool, defaultOrder, nativeManagerName) {
		if selected == candidate {
			return candidate, nil
		}
	}
	display := candidate.Kind
	if candidate.Label != "" {
		display = candidate.Label
	}
	return nil, fmt.Errorf("tracked candidate %q (kind %q) is not selected by the current schema", display, candidate.Kind)
}

// probeVersion calls the adapter's InstalledVersion if it implements Versioner.
func probeVersion(ctx context.Context, adapter exec.Adapter, runner run.Runner, tool *config.Tool, mc *config.MethodCandidate) string {
	v, ok := adapter.(exec.Versioner)
	if !ok {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	ver, err := v.InstalledVersion(ctx, runner, tool, mc)
	if err != nil || ver == "" {
		return ""
	}
	return ver
}
