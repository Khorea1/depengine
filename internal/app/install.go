package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
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
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newInstallCmd builds `depengine install`. All flags are declared here,
// once — Cobra's --help, error messages, and shell completion are derived
// straight from this declaration.
func newInstallCmd() *cobra.Command {
	installSchema := new(string)
	installManifest := new(string)
	installNoManifest := new(bool)
	installDryRun := new(bool)
	installVerbose := new(bool)
	installJSON := new(bool)
	installOnly := new(string)
	installSkip := new(string)
	installProfile := new(string)
	installFrozen := new(bool)
	installDiagnose := new(bool)
	installLogLevel := new(string)
	installSortBy := new(string)
	installJobs := new(int)
	installAllowArbitrary := new(bool)
	installQuiet := new(bool)

	cmd := &cobra.Command{
		Use:     "install",
		Short:   ifPT("Instalar ferramentas do schema.toml", "Install tools from schema.toml"),
		GroupID: groupManage,
		Args:    cobra.NoArgs,
		RunE: func(installCmd *cobra.Command, args []string) error {
			return runInstall(installCmd, installSchema, installManifest, installNoManifest, installDryRun, installVerbose, installJSON, installOnly, installSkip, installProfile, installFrozen, installDiagnose, installLogLevel, installSortBy, installJobs, installAllowArbitrary, installQuiet)
		},
	}
	f := cmd.Flags()
	f.StringVar(installSchema, "schema", defaultSchemaPath(), "path to schema.toml")
	f.StringVar(installManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(installNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	f.BoolVar(installDryRun, "dry-run", false, "show what would be installed")
	f.BoolVar(installVerbose, "verbose", false, "detailed output")
	f.BoolVar(installJSON, "json", false, "JSON output")
	f.StringVar(installOnly, "only", "", "only install specific tool")
	f.StringVar(installSkip, "skip", "", "skip specific tools (comma-separated)")
	f.StringVar(installProfile, "profile", "", "only install tools with matching tag (e.g. minimal,desktop,server)")
	f.BoolVar(installFrozen, "frozen-lockfile", false, "fail if depengine.lock is missing, detectably stale, or lacks a supported required pin")
	f.BoolVar(installDiagnose, "diagnose", false, "diagnostic mode: DEBUG + dry-run + verbose")
	f.StringVar(installLogLevel, "log-level", "", "log level: debug, info, warn, error")
	f.StringVar(installSortBy, "sort-by", "", "sort output by: name, status, method")
	f.IntVar(installJobs, "jobs", 1, "max concurrent installations (default 1 = sequential)")
	f.BoolVar(installAllowArbitrary, "allow-arbitrary-code", false, "permit hooks, build scripts, and other arbitrary code execution")
	f.BoolVar(installAllowArbitrary, "yolo", false, "alias for --allow-arbitrary-code")
	if err := f.MarkHidden("yolo"); err != nil {
		panic(err)
	}
	f.BoolVar(installQuiet, "quiet", false, "suppress per-tool status lines; show only final summary")
	return cmd
}

// installPlan is the resolved, value-copied view of the install flags.
// collectInstallPlan applies --diagnose defaults (mutating only flags the
// user didn't explicitly set), picks the logger, validates --sort-by, and
// resolves the manifest path. Everything downstream reads this struct.
type installPlan struct {
	schema       string
	manifestFlag string
	manifestPath string
	manifestAuto bool
	noManifest   bool
	dryRun       bool
	verbose      bool
	json         bool
	only         string
	skip         string
	profile      string
	frozen       bool
	diagnose     bool
	logLevel     string
	sortBy       string
	jobs         int
	allowCode    bool
	quiet        bool
}

// collectInstallPlan resolves flags into an installPlan plus the logger.
// Returns an ExitError(2) when --sort-by is invalid.
func collectInstallPlan(cmd *cobra.Command, installSchema, installManifest *string, installNoManifest, installDryRun, installVerbose, installJSON *bool, installOnly, installSkip, installProfile *string, installFrozen, installDiagnose *bool, installLogLevel, installSortBy *string, installJobs *int, installAllowArbitrary, installQuiet *bool) (installPlan, *slog.Logger) {
	lg := log.Default
	p := installPlan{
		schema:       *installSchema,
		manifestFlag: *installManifest,
		noManifest:   *installNoManifest,
		dryRun:       *installDryRun,
		verbose:      *installVerbose,
		json:         *installJSON,
		only:         *installOnly,
		skip:         *installSkip,
		profile:      *installProfile,
		frozen:       *installFrozen,
		diagnose:     *installDiagnose,
		logLevel:     *installLogLevel,
		sortBy:       *installSortBy,
		jobs:         *installJobs,
		allowCode:    *installAllowArbitrary,
		quiet:        *installQuiet,
	}

	if p.diagnose {
		// Only override flags the user didn't explicitly set.
		saw := make(map[string]bool)
		cmd.Flags().Visit(func(f *pflag.Flag) {
			saw[f.Name] = true
		})
		if !saw["dry-run"] {
			p.dryRun = true
			*installDryRun = true
		}
		if !saw["verbose"] {
			p.verbose = true
			*installVerbose = true
		}
		if !saw["log-level"] {
			lg = log.New(os.Stderr, slog.LevelDebug)
		}
	}
	if p.logLevel != "" {
		lg = log.New(os.Stderr, log.LevelFromString(p.logLevel))
	}

	p.manifestPath, p.manifestAuto = resolveInstallManifestPath(p.noManifest, p.manifestFlag, config.DefaultManifestPath())
	return p, lg
}

// resolveInstallManifestPath maps --no-manifest/--manifest plus the
// auto-detected default into the effective manifest path. Pure: the default
// is passed in so tests don't touch XDG env.
func resolveInstallManifestPath(noManifest bool, flag, def string) (string, bool) {
	if noManifest || flag != "" {
		return flag, false
	}
	if def != "" {
		return def, true
	}
	return "", false
}

// validateInstallSortBy rejects unknown --sort-by values with ExitError(2).
func validateInstallSortBy(sortBy string, lg *slog.Logger) error {
	if sortBy == "" {
		return nil
	}
	if _, ok := exec.ParseSortField(sortBy); !ok {
		lg.Error("invalid --sort-by value", "value", sortBy, "valid", "name, status, method")
		return exitWithCode(2)
	}
	return nil
}

// printInstallHeader prints the aligned pre-run block answering "what
// schema, what target, how many tools, is this a dry run".
func printInstallHeader(cs *cliStyle, p installPlan, s *config.Schema, clan string, facts *engine.Facts, manifestCount int) {
	title := "depengine install"
	if p.dryRun {
		title = "depengine install — dry run (planning only)"
	}
	pairs := [][2]string{
		{"schema", p.schema},
	}
	if p.manifestAuto && manifestCount > 0 {
		pairs = append(pairs, [2]string{"manifest", fmt.Sprintf("%s (%s)", p.manifestPath, plural(manifestCount, "tool")+" merged")})
	}
	pairs = append(pairs,
		[2]string{"target", fmt.Sprintf("%s (%s) · %s", facts.DistroID, clan, facts.TargetArch)},
		[2]string{"tools", fmt.Sprintf("%d", len(s.Tools))},
	)
	printKV(cs, title, pairs...)
}

// newInstallExecutor wires the executor: adapters, schema info, logger,
// runner, facts, and the install plan's behavior options.
func newInstallExecutor(p installPlan, s *config.Schema, clan string, facts *engine.Facts, schemaModTime time.Time, lg *slog.Logger) *exec.Executor {
	ex := exec.New()
	exec.WithDefaultMethodOrder(s.Defaults.MethodOrder)(ex)
	exec.WithAdapters(
		git.NewGitAdapter(),
		httpdownload.NewHTTPAdapter(),
		container.NewContainerAdapter(),
		exec.NewNativeAdapter(clan),
	)(ex)
	exec.WithSchemaInfo(p.schema, schemaModTime)(ex)
	exec.WithLogger(lg)(ex)
	exec.WithRunner(run.NewLoggingRunner(run.OSExecRunner{}, lg))(ex)

	exec.WithFacts(facts)(ex)
	if p.dryRun {
		exec.WithDryRun()(ex)
	}
	if p.sortBy != "" {
		exec.WithSortBy(exec.SortField(p.sortBy))(ex)
	}
	if p.jobs > 1 {
		exec.WithMaxJobs(p.jobs)(ex)
	}
	if p.allowCode {
		exec.WithAllowArbitraryCode()(ex)
	}
	if p.quiet {
		exec.WithQuiet()(ex)
	}
	if p.diagnose {
		exec.WithDiagnose()(ex)
	}
	return ex
}

// resolveInstallLock loads the lockfile and auto-resolves {latest} pins when
// no lockfile exists (npm/pip style: first install needs no explicit update).
func resolveInstallLock(ctx context.Context, p installPlan, s *config.Schema, lg *slog.Logger) (*lock.Lock, error) {
	lk, err := loadLockfile(p.schema, s, p.frozen, lg)
	if err != nil {
		return nil, err
	}
	if lk == nil && !p.frozen {
		if hasLatestPlaceholders(s) {
			lg.Info("no lockfile found — resolving latest versions")
			newLock, err := lock.ResolveAll(ctx, s, run.OSExecRunner{})
			if err != nil {
				lg.Warn("could not auto-resolve latest", "error", err, "hint", "run 'depengine update' manually")
			} else if newLock != nil {
				lock.Apply(s, newLock)
				lk = newLock
			}
		}
	}
	return lk, nil
}

// installOutputMode selects the report rendering path. Pure.
func installOutputMode(json, quiet, verbose bool) string {
	if json {
		return "json"
	}
	if quiet || verbose {
		return "detail"
	}
	return "summary"
}

// renderInstallReport prints the execution report: JSON, the detail table
// (--quiet needs it as the only detail surface, --verbose asks for the
// recap), or the one-line summary. Plain and dry-run installs skip the
// table — the live ✓/✗/→ lines already told the story.
func renderInstallReport(report *exec.ExecReport, p installPlan, cs *cliStyle) {
	switch installOutputMode(p.json, p.quiet, p.verbose) {
	case "json":
		fmt.Println(report.JSON())
	case "detail":
		fmt.Fprint(os.Stderr, report.Detail())
	default:
		fmt.Fprintln(os.Stderr, report.Summary())
	}

	if p.dryRun && !p.json {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, cs.cyan("Dry run — read-only resolution/checks may run; mutation steps were not executed. Remove --dry-run to install."))
	}
}

// shouldShareInstallHint is true after a successful real install. Pure.
func shouldShareInstallHint(report *exec.ExecReport, dryRun bool) bool {
	return report.Failed == 0 && report.Success > 0 && !dryRun
}

// installExitForReport maps a finished report to the process exit. Pure.
func installExitForReport(report *exec.ExecReport) error {
	if report.Failed > 0 {
		return exitWithCode(1)
	}
	return nil
}

// finishInstallRun persists post-run state (lockfile + version sync),
// prints the share hint, and maps the report to the exit error.
func finishInstallRun(ctx context.Context, report *exec.ExecReport, p installPlan, s *config.Schema, lockPath string, lk *lock.Lock, lg *slog.Logger, cs *cliStyle) error {
	if !p.dryRun {
		saveLockfile(ctx, s, lockPath, lk, lg, p.diagnose)
		// Reconcile recorded versions with the lock: backfill versions the
		// adapter could not determine (e.g. {latest} pins baked into URLs)
		// and surface installed-vs-pinned mismatches instead of a silent
		// "already installed".
		syncInstalledVersions(ctx, s, lockPath, report, lg)
	}

	// After successful install, guide the user to share.
	if shouldShareInstallHint(report, p.dryRun) {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, cs.dim("Share schema.toml in git so others can reproduce your tools:"))
		fmt.Fprintln(os.Stderr, cs.dim("  git add schema.toml depengine.lock && git commit"))
	}

	return installExitForReport(report)
}

