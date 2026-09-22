package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

// newUpdateCmd builds `depengine update`.
func newUpdateCmd() *cobra.Command {
	updateSchema := new(string)
	updateManifest := new(string)
	updateNoManifest := new(bool)
	updateLock := new(string)
	updateProfile := new(string)
	updateFrozen := new(bool)
	updateDryRun := new(bool)
	updateVerbose := new(bool)

	cmd := &cobra.Command{
		Use:     "update",
		Short:   ifPT("Resolver e fixar versões em depengine.lock", "Resolve and pin versions into depengine.lock"),
		GroupID: groupManage,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runUpdate(cmd.Context(), updateSchema, updateManifest, updateNoManifest, updateLock, updateProfile, updateFrozen, updateDryRun, updateVerbose)
		},
	}
	f := cmd.Flags()
	f.StringVar(updateSchema, "schema", defaultSchemaPath(), "path to schema.toml")
	f.StringVar(updateManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(updateNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	f.StringVar(updateLock, "lock", "", "path to depengine.lock (default: alongside schema.toml)")
	f.StringVar(updateProfile, "profile", "", "only resolve & pin tools with matching tag")
	f.BoolVar(updateFrozen, "frozen-lockfile", false, "abort if depengine.lock does not exist")
	f.BoolVar(updateDryRun, "dry-run", false, "show what would be updated without writing lock")
	f.BoolVar(updateVerbose, "v", false, "detailed output")
	return cmd
}

func runUpdate(ctx context.Context, updateSchema, updateManifest *string, updateNoManifest *bool, updateLock, updateProfile *string, updateFrozen, updateDryRun, updateVerbose *bool) error {
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
		return exitWithCode(exitCodeForError(err))
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
		return exitWithCode(1)
	}

	lockPath := *updateLock
	if lockPath == "" {
		lockPath = lock.DefaultPath(*updateSchema)
	}
	if *updateFrozen {
		if _, err := os.Stat(lockPath); err != nil {
			lg.Error("frozen-lockfile: lockfile not found", "path", lockPath)
			return exitWithCode(2)
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
			return exitWithCode(1)
		}
		done(fmt.Sprintf("(%d pinned)", pinned))
	}

	// Warn about installed tools whose versions no longer match the pins
	// that were just resolved. Warn-only: applying the new versions is a
	// separate install step.
	reportVersionDrift(s, newLock)

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
	return nil
}

// reportVersionDrift compares freshly-resolved lock pins against the versions
// recorded in state and warns about installed tools that are now out of date.
// Warn-only by design: `depengine update` refreshes the lock; applying the new
// versions is a separate install step.
func reportVersionDrift(schema *config.Schema, newLock *lock.Lock) {
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
		tool := schema.Tools[name]
		pin, ok := lockPinForToolState(newLock, name, tool, ts)
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
