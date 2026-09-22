package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// upgradeOutdatedTool is a state-tracked tool whose installed version lags
// the lockfile pin, with the exact schema candidate resolved before any
// destructive transition.
type upgradeOutdatedTool struct {
	name       string
	ts         state.ToolState
	pinnedVer  string
	tool       *config.Tool
	method     *config.MethodCandidate
	methodKind string
}

// upgradeOptions carries dereferenced upgrade flag values between phases.
type upgradeOptions struct {
	schema         string
	manifest       string
	only           string
	noManifest     bool
	dryRun         bool
	force          bool
	jsonOut        bool
	quiet          bool
	allowArbitrary bool
}

// upgradeCounts tallies per-tool outcomes for the final report.
type upgradeCounts struct {
	upgraded     int
	skipped      int
	failed       int
	wouldUpgrade int
}

// newUpgradeOptions dereferences the CLI flag pointers into one value.
func newUpgradeOptions(upgradeSchema, upgradeManifest *string, upgradeNoManifest, upgradeDryRun *bool, upgradeOnly *string, upgradeForce, upgradeJSON, upgradeQuiet, upgradeAllowArbitrary *bool) upgradeOptions {
	return upgradeOptions{
		schema:         *upgradeSchema,
		manifest:       *upgradeManifest,
		only:           *upgradeOnly,
		noManifest:     *upgradeNoManifest,
		dryRun:         *upgradeDryRun,
		force:          *upgradeForce,
		jsonOut:        *upgradeJSON,
		quiet:          *upgradeQuiet,
		allowArbitrary: *upgradeAllowArbitrary,
	}
}

// resolveUpgradeManifestPath mirrors the --manifest/--no-manifest
// auto-detection: an explicit flag wins, --no-manifest disables lookup,
// otherwise the default personal manifest path applies when set.
func resolveUpgradeManifestPath(noManifest bool, flag string) (string, bool) {
	if noManifest || flag != "" {
		return flag, false
	}
	if def := config.DefaultManifestPath(); def != "" {
		return def, true
	}
	return "", false
}

// loadUpgradeState snapshots installed-tool state. Dry-runs take an unlocked
// read (never creating the state lock file — saves use atomic rename, so an
// unlocked read observes a complete old or new file); real upgrades hold the
// exclusive lock for the read-modify-write transaction. The caller owns the
// returned lock and must Close it.
func loadUpgradeState(dryRun bool) (*state.State, *state.LockedState, error) {
	if dryRun {
		st, err := state.Load()
		if err != nil {
			return nil, nil, err
		}
		return st, nil, nil
	}
	ls, err := state.LoadLocked()
	if err != nil {
		return nil, nil, err
	}
	return ls.State(), ls, nil
}

// buildUpgradeExecutor wires the executor for reinstall Install calls:
// default method order, host adapters, schema info, logging runner, and
// facts, plus the dry-run/arbitrary-code/quiet gates.
func buildUpgradeExecutor(s *config.Schema, clan string, facts *engine.Facts, schemaPath string, opts upgradeOptions, lg *slog.Logger) (*exec.Executor, *run.LoggingRunner, error) {
	schemaFile, err := os.Stat(schemaPath)
	if err != nil {
		lg.Error("stat schema", "error", err)
		return nil, nil, exitWithCode(1)
	}
	ex := exec.New()
	exec.WithDefaultMethodOrder(s.Defaults.MethodOrder)(ex)
	exec.WithAdapters(
		git.NewGitAdapter(),
		httpdownload.NewHTTPAdapter(),
		container.NewContainerAdapter(),
		exec.NewNativeAdapter(clan),
	)(ex)
	exec.WithSchemaInfo(schemaPath, schemaFile.ModTime())(ex)
	exec.WithLogger(lg)(ex)
	runner := run.NewLoggingRunner(run.OSExecRunner{}, lg)
	exec.WithRunner(runner)(ex)
	exec.WithFacts(facts)(ex)
	if opts.dryRun {
		exec.WithDryRun()(ex)
	}
	if opts.allowArbitrary {
		exec.WithAllowArbitraryCode()(ex)
	}
	if opts.quiet {
		exec.WithQuiet()(ex)
	}
	return ex, runner, nil
}