// runInstall installs tools from schema.toml. Thin orchestrator over the
// phase helpers above — flag resolution, header, executor build, lock,
// execute, render, finish.
func runInstall(cmd *cobra.Command, installSchema, installManifest *string, installNoManifest, installDryRun, installVerbose, installJSON *bool, installOnly, installSkip, installProfile *string, installFrozen, installDiagnose *bool, installLogLevel, installSortBy *string, installJobs *int, installAllowArbitrary, installQuiet *bool) error {
	p, lg := collectInstallPlan(cmd, installSchema, installManifest, installNoManifest, installDryRun, installVerbose, installJSON, installOnly, installSkip, installProfile, installFrozen, installDiagnose, installLogLevel, installSortBy, installJobs, installAllowArbitrary, installQuiet)

	ctx := cmd.Context()

	if err := validateInstallSortBy(p.sortBy, lg); err != nil {
		return err
	}

	s, clan, facts, manifestCount, err := loadSchemaWithManifest(p.schema, p.manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "error: %s not found\n", p.schema)
			fmt.Fprintf(os.Stderr, "Run 'depengine init' to create one, or point --schema to an existing file.\n")
			return exitWithCode(1)
		}
		lg.Error("load schema", "error", err)
		return exitWithCode(exitCodeForError(err))
	}
	if helper := s.Defaults.AurHelper; helper != "" {
		ecosystem.ReconfigureAUR(helper)
	}

	if p.verbose {
		fmt.Fprintln(os.Stderr, "depengine: --verbose is deprecated; output is now verbose by default. Use --quiet for the old summary-only behavior.")
	}

	// One aligned block instead of several scattered Fprintf calls — a
	// single glance answers "what schema, what target, how many tools,
	// is this a dry run" before any per-tool output starts scrolling by.
	cs := newCLIStyle(os.Stderr)
	printInstallHeader(cs, p, s, clan, facts, manifestCount)

	schemaFile, err := os.Stat(p.schema)
	if err != nil {
		lg.Error("stat schema", "error", err)
		return exitWithCode(exitCodeForError(err))
	}

	// Lock semantics follow the effective install closure. A partial lock
	// produced for a profile/--only selection must not be rejected because an
	// intentionally omitted tool lacks pins, while dependencies pulled into the
	// closure remain validated.
	s.Tools = filterTools(s.Tools, p.only, p.skip, p.profile)
	ex := newInstallExecutor(p, s, clan, facts, schemaFile.ModTime(), lg)

	lockPath := lock.DefaultPath(p.schema)
	lk, err := resolveInstallLock(ctx, p, s, lg)
	if err != nil {
		return err
	}

	if !p.dryRun {
		if _, err := state.SaveSnapshot(); err != nil {
			lg.Warn("could not save pre-install snapshot", "error", err)
		}
	}

	if p.diagnose {
		lg.Debug("facts", "facts", facts)
		lg.Debug("schema", "tools", len(s.Tools))
	}

	report, err := ex.Execute(ctx, s, clan)
	if err != nil {
		lg.Error("execute failed", "error", err)
		return exitWithCode(2)
	}

	renderInstallReport(report, p, cs)

	return finishInstallRun(ctx, report, p, s, lockPath, lk, lg, cs)
}

