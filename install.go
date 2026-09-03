package main

import (
	"context"
	"flag"
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
)

func runInstall(args []string) {
	// flags maintained in help.go:printCommandHelp
	installCmd := flag.NewFlagSet("install", flag.ExitOnError)
	installSchema := installCmd.String("schema", defaultSchemaPath(), "path to schema.toml")
	installManifest := installCmd.String("manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	installNoManifest := installCmd.Bool("no-manifest", false, "disable personal manifest (default: auto-detect)")
	installDryRun := installCmd.Bool("dry-run", false, "show what would be installed")
	installVerbose := installCmd.Bool("verbose", false, "detailed output")
	installJSON := installCmd.Bool("json", false, "JSON output")
	installOnly := installCmd.String("only", "", "only install specific tool")
	installSkip := installCmd.String("skip", "", "skip specific tools (comma-separated)")
	installProfile := installCmd.String("profile", "", "only install tools with matching tag (e.g. minimal,desktop,server)")
	installFrozen := installCmd.Bool("frozen-lockfile", false, "fail if depengine.lock does not exist or needs update")
	installDiagnose := installCmd.Bool("diagnose", false, "diagnostic mode: DEBUG + dry-run + verbose")
	installLogLevel := installCmd.String("log-level", "", "log level: debug, info, warn, error")
	installSortBy := installCmd.String("sort-by", "", "sort output by: name, status, method")
	installJobs := installCmd.Int("jobs", 1, "max concurrent installations (default 1 = sequential)")
	installAllowArbitrary := installCmd.Bool("allow-arbitrary-code", false, "suppress security warnings for build scripts / arbitrary code")
	installQuiet := installCmd.Bool("quiet", false, "suppress per-tool status lines; show only final summary")
	installCmd.Parse(args)

	lg := log.Default

	if *installDiagnose {
		// Only override flags the user didn't explicitly set.
		saw := make(map[string]bool)
		installCmd.Visit(func(f *flag.Flag) {
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
	title := "depengine install"
	if *installDryRun {
		title = "depengine install — dry run (no changes will be made)"
	}
	fmt.Fprintln(os.Stderr, boldIfColor(title))
	fmt.Fprintf(os.Stderr, "  schema   %s\n", *installSchema)
	if manifestAuto && manifestCount > 0 {
		fmt.Fprintf(os.Stderr, "  manifest %s (%d tools merged)\n", manifestPath, manifestCount)
	}
	fmt.Fprintf(os.Stderr, "  target   %s (%s) · %s\n", facts.DistroID, clan, facts.TargetArch)
	fmt.Fprintf(os.Stderr, "  tools    %d\n", len(s.Tools))
	fmt.Fprintln(os.Stderr)

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
		fmt.Fprintln(os.Stderr, colorWrap("36", "Dry run — no changes were made. Remove --dry-run to install."))
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
		fmt.Fprintln(os.Stderr, colorWrap("2", "Share schema.toml in git so others can reproduce your tools:"))
		fmt.Fprintln(os.Stderr, colorWrap("2", "  git add schema.toml depengine.lock && git commit"))
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
			fmt.Fprintln(os.Stderr, colorWrap("33", fmt.Sprintf("  ⚠ %s: installed version %s differs from pinned %s (remove and reinstall to upgrade)",
				tr.Tool, ts.Version, pin.Latest)))
		}
	}

	if changed {
		if err := ls.Save(); err != nil {
			lg.Warn("state save failed (version sync)", "error", err)
		}
	}
}

// boldIfColor wraps s in bold when the terminal supports color, matching
// pkg/exec's color decision (NO_COLOR / TERM=dumb / char-device check) so
// the CLI's own header line doesn't make a different call than the ✓/✗
// status lines and the Detail() table right below it.
func boldIfColor(s string) string {
	return colorWrap("1", s)
}

// colorWrap wraps s in ANSI code (an SGR parameter, e.g. "1" for bold, "33"
// for yellow) when color output is enabled, and returns s unchanged
// otherwise. Centralizes the on/off decision for install.go's own tip and
// warning lines (the "share schema.toml" hint, the pinned-version-mismatch
// warning, the dry-run footer) so they don't each re-derive it.
func colorWrap(code, s string) string {
	if !exec.ShouldUseColor() {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}