// loadUpgradeLock loads the lockfile, warning on corruption. A missing
// lockfile is fatal: there is nothing to upgrade toward.
func loadUpgradeLock(schemaPath string, lg *slog.Logger) (*lock.Lock, error) {
	lockPath := lock.DefaultPath(schemaPath)
	lk, err := lock.Load(lockPath)
	if err != nil && !os.IsNotExist(err) {
		lg.Warn("load lock", "error", err)
	}
	if lk == nil {
		fmt.Fprintln(os.Stderr, "No lockfile found. Run 'depengine update' first to resolve and pin versions.")
		return nil, exitWithCode(1)
	}
	return lk, nil
}

// collectOutdatedTools compares installed-tool state against lockfile pins.
// Same-kind candidates have independent lock identities, so each tool is
// resolved to its exact tracked candidate before looking up its pin — an
// arbitrary first match could upgrade from/to the wrong artifact. Unresolvable
// candidates with a pin for their kind are terminal discovery failures that
// participate in the same report. Output is sorted for determinism.
func collectOutdatedTools(st *state.State, s *config.Schema, lk *lock.Lock, only string, defaultOrder []string, nativeManager string) ([]upgradeOutdatedTool, []upgradeResult) {
	var (
		outdated          []upgradeOutdatedTool
		discoveryFailures []upgradeResult
	)
	for name, ts := range st.Tools {
		// Filter by --only.
		if only != "" && name != only {
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

		method, methodErr := findTrackedMethodCandidate(tool, ts, defaultOrder, nativeManager)
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

		outdated = append(outdated, upgradeOutdatedTool{
			name:       name,
			ts:         ts,
			pinnedVer:  pin.Latest,
			tool:       tool,
			method:     method,
			methodKind: methodKind,
		})
	}

	sort.Slice(outdated, func(i, j int) bool {
		return outdated[i].name < outdated[j].name
	})
	return outdated, discoveryFailures
}

// reportUpgradeUpToDate covers the nothing-to-do case.
func reportUpgradeUpToDate(jsonOut bool) error {
	if jsonOut {
		fmt.Println(`{"upgraded":0,"skipped":0,"failed":0,"would_upgrade":0,"results":[]}`)
	} else {
		fmt.Fprintln(os.Stderr, "All installed tools are up to date.")
	}
	return nil
}

// shouldPromptUpgrade gates the confirmation prompt: --force, --dry-run, and
// --json runs never prompt, and a non-interactive session cannot answer.
func shouldPromptUpgrade(force, dryRun, jsonOut, interactive bool) bool {
	return !force && !dryRun && !jsonOut && interactive
}

// confirmUpgradeProceed renders the risk-review list — aligned name column
// plus dimmed old→new pairs, so the decision-relevant data lines up — and
// reads the answer. It reports false when the operator aborts.
func confirmUpgradeProceed(outdated []upgradeOutdatedTool, c *cliStyle) bool {
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
	return confirmationAccepted(os.Stdin)
}

// upgradeSingleTool runs the Remove→Install sequencing for one outdated tool:
// adapter lookup, fail-closed preflight, then remove, reinstall, probe, and
// state update. Every outcome is a result value; the whole run is never
// aborted from here.
func upgradeSingleTool(ctx context.Context, ex *exec.Executor, runner *run.LoggingRunner, facts *engine.Facts, st *state.State, ot upgradeOutdatedTool, opts upgradeOptions, c *cliStyle) upgradeResult {
	res := upgradeResult{
		Tool:   ot.name,
		OldVer: ot.ts.Version,
		Method: ot.ts.Method,
	}
	fail := func(format string, args ...any) upgradeResult {
		res.Status = "failed"
		res.Error = fmt.Sprintf(format, args...)
		if !opts.quiet {
			c.fail("%s: %s", ot.name, res.Error)
		}
		return res
	}

	adapter := ex.LookupAdapter(ot.methodKind)
	if adapter == nil {
		return fail("no adapter for method %q", ot.methodKind)
	}

	// The legacy upgrade path calls Remove/Install directly. Fail closed on
	// semantics it cannot yet preserve instead of removing a working tool and
	// discovering the mismatch during reinstall.
	if err := preflightDirectUpgrade(ctx, runner, facts, ot.tool, ot.method, adapter, opts.allowArbitrary); err != nil {
		return fail("upgrade preflight failed: %v", err)
	}

	if opts.dryRun {
		res.Status = "would_upgrade"
		res.NewVer = ot.pinnedVer
		if !opts.quiet {
			c.arrow("%s: %s → %s (dry-run)", ot.name, ot.ts.Version, ot.pinnedVer)
		}
		return res
	}

	if !exec.CanRemove(adapter) {
		// Adapter can't remove — skip with a clear message.
		res.Status = "skipped"
		res.Error = fmt.Sprintf("adapter %q does not support removal — remove manually and reinstall", ot.methodKind)
		if !opts.quiet {
			c.skip("%s: %s", ot.name, res.Error)
		}
		return res
	}

	remover := adapter.(exec.Remover)
	tr := runner.WithContext(run.Context{Tool: ot.name, Method: ot.methodKind})
	if err := removeInstalledTool(ctx, remover, tr, ot); err != nil {
		return fail("remove failed: %v", err)
	}

	// Install the exact candidate resolved before the destructive
	// transition. Never fall back to another candidate of the same kind.
	newVer, err := reinstallUpgradeTool(ctx, adapter, tr, st, ot)
	if err != nil {
		return fail("reinstall failed: %v", err)
	}

	newTS := upgradedToolState(ot.ts, ot.methodKind, ot.tool, ot.method, newVer, ot.pinnedVer, time.Now().UTC())
	st.Tools[ot.name] = newTS

	res.Status = "upgraded"
	res.NewVer = newVer
	if res.NewVer == "" {
		res.NewVer = ot.pinnedVer
	}
	if !opts.quiet {
		c.ok("%s: %s → %s", ot.name, ot.ts.Version, res.NewVer)
	}
	return res
}

// removeInstalledTool removes the tracked installation, recovering method
// config from the schema when state config is empty.
func removeInstalledTool(ctx context.Context, remover exec.Remover, tr run.Runner, ot upgradeOutdatedTool) error {
	mc := &config.MethodCandidate{
		Kind:   ot.methodKind,
		Config: ot.ts.Config,
	}
	if ot.ts.Config == nil {
		mc.Config = findMethodConfig(ot.tool, ot.methodKind)
	}
	removeCtx, removeCancel := context.WithTimeout(ctx, 2*time.Minute)
	defer removeCancel()
	return remover.Remove(removeCtx, tr, ot.tool, mc)
}

// reinstallUpgradeTool installs the already-resolved candidate and probes the
// installed version. On reinstall failure the tool's shared-resource claims
// are released so state does not pretend a missing tool still holds refs;
// newly zero-ref resources stay on the host for explicit retry/cleanup.
func reinstallUpgradeTool(ctx context.Context, adapter exec.Adapter, tr run.Runner, st *state.State, ot upgradeOutdatedTool) (string, error) {
	installCtx, installCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer installCancel()
	if err := adapter.Install(installCtx, tr, ot.tool, ot.method); err != nil {
		if releaseErr := recordFailedUpgradeRemoval(st, ot.name); releaseErr != nil {
			log.Default.Error("release failed-upgrade resources", "tool", ot.name, "error", releaseErr)
		}
		return "", err
	}
	return probeVersion(ctx, adapter, tr, ot.tool, ot.method), nil
}

// runUpgradeLoop upgrades each outdated tool in order, seeding the report
// with the already-terminal candidate discovery failures.
func runUpgradeLoop(ctx context.Context, ex *exec.Executor, runner *run.LoggingRunner, facts *engine.Facts, st *state.State, outdated []upgradeOutdatedTool, discoveryFailures []upgradeResult, opts upgradeOptions, c *cliStyle) ([]upgradeResult, upgradeCounts) {
	results := append([]upgradeResult(nil), discoveryFailures...)
	counts := upgradeCounts{failed: len(discoveryFailures)}
	for _, res := range discoveryFailures {
		if !opts.quiet {
			c.fail("%s: %s", res.Tool, res.Error)
		}
	}

	for _, ot := range outdated {
		res := upgradeSingleTool(ctx, ex, runner, facts, st, ot, opts, c)
		results = append(results, res)
		switch res.Status {
		case "upgraded":
			counts.upgraded++
		case "skipped":
			counts.skipped++
		case "failed":
			counts.failed++
		case "would_upgrade":
			counts.wouldUpgrade++
		}
	}
	return results, counts
}

// writeUpgradeReport renders the JSON or footer summary and maps failures to
// the process exit code. The footer mirrors the ✓/✗/–/→ vocabulary of the
// per-tool lines: a count only gets a colored marker when non-zero, so a
// clean run is one quiet green line and a failure is impossible to miss.
func writeUpgradeReport(jsonOut, dryRun bool, counts upgradeCounts, results []upgradeResult) error {
	if jsonOut {
		out := map[string]any{
			"upgraded":      counts.upgraded,
			"skipped":       counts.skipped,
			"failed":        counts.failed,
			"would_upgrade": counts.wouldUpgrade,
			"results":       results,
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		c := newCLIStyle(os.Stderr)
		fmt.Fprintln(os.Stderr)
		var parts []string
		if dryRun {
			if counts.wouldUpgrade > 0 {
				parts = append(parts, c.cyan(fmt.Sprintf("%d would upgrade", counts.wouldUpgrade)))
			}
		} else if counts.upgraded > 0 {
			parts = append(parts, c.green(fmt.Sprintf("%d upgraded", counts.upgraded)))
		}
		if counts.skipped > 0 {
			parts = append(parts, c.yellow(fmt.Sprintf("%d skipped", counts.skipped)))
		}
		if counts.failed > 0 {
			parts = append(parts, c.red(fmt.Sprintf("%d failed", counts.failed)))
		}
		if len(parts) == 0 {
			parts = append(parts, "nothing to do")
		}
		fmt.Fprintf(os.Stderr, "  %s\n", strings.Join(parts, "  ·  "))
	}

	if counts.failed > 0 {
		return exitWithCode(1)
	}
	return nil
}

// runUpgrade upgrades installed tools whose recorded version is outdated
// relative to the pinned version in depengine.lock. For each outdated tool,
// it calls adapter.Remove followed by adapter.Install, then updates state.
// Thin orchestrator over the phase helpers above: flag resolution, schema and
// lock loading, state snapshot, executor wiring, drift collection,
// confirmation, per-tool loop, state save, and reporting.
func runUpgrade(ctx context.Context, upgradeSchema, upgradeManifest *string, upgradeNoManifest, upgradeDryRun *bool, upgradeOnly *string, upgradeForce, upgradeJSON, upgradeQuiet, upgradeAllowArbitrary *bool) error {
	lg := log.Default
	opts := newUpgradeOptions(upgradeSchema, upgradeManifest, upgradeNoManifest, upgradeDryRun, upgradeOnly, upgradeForce, upgradeJSON, upgradeQuiet, upgradeAllowArbitrary)

	manifestPath, manifestAuto := resolveUpgradeManifestPath(opts.noManifest, opts.manifest)

	s, clan, facts, manifestCount, err := loadSchemaWithManifest(opts.schema, manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "error: %s not found\n", opts.schema)
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
	lk, err := loadUpgradeLock(opts.schema, lg)
	if err != nil {
		return err
	}

	st, ls, err := loadUpgradeState(opts.dryRun)
	if err != nil {
		lg.Error("load state", "error", err)
		return exitWithCode(3)
	}
	if ls != nil {
		defer ls.Close()
	}

	ex, runner, err := buildUpgradeExecutor(s, clan, facts, opts.schema, opts, lg)
	if err != nil {
		return err
	}

	// Apply lock pins to the schema so Install sees resolved versions.
	lock.Apply(s, lk)

	outdated, discoveryFailures := collectOutdatedTools(st, s, lk, opts.only, ex.DefaultMethodOrder(), ex.NativeManagerName())
	if len(outdated) == 0 && len(discoveryFailures) == 0 {
		return reportUpgradeUpToDate(opts.jsonOut)
	}

	c := newCLIStyle(os.Stderr)

	printKV(c, "depengine upgrade",
		[2]string{"schema", opts.schema},
		[2]string{"target", fmt.Sprintf("%s (%s) · %s", facts.DistroID, clan, facts.TargetArch)},
		[2]string{"outdated", fmt.Sprintf("%d", len(outdated))},
	)

	// Confirmation prompt (unless --force or --dry-run or --json).
	if shouldPromptUpgrade(opts.force, opts.dryRun, opts.jsonOut, isInteractive()) {
		if !confirmUpgradeProceed(outdated, c) {
			fmt.Fprintln(os.Stderr, "Aborted.")
			return nil
		}
	}

	// Upgrade each outdated tool: preflight, then Remove and Install. Candidate
	// discovery failures are already terminal and participate in the same report.
	results, counts := runUpgradeLoop(ctx, ex, runner, facts, st, outdated, discoveryFailures, opts, c)

	// Save state (unless dry-run).
	if !opts.dryRun {
		if err := ls.Save(); err != nil {
			lg.Error("state save failed", "error", err)
			return exitWithCode(3)
		}
	}

	return writeUpgradeReport(opts.jsonOut, opts.dryRun, counts, results)
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
