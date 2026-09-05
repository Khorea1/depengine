package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/ecosystem"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/git"
	"github.com/Khorea1/depengine/pkg/httpdownload"
	"github.com/Khorea1/depengine/pkg/lock"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/state"
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
			runInstall(installCmd, installSchema, installManifest, installNoManifest, installDryRun, installVerbose, installJSON, installOnly, installSkip, installProfile, installFrozen, installDiagnose, installLogLevel, installSortBy, installJobs, installAllowArbitrary, installQuiet)
			return nil
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
	f.BoolVar(installFrozen, "frozen-lockfile", false, "fail if depengine.lock does not exist or needs update")
	f.BoolVar(installDiagnose, "diagnose", false, "diagnostic mode: DEBUG + dry-run + verbose")
	f.StringVar(installLogLevel, "log-level", "", "log level: debug, info, warn, error")
	f.StringVar(installSortBy, "sort-by", "", "sort output by: name, status, method")
	f.IntVar(installJobs, "jobs", 1, "max concurrent installations (default 1 = sequential)")
	f.BoolVar(installAllowArbitrary, "allow-arbitrary-code", false, "suppress security warnings for build scripts / arbitrary code")
	f.BoolVar(installQuiet, "quiet", false, "suppress per-tool status lines; show only final summary")
	return cmd
}

// runInstall installs tools from schema.toml. Body unchanged from the
// pre-Cobra version — only the flag declarations above it moved.
func runInstall(cmd *cobra.Command, installSchema, installManifest *string, installNoManifest, installDryRun, installVerbose, installJSON *bool, installOnly, installSkip, installProfile *string, installFrozen, installDiagnose *bool, installLogLevel, installSortBy *string, installJobs *int, installAllowArbitrary, installQuiet *bool) {
	lg := log.Default

	if *installDiagnose {
		// Only override flags the user didn't explicitly set.
		saw := make(map[string]bool)
		cmd.Flags().Visit(func(f *pflag.Flag) {
			saw[f.Name] = true
		})
		if !saw["dry-run"] {
			*installDryRun = true
		}
		if !saw["verbose"] {
			*installVerbose = true
		}
		if !saw["log-level"] {
			lg = log.New(os.Stderr, slog.LevelDebug)
		}
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

	noManifest := *installNoManifest
	manifestPath := *installManifest
	manifestAuto := false
	if !noManifest && manifestPath == "" {
		manifestPath = config.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}

	s, clan, facts, manifestCount, err := loadSchemaWithManifest(*installSchema, manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "error: %s not found\n", *installSchema)
			fmt.Fprintf(os.Stderr, "Run 'depengine init' to create one, or point --schema to an existing file.\n")
			os.Exit(1)
		}
		lg.Error("load schema", "error", err)
		os.Exit(exitCodeForError(err))
	}
	if helper := s.Defaults.AurHelper; helper != "" {
		ecosystem.ReconfigureAUR(helper)
	}

	if *installVerbose {
		fmt.Fprintln(os.Stderr, "depengine: --verbose is deprecated; output is now verbose by default. Use --quiet for the old summary-only behavior.")
	}

	// One aligned block instead of several scattered Fprintf calls — a
	// single glance answers "what schema, what target, how many tools,
	// is this a dry run" before any per-tool output starts scrolling by.
	cs := newCLIStyle(os.Stderr)
	title := "depengine install"
	if *installDryRun {
		title = "depengine install — dry run (no changes will be made)"
	}
	pairs := [][2]string{
		{"schema", *installSchema},
	}
	if manifestAuto && manifestCount > 0 {
		pairs = append(pairs, [2]string{"manifest", fmt.Sprintf("%s (%s)", manifestPath, plural(manifestCount, "tool")+" merged")})
	}
	pairs = append(pairs,
		[2]string{"target", fmt.Sprintf("%s (%s) · %s", facts.DistroID, clan, facts.TargetArch)},
		[2]string{"tools", fmt.Sprintf("%d", len(s.Tools))},
	)
	printKV(cs, title, pairs...)

	schemaFile, err := os.Stat(*installSchema)
	if err != nil {
		lg.Error("stat schema", "error", err)
		os.Exit(exitCodeForError(err))
	}

	ex := exec.New()
	exec.WithDefaultMethodOrder(s.Defaults.MethodOrder)(ex)
	exec.WithAdapters(
		git.NewGitAdapter(),
		httpdownload.NewHTTPAdapter(),
		exec.NewNativeAdapter(clan),
	)(ex)
	exec.WithSchemaInfo(*installSchema, schemaFile.ModTime())(ex)
	exec.WithLogger(lg)(ex)
	exec.WithRunner(run.NewLoggingRunner(run.OSExecRunner{}, lg))(ex)

	exec.WithFacts(facts)(ex)
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
	if *installQuiet {
		exec.WithQuiet()(ex)
	}
	if *installDiagnose {
		exec.WithDiagnose()(ex)
	}

	lockPath := lock.DefaultPath(*installSchema)
	lk := loadLockfile(*installSchema, s, *installFrozen, lg)

	// Auto-resolve {latest} if no lockfile exists — makes first install work
	// like npm/pip: no explicit 'depengine update' needed.
	if lk == nil && !*installFrozen {
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
	s.Tools = filterTools(s.Tools, *installOnly, *installSkip, *installProfile)

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
	} else if *installQuiet || *installVerbose {
		// --quiet showed no live per-tool lines, so the table is the only
		// place detail (and failure reasons) surface. --verbose is an
		// explicit ask for the same recap in addition to what already
		// streamed. Plain --dry-run and plain install deliberately do NOT
		// hit this branch: the live ✓/✗/→ lines already told the whole
		// story, and reprinting them as a table would just be noise.
		fmt.Fprint(os.Stderr, report.Detail())
	} else {
		fmt.Fprintln(os.Stderr, report.Summary())
	}

	if *installDryRun && !*installJSON {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, cs.cyan("Dry run — no changes were made. Remove --dry-run to install."))
	}

	if !*installDryRun {
		saveLockfile(ctx, s, lockPath, lk, lg, *installDiagnose)
		// Reconcile recorded versions with the lock: backfill versions the
		// adapter could not determine (e.g. {latest} pins baked into URLs)
		// and surface installed-vs-pinned mismatches instead of a silent
		// "already installed".
		syncInstalledVersions(ctx, lockPath, report, lg)
	}

	// After successful install, guide the user to share.
	if report.Failed == 0 && report.Success > 0 && !*installDryRun {
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, cs.dim("Share schema.toml in git so others can reproduce your tools:"))
		fmt.Fprintln(os.Stderr, cs.dim("  git add schema.toml depengine.lock && git commit"))
	}

	if report.Failed > 0 {
		os.Exit(1)
	}
}

