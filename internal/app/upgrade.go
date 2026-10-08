package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
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
		Short:   ifPT("Atualizar ferramentas para os alvos fixados no depengine.lock", "Upgrade installed tools to the targets pinned in depengine.lock"),
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

// upgradeOutdatedTool is a state-tracked tool whose observed identity differs
// from its locked target, with the exact schema candidate resolved before any
// destructive transition.
type upgradeOutdatedTool struct {
	name       string
	ts         state.ToolState
	pinnedVer  string
	resolved   *plan.ResolvedInstallPlan
	schema     *config.Schema
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
// unlocked read observes a complete old or new file). Real upgrades return the
// exclusive lock to the caller for discovery; runUpgrade releases it before
// per-candidate execution, which locks each mutation transaction. The caller
// owns the returned lock and must Close it.
func loadUpgradeState(ctx context.Context, dryRun bool) (*state.State, *state.LockedState, error) {
	if dryRun {
		st, err := state.Load()
		if err != nil {
			return nil, nil, err
		}
		return st, nil, nil
	}
	ls, err := state.LoadLockedContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	return ls.State(), ls, nil
}

// buildUpgradeExecutor wires the executor with the upgrade schema, host adapters,
// resolved lock, runner, and execution gates.
func buildUpgradeExecutor(s *config.Schema, clan string, facts *platform.Facts, schemaPath string, opts upgradeOptions, lg *slog.Logger, lockDocuments ...*plan.LockDocument) (*exec.Executor, error) {
	schemaFile, err := os.Stat(schemaPath)
	if err != nil {
		lg.Error("stat schema", "error", err)
		return nil, exitWithCode(1)
	}
	ex := exec.New()
	exec.WithDefaultMethodOrder(s.Defaults.MethodOrder)(ex)
	// Keep schema-specific AUR configuration local to this executor. All other
	// adapters are inherited from the registry snapshot created by exec.New.
	if helper := s.Defaults.AurHelper; helper != "" {
		exec.WithAdapters(exec.NewNativeAdapter(clan), ecosystem.NewAURAdapter(helper))(ex)
	} else {
		exec.WithAdapters(exec.NewNativeAdapter(clan))(ex)
	}
	exec.WithSchemaInfo(schemaPath, schemaFile.ModTime())(ex)
	exec.WithLogger(lg)(ex)
	runner := run.NewLoggingRunner(run.OSExecRunner{}, lg)
	exec.WithRunner(runner)(ex)
	exec.WithFacts(facts)(ex)
	if len(lockDocuments) > 0 && lockDocuments[0] != nil {
		exec.WithLockDocument(*lockDocuments[0])(ex)
	}
	if opts.dryRun {
		exec.WithDryRun()(ex)
	}
	if opts.allowArbitrary {
		exec.WithAllowArbitraryCode()(ex)
	}
	if opts.quiet {
		exec.WithQuiet()(ex)
	}
	return ex, nil
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

// collectOutdatedTools compares host reconciliation with the locked target for
// v2 and recorded versions with legacy pins for v1. Each state entry must map
// to its exact selected candidate; unresolved locked targets are terminal
// discovery failures. Output is sorted for determinism.
func collectOutdatedTools(ctx context.Context, st *state.State, s *config.Schema, lk *lock.Lock, only string, ex *exec.Executor, clan string) ([]upgradeOutdatedTool, []upgradeResult) {
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

		method, methodErr := findTrackedMethodCandidate(tool, ts, ex.SelectedMethods(tool, clan))
		if methodErr != nil {
			if hasUpgradeLockTarget(lk, name, methodKind) {
				discoveryFailures = append(discoveryFailures, upgradeResult{
					Tool: name, Status: "failed", OldVer: ts.Version, Method: ts.Method,
					Error: fmt.Sprintf("cannot resolve tracked candidate: %v", methodErr),
				})
			}
			continue
		}
		var resolved *plan.ResolvedInstallPlan
		var err error
		version := ""
		if lk.Version == lock.CurrentVersion {
			var verification plan.VerificationResult
			resolved, verification, err = ex.ResolveAndVerifyCandidate(ctx, tool, method, clan)
			if err == nil {
				var decision plan.ReconciliationDecision
				decision, err = plan.TransitionForVerification(verification)
				if err == nil && decision.Transition == plan.TransitionInstall {
					err = fmt.Errorf("locked target is absent; run install before upgrade")
				}
				if err == nil && decision.Transition != plan.TransitionUpgrade {
					continue
				}
			}
			if err != nil {
				discoveryFailures = append(discoveryFailures, upgradeResult{
					Tool: name, Status: "failed", OldVer: ts.Version, Method: ts.Method,
					Error: fmt.Sprintf("cannot resolve locked target: %v", err),
				})
				continue
			}
			if resolved != nil {
				version = resolved.Identity.Version
			}
		} else {
			if pin, ok := legacyV1PinForCandidate(lk, name, tool, method); ok {
				version = legacyV1PinnedVersion(pin)
				if version != "" {
					resolved, _, err = ex.ResolveAndVerifyCandidateAtVersion(ctx, tool, method, version, clan)
					if err != nil {
						discoveryFailures = append(discoveryFailures, upgradeResult{Tool: name, Status: "failed", OldVer: ts.Version, Method: ts.Method, Error: fmt.Sprintf("cannot resolve legacy pinned target: %v", err)})
						continue
					}
				}
			}
			if version == "" || ts.Version == "" || !state.VersionOutdated(ts.Version, version) {
				continue
			}
		}

		outdated = append(outdated, upgradeOutdatedTool{
			name:       name,
			ts:         ts,
			pinnedVer:  version,
			resolved:   resolved,
			schema:     s,
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

// upgradeSingleTool delegates one previously resolved candidate to the executor.
func upgradeSingleTool(ctx context.Context, ex *exec.Executor, clan string, ot upgradeOutdatedTool, opts upgradeOptions, c *cliStyle) upgradeResult {
	res := upgradeResult{Tool: ot.name, Status: "failed", OldVer: ot.ts.Version, Method: ot.ts.Method}
	if ot.resolved == nil {
		res.Status = "failed"
		res.Error = "upgrade target was not resolved during discovery"
		return res
	}
	result, err := ex.ExecuteResolvedUpgradeCandidate(ctx, ot.schema, clan, ot.tool, ot.method, ot.resolved, ot.ts)
	if err != nil {
		res.Error = err.Error()
		if !opts.quiet {
			c.fail("%s: %s", ot.name, res.Error)
		}
		return res
	}
	res.NewVer = ot.pinnedVer
	switch result.Status {
	case exec.StatusInstalled:
		res.Status = "upgraded"
		if !opts.quiet {
			c.ok("%s: %s → %s", ot.name, ot.ts.Version, res.NewVer)
		}
	case exec.StatusAlready:
		res.Status = "already-current"
	case exec.StatusWouldInstall:
		res.Status = "would_upgrade"
		if !opts.quiet {
			c.arrow("%s: %s → %s (dry-run)", ot.name, ot.ts.Version, res.NewVer)
		}
	case exec.StatusSkippedUnavailable, exec.StatusSkippedWhen:
		res.Status = "skipped"
		res.Error = result.Error
		if !opts.quiet {
			c.skip("%s: %s", ot.name, res.Error)
		}
	default:
		res.Status = "failed"
		res.Error = result.Error
		if res.Error == "" {
			res.Error = "executor did not complete the upgrade"
		}
		if !opts.quiet {
			c.fail("%s: %s", ot.name, res.Error)
		}
	}
	return res
}

// runUpgradeLoop upgrades each outdated tool in order, seeding the report
// with the already-terminal candidate discovery failures.
func runUpgradeLoop(ctx context.Context, ex *exec.Executor, clan string, outdated []upgradeOutdatedTool, discoveryFailures []upgradeResult, opts upgradeOptions, c *cliStyle) ([]upgradeResult, upgradeCounts) {
	results := append([]upgradeResult(nil), discoveryFailures...)
	counts := upgradeCounts{failed: len(discoveryFailures)}
	for _, res := range discoveryFailures {
		if !opts.quiet {
			c.fail("%s: %s", res.Tool, res.Error)
		}
	}

	for _, ot := range outdated {
		res := upgradeSingleTool(ctx, ex, clan, ot, opts, c)
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

// runUpgrade reconciles installed tools against depengine.lock, then executes
// exact targets for drifted candidates. It orchestrates schema/lock loading,
// executor wiring, discovery, confirmation, execution, and reporting.
func runUpgrade(ctx context.Context, upgradeSchema, upgradeManifest *string, upgradeNoManifest, upgradeDryRun *bool, upgradeOnly *string, upgradeForce, upgradeJSON, upgradeQuiet, upgradeAllowArbitrary *bool) error {
	lg := log.Default
	opts := newUpgradeOptions(upgradeSchema, upgradeManifest, upgradeNoManifest, upgradeDryRun, upgradeOnly, upgradeForce, upgradeJSON, upgradeQuiet, upgradeAllowArbitrary)

	manifestPath, manifestAuto := resolveUpgradeManifestPath(opts.noManifest, opts.manifest)

	project, err := loadProject(opts.schema, projectLoadOptions{ManifestPath: manifestPath, ManifestAuto: manifestAuto, Provenance: true})
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "error: %s not found\n", opts.schema)
			fmt.Fprintf(os.Stderr, "Run 'depengine init' to create one, or point --schema to an existing file.\n")
			return exitWithCode(1)
		}
		lg.Error("load schema", "error", err)
		return exitWithCode(exitCodeForError(err))
	}
	opts.schema = project.SchemaPath
	s, clan, facts, manifestCount := project.Schema, project.Clan, project.Facts, project.ManifestCount
	if manifestAuto && manifestCount > 0 {
		fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, manifestCount)
	}

	lk, err := loadUpgradeLock(opts.schema, lg)
	if err != nil {
		return err
	}

	st, ls, err := loadUpgradeState(ctx, opts.dryRun)
	if err != nil {
		lg.Error("load state", "error", err)
		return exitWithCode(3)
	}
	if ls != nil {
		defer func() {
			if ls != nil {
				_ = ls.Close()
			}
		}()
	}

	var lockDocument *plan.LockDocument
	if lk.Version == lock.CurrentVersion {
		document, err := lk.ProjectionDocument()
		if err != nil {
			return fmt.Errorf("load universal lock projection: %w", err)
		}
		lockDocument = &document
	}
	ex, err := buildUpgradeExecutor(s, clan, facts, opts.schema, opts, lg, lockDocument)
	if err != nil {
		return err
	}

	// V1 pins are projected for legacy compatibility; v2 resolution uses the
	// LockDocument passed to the executor and never reads ToolPin payloads.
	if lk.Version == 1 {
		lock.ApplyLegacyV1(s, lk)
	}

	outdated, discoveryFailures := collectOutdatedTools(ctx, st, s, lk, opts.only, ex, clan)
	if len(outdated) == 0 && len(discoveryFailures) == 0 {
		return reportUpgradeUpToDate(opts.jsonOut)
	}

	c := newCLIStyle(os.Stderr)

	printKV(c, "depengine upgrade",
		[2]string{"schema", opts.schema},
		[2]string{"target", fmt.Sprintf("%s (%s) · %s", facts.DistroID, clan, facts.TargetArch)},
		[2]string{"outdated", fmt.Sprintf("%d", len(outdated))},
	)

	if shouldPromptUpgrade(opts.force, opts.dryRun, opts.jsonOut, isInteractive()) {
		if !confirmUpgradeProceed(outdated, c) {
			fmt.Fprintln(os.Stderr, "Aborted.")
			return nil
		}
	}

	// Delegate each exact resolved candidate to the executor. Discovery failures
	// are already terminal and participate in the same report.
	if ls != nil {
		if err := ls.Close(); err != nil {
			return fmt.Errorf("release upgrade state lock: %w", err)
		}
		ls = nil
	}
	results, counts := runUpgradeLoop(ctx, ex, clan, outdated, discoveryFailures, opts, c)

	return writeUpgradeReport(opts.jsonOut, opts.dryRun, counts, results)
}

func hasPinnedVersionForKind(l *lock.Lock, toolName, kind string) bool {
	if l == nil || kind == "" {
		return false
	}
	prefix := toolName + "/" + kind + "/"
	for key, pin := range l.Tools {
		if strings.HasPrefix(key, prefix) && legacyV1PinnedVersion(pin) != "" {
			return true
		}
	}
	return false
}

// hasUpgradeLockTarget checks the universal projection for v2 and the method-specific pin for v1.
func hasUpgradeLockTarget(lk *lock.Lock, toolName, kind string) bool {
	if lk == nil {
		return false
	}
	if lk.Version == lock.CurrentVersion {
		document, err := lk.ProjectionDocument()
		if err != nil {
			return true // malformed v2 identity must fail closed in discovery.
		}
		_, ok := document.EntryForTool(toolName)
		return ok
	}
	return hasPinnedVersionForKind(lk, toolName, kind)
}

// findTrackedMethodCandidate resolves the exact currently-selected schema
// candidate represented by durable ToolState. Candidate identity comes from
// findStateMethodCandidate; this additional gate ensures the current method
// policy still selects it before a destructive upgrade.
func findTrackedMethodCandidate(tool *config.Tool, ts state.ToolState, selected []*config.MethodCandidate) (*config.MethodCandidate, error) {
	candidate, err := findStateMethodCandidate(tool, ts)
	if err != nil {
		return nil, err
	}
	for _, selectedCandidate := range selected {
		if selectedCandidate == candidate {
			return candidate, nil
		}
	}
	display := candidate.Kind
	if candidate.Label != "" {
		display = candidate.Label
	}
	return nil, fmt.Errorf("tracked candidate %q (kind %q) is not selected by the current schema", display, candidate.Kind)
}