// syncInstalledVersions reconciles recorded versions with lock pins after an
// install run. Two jobs:
//  1. Tools that were installed this run but whose adapter could not
//     determine a version (e.g. a {latest} pin baked into the download URL)
//     get the pinned version recorded, so status/sbom never report 0.0.0
//     when the pin is knowable.
//  2. Already-installed tools whose recorded version differs from the
//     current pin get a visible mismatch warning instead of a silent
//     "already installed".
func syncInstalledVersions(ctx context.Context, schema *config.Schema, lockPath string, report *exec.ExecReport, lg *slog.Logger) {
	lk, err := lock.Load(lockPath)
	if err != nil {
		lg.Warn("load lock for version sync", "error", err)
		return
	}
	if lk == nil || len(lk.Tools) == 0 {
		return
	}

	ls, err := state.LoadLocked()
	if err != nil {
		lg.Warn("state lock for version sync", "error", err)
		return
	}
	defer ls.Close()
	st := ls.State()

	installed := make(map[string]bool, len(report.Tools))
	for _, tr := range report.Tools {
		if tr.Status == exec.StatusInstalled || tr.Status == exec.StatusAlready {
			installed[tr.Tool] = true
		}
	}

	changed := false
	// Backfill: tools installed this run with no recorded version get the pin.
	for name, ts := range st.Tools {
		if ts.Version != "" || !installed[name] {
			continue
		}
		tool := schema.Tools[name]
		if pin, ok := lockPinForToolState(lk, name, tool, ts); ok && pin.Latest != "" {
			ts.Version = pin.Latest
			st.Tools[name] = ts
			changed = true
		}
	}

	// Mismatch warnings: already-installed tools whose known version differs
	// from the pin that would now apply.
	for _, tr := range report.Tools {
		if tr.Status != exec.StatusAlready {
			continue
		}
		ts, ok := st.Tools[tr.Tool]
		if !ok || ts.Version == "" {
			continue
		}
		pin, ok := lockPinForToolState(lk, tr.Tool, schema.Tools[tr.Tool], ts)
		if !ok || pin.Latest == "" {
			continue
		}
		if state.VersionOutdated(ts.Version, pin.Latest) {
			s := newCLIStyle(os.Stderr)
			s.warn("%s: installed version %s differs from pinned %s (run 'depengine upgrade')",
				tr.Tool, ts.Version, pin.Latest)
		}
	}

	if changed {
		if err := ls.Save(); err != nil {
			lg.Warn("state save failed (version sync)", "error", err)
		}
	}
}
