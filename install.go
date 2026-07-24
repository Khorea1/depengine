package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"depengine/pkg/exec"
	"depengine/pkg/git"
	"depengine/pkg/httpdownload"
	"depengine/pkg/ecosystem"
	"depengine/pkg/lock"
	"depengine/pkg/log"
	"depengine/pkg/run"
	"depengine/pkg/schema"
	"depengine/pkg/state"
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
	installFrozen := installCmd.Bool("frozen-lockfile", false, "fail if schema.lock does not exist or needs update")
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
		manifestPath = schema.DefaultManifestPath()
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
	if manifestAuto && manifestCount > 0 {
		fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, manifestCount)
	}
	if helper := s.Defaults.AurHelper; helper != "" {
		ecosystem.ReconfigureAUR(helper)
	}

	fmt.Fprintf(os.Stderr, "depengine install: distro=%s clan=%s arch=%s tools=%d\n",
		facts.DistroID, clan, facts.TargetArch, len(s.Tools))

	schemaFile, err := os.Stat(*installSchema)
	if err != nil {
		lg.Error("stat schema", "error", err)
		os.Exit(exitCodeForError(err))
	}

	if *installVerbose {
		fmt.Fprintln(os.Stderr, "depengine: --verbose is deprecated; output is now verbose by default. Use --quiet for the old summary-only behavior.")
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
	} else if *installVerbose || *installDryRun {
		fmt.Fprint(os.Stderr, report.Detail())
	} else {
		fmt.Fprintln(os.Stderr, report.Summary())
	}

	if !*installDryRun {
		saveLockfile(ctx, s, lockPath, lk, lg, *installDiagnose)
	}

	// After successful install, guide the user to share.
	if report.Failed == 0 && report.Success > 0 && !*installDryRun {
		fmt.Fprintln(os.Stderr, "Share schema.toml in git so others can reproduce your tools:")
		fmt.Fprintln(os.Stderr, "  git add schema.toml schema.lock && git commit")
	}

	if report.Failed > 0 {
		os.Exit(1)
	}
}
