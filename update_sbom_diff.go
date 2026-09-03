package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/ecosystem"
	"github.com/Khorea1/depengine/pkg/lock"
	"github.com/Khorea1/depengine/pkg/log"
	"github.com/Khorea1/depengine/pkg/run"
	"github.com/Khorea1/depengine/pkg/sbom"
	"github.com/Khorea1/depengine/pkg/state"
)

func runUpdate(args []string) {
	// flags maintained in help.go:printCommandHelp
	updateCmd := flag.NewFlagSet("update", flag.ExitOnError)
	updateSchema := updateCmd.String("schema", defaultSchemaPath(), "path to schema.toml")
	updateManifest := updateCmd.String("manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	updateNoManifest := updateCmd.Bool("no-manifest", false, "disable personal manifest (default: auto-detect)")
	updateLock := updateCmd.String("lock", "", "path to depengine.lock (default: alongside schema.toml)")
	updateProfile := updateCmd.String("profile", "", "only resolve & pin tools with matching tag")
	updateFrozen := updateCmd.Bool("frozen-lockfile", false, "abort if depengine.lock does not exist")
	updateDryRun := updateCmd.Bool("dry-run", false, "show what would be updated without writing lock")
	updateVerbose := updateCmd.Bool("v", false, "detailed output")
	updateCmd.Parse(args)

	ctx := context.Background()
	lg := log.Default

	noManifest := *updateNoManifest
	manifestPath := *updateManifest
	manifestAuto := false
	if !noManifest && manifestPath == "" {
		manifestPath = config.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}

	s, clan, facts, manifestCount, err := loadSchemaWithManifest(*updateSchema, manifestPath)
	if err != nil {
		log.Default.Error("load schema", "error", err)
		os.Exit(exitCodeForError(err))
	}
	if manifestAuto && manifestCount > 0 {
		fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, manifestCount)
	}
	if helper := s.Defaults.AurHelper; helper != "" {
		ecosystem.ReconfigureAUR(helper)
	}

	s.Tools = filterTools(s.Tools, "", "", *updateProfile)

	// Aligned KV block up front — same header language as install — so the
	// plan (what schema, which target, how many tools) reads in one glance
	// before the spinner takes over the line below.
	c := newCLIStyle(os.Stderr)
	printKV(c, "depengine update",
		[2]string{"schema", *updateSchema},
		[2]string{"target", fmt.Sprintf("%s (%s) · %s", facts.DistroID, clan, facts.TargetArch)},
		[2]string{"tools", fmt.Sprintf("%d", len(s.Tools))},
	)

	done := spinner(ctx, "Resolving latest versions")
	newLock, err := lock.ResolveAll(ctx, s, run.OSExecRunner{})
	if err != nil {
		done("FAIL")
		lg.Error("resolve lock", "error", err)
		os.Exit(1)
	}

	lockPath := *updateLock
	if lockPath == "" {
		lockPath = lock.DefaultPath(*updateSchema)
	}
	if *updateFrozen {
		if _, err := os.Stat(lockPath); err != nil {
			lg.Error("frozen-lockfile: lockfile not found", "path", lockPath)
			os.Exit(2)
		}
	}
	pinned := len(newLock.Tools)
	if *updateDryRun {
		done(c.cyan("dry-run"))
		c.arrow("would pin %d versions to %s", pinned, lockPath)
	} else {
		if err := lock.Save(lockPath, newLock); err != nil {
			done("FAIL")
			lg.Error("save lock", "error", err)
			os.Exit(1)
		}
		done(fmt.Sprintf("(%d pinned)", pinned))
	}

	// Warn about installed tools whose versions no longer match the pins
	// that were just resolved. Warn-only: applying the new versions is a
	// separate install step.
	reportVersionDrift(newLock)

	if *updateVerbose {
		// Sorted so reruns are diffable; aligned so the eye scans the
		// version column, not the ragged arrow column.
		keys := make([]string, 0, len(newLock.Tools))
		for key := range newLock.Tools {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		keyW := 0
		for _, k := range keys {
			if len(k) > keyW {
				keyW = len(k)
			}
		}
		for _, key := range keys {
			pin := newLock.Tools[key]
			fmt.Fprintf(os.Stderr, "  %s  → %s\n", padRight(key, keyW), c.cyan(pin.Latest))
			if pin.Checksum != "" {
				fmt.Fprintf(os.Stderr, "  %s  %s\n", padRight("", keyW), c.dim(pin.Checksum))
			}
		}
	}

	if !*updateDryRun {
		fmt.Fprintln(c.w, c.dim("Run 'depengine install' to apply."))
	}
}

// reportVersionDrift compares freshly-resolved lock pins against the versions
// recorded in state and warns about installed tools that are now out of date.
// Warn-only by design: `depengine update` refreshes the lock; applying the new
// versions is a separate install step.
func reportVersionDrift(newLock *lock.Lock) {
	if newLock == nil || len(newLock.Tools) == 0 {
		return
	}
	ls, err := state.LoadShared()
	if err != nil {
		return
	}
	defer ls.Close()
	st := ls.State()
	if len(st.Tools) == 0 {
		return
	}

	var drifted []string
	for name, ts := range st.Tools {
		if ts.Version == "" {
			continue
		}
		pin, ok := lockPinFor(newLock, name, ts.MethodKind)
		if !ok || pin.Latest == "" {
			continue
		}
		if state.VersionOutdated(ts.Version, pin.Latest) {
			drifted = append(drifted, fmt.Sprintf("%s: installed %s, pinned %s", name, ts.Version, pin.Latest))
		}
	}
	if len(drifted) == 0 {
		return
	}
	sort.Strings(drifted)
	c := newCLIStyle(os.Stderr)
	fmt.Fprintln(c.w, c.yellow("  ⚠ version drift: installed versions differ from newly-pinned versions"))
	for _, d := range drifted {
		fmt.Fprintf(c.w, "    %s\n", d)
	}
	fmt.Fprintln(c.w, c.dim("    Run 'depengine upgrade' to apply."))
}

func runSBOM(args []string) {
	// flags maintained in help.go:printCommandHelp
	sbomCmd := flag.NewFlagSet("sbom", flag.ExitOnError)
	sbomFormat := sbomCmd.String("format", "cyclonedx", "output format: cyclonedx or spdx")
	sbomCmd.Parse(args)

	ls, err := state.LoadShared()
	if err != nil {
		log.Default.Error("load state", "error", err)
		os.Exit(3)
	}
	defer ls.Close()

	st := ls.State()

	// Fall back to lock pins for tools whose recorded version is empty
	// (e.g. state files written before version tracking): 0.0.0 should only
	// appear when nothing is knowable.
	if st.SchemaPath != "" {
		if lk, lerr := lock.Load(lock.DefaultPath(st.SchemaPath)); lerr == nil && lk != nil {
			for name, ts := range st.Tools {
				if ts.Version != "" {
					continue
				}
				if pin, ok := lockPinFor(lk, name, ts.MethodKind); ok && pin.Latest != "" {
					ts.Version = pin.Latest
					st.Tools[name] = ts
				}
			}
		}
	}

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
func runDiff(args []string) {
	// flags maintained in help.go:printCommandHelp
	diffCmd := flag.NewFlagSet("diff", flag.ExitOnError)
	diffOther := diffCmd.String("other", "", "path to other state file (used when no args)")
	diffJSON := diffCmd.Bool("json", false, "output as JSON")
	diffArgs := parseFlagsInterspersed(diffCmd, args)

	var aPath, bPath string
	var aState, bState *state.State
	var err error

	switch len(diffArgs) {
	case 0:
		if *diffOther == "" {
			fmt.Fprintf(os.Stderr, "error: --other is required when no arguments are given\n")
			os.Exit(2)
		}
		bPath = *diffOther
	case 1:
		bPath = diffArgs[0]
	case 2:
		aPath = diffArgs[0]
		bPath = diffArgs[1]
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

	if len(diffArgs) != 2 {
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
	}

	items := state.Diff(aState, bState)
	if len(items) == 0 {
		if *diffJSON {
			fmt.Println("[]")
		} else {
			fmt.Fprintln(os.Stderr, "No differences found.")
		}
		return
	}

	if *diffJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(items); err != nil {
			log.Default.Error("encode JSON", "error", err)
			os.Exit(3)
		}
	} else {
		c := newCLIStyle(os.Stderr)
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

		// Section headers are bold, counts live in the header line, and each
		// entry is one aligned line — a diff is scanned section-first, so the
		// header must carry the "is there anything here" answer by itself.
		if onlyA > 0 {
			fmt.Fprintf(c.w, "\n%s\n", c.bold(fmt.Sprintf("Only in current (%d)", onlyA)))
			for _, item := range items {
				if item.Side == "only_a" {
					fmt.Fprintf(c.w, "  %s %s  %s\n", c.green("+"), item.Name, c.dim(fmt.Sprintf("%s, %s", item.MethodA, item.InstalledAtA)))
				}
			}
		}

		if onlyB > 0 {
			fmt.Fprintf(c.w, "\n%s\n", c.bold(fmt.Sprintf("Only in other (%d)", onlyB)))
			for _, item := range items {
				if item.Side == "only_b" {
					fmt.Fprintf(c.w, "  %s %s  %s\n", c.red("-"), item.Name, c.dim(fmt.Sprintf("%s, %s", item.MethodB, item.InstalledAtB)))
				}
			}
		}

		if diffCount > 0 {
			fmt.Fprintf(c.w, "\n%s\n", c.bold(fmt.Sprintf("Definition changed (%d)", diffCount)))
			for _, item := range items {
				if item.Side == "different" {
					fmt.Fprintf(c.w, "  %s %s\n", c.yellow("~"), item.Name)
					fmt.Fprintf(c.w, "    %s %s %s\n", c.dim("current:"), item.MethodA, c.dim(fmt.Sprintf("(hash: %s)", item.HashA)))
					fmt.Fprintf(c.w, "    %s %s %s\n", c.dim("other:  "), item.MethodB, c.dim(fmt.Sprintf("(hash: %s)", item.HashB)))
				}
			}
		}

		fmt.Fprintf(c.w, "\n%s\n", c.dim(fmt.Sprintf("%s differ.", plural(len(items), "tool"))))
	}
}

// isTerminal reports whether the given file is connected to a terminal.
func isTerminal(f *os.File) bool {
	fi, _ := f.Stat()
	return fi != nil && fi.Mode()&os.ModeCharDevice != 0
}

// spinner runs a simple terminal spinner. It returns a done function that
// the caller must call with the final status text (e.g. "done (42 pinned)").
// When stderr is not a terminal, no spinner is shown and done simply prints
// the message + status.
func spinner(ctx context.Context, msg string) func(status string) {
	if !isTerminal(os.Stderr) {
		fmt.Fprint(os.Stderr, msg+" ")
		return func(status string) {
			fmt.Fprintf(os.Stderr, "%s\n", status)
		}
	}
	chars := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	stop := make(chan struct{})
	go func() {
		i := 0
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			default:
				fmt.Fprintf(os.Stderr, "\r%s %s ", msg, chars[i%len(chars)])
				i++
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()
	return func(status string) {
		close(stop)
		fmt.Fprintf(os.Stderr, "\r%s %s\n", msg, status)
	}
}