// lockPinFor looks up a tool's pin in a lockfile. It accepts both the
// canonical "<tool>/<kind>/<idx>" keys and the legacy "<tool>/<kind>" form
// (readers accept both; only writers emit canonical). When kind is empty,
// any pin for the tool is accepted. Returns the first pin with a non-empty
// Latest.
func lockPinFor(l *lock.Lock, tool, kind string) (lock.ToolPin, bool) {
	if l == nil {
		return lock.ToolPin{}, false
	}
	if kind != "" {
		exact := tool + "/" + kind
		// Legacy key without the idx segment.
		if pin, ok := l.Tools[exact]; ok && pin.Latest != "" {
			return pin, true
		}
		// Canonical "<tool>/<kind>/<idx>" keys.
		prefix := exact + "/"
		for k, pin := range l.Tools {
			if strings.HasPrefix(k, prefix) && pin.Latest != "" {
				return pin, true
			}
		}
		return lock.ToolPin{}, false
	}
	prefix := tool + "/"
	for k, pin := range l.Tools {
		if strings.HasPrefix(k, prefix) && pin.Latest != "" {
			return pin, true
		}
	}
	return lock.ToolPin{}, false
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
func syncInstalledVersions(ctx context.Context, lockPath string, report *exec.ExecReport, lg *slog.Logger) {
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

	lookupPin := func(name, kind string) (lock.ToolPin, bool) {
		if pin, ok := lockPinFor(lk, name, kind); ok {
			return pin, true
		}
		return lockPinFor(lk, name, "")
	}

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
		if pin, ok := lookupPin(name, ts.MethodKind); ok && pin.Latest != "" {
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
		pin, ok := lookupPin(tr.Tool, tr.MethodKind)
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
