package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
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
	updateQuiet := new(bool)
	updateJSON := new(bool)
	updatePrune := new(bool)
	updateFormat := new(string)
	updateCatalog := new([]string)
	updateNoCatalog := new(bool)

	cmd := &cobra.Command{
		Use:     "update",
		Aliases: []string{"lock"},
		Short:   ifPT("Resolver referências mutáveis e gravar v2 quando toda ferramenta tem identidade imutável; manter v1 para schemas só nativos", "Resolve mutable references and write v2 when every tool has an immutable identity; keep v1 for native-only schemas"),
		Example: `  depengine update                    # resolve mutable references; write depengine.lock
  depengine update --dry-run -v       # show the pins without writing
  depengine update --profile desktop  # re-resolve only tagged tools; keep other pins
  depengine update --prune             # remove metadata for deleted tools`,
		GroupID: groupManage,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := normalizeOutputFormat(updateFormat, updateJSON); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return exitWithCode(2)
			}
			catalog, err := catalogFromCommand(cmd)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return exitWithCode(2)
			}
			return runUpdate(cmd.Context(), updateSchema, updateManifest, updateNoManifest, updateLock, updateProfile, updateFrozen, updateDryRun, updateVerbose, updateQuiet, updateJSON, updatePrune, catalog)
		},
	}
	f := cmd.Flags()
	f.StringVar(updateSchema, "schema", defaultSchemaPath(), "path to schema.toml")
	f.StringVar(updateManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(updateNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	addCatalogFlags(cmd, updateCatalog, updateNoCatalog)
	f.StringVar(updateLock, "lock", "", "path to depengine.lock (default: alongside schema.toml)")
	f.StringVar(updateProfile, "profile", "", ifPT("resolver apenas ferramentas com tag correspondente; os demais pins de um lock legível são preservados", "only re-resolve tools with matching tag; other pins from a readable lock are preserved"))
	f.BoolVar(updateFrozen, "frozen-lockfile", false, "abort if depengine.lock does not exist")
	f.BoolVar(updateDryRun, "dry-run", false, "show what would be updated without writing lock")
	f.BoolVar(updatePrune, "prune", false, "remove obsolete lock metadata for tools absent from the full effective schema")
	f.BoolVarP(updateVerbose, "verbose", "v", false, "include the resolved lock projection in the output")
	f.BoolVar(updateQuiet, "quiet", false, "suppress progress and the Next hint")
	f.BoolVar(updateJSON, "json", false, "shorthand for --format=json")
	f.StringVar(updateFormat, "format", "text", "output format: text or json")
	return cmd
}

func runUpdate(ctx context.Context, updateSchema, updateManifest *string, updateNoManifest *bool, updateLock, updateProfile *string, updateFrozen, updateDryRun, updateVerbose, updateQuiet, updateJSON, updatePrune *bool, catalog *config.Catalog) error {
	lg := log.Default
	quiet := *updateQuiet

	noManifest := *updateNoManifest
	manifestPath := *updateManifest
	manifestAuto := false
	if !noManifest && manifestPath == "" {
		manifestPath = config.DefaultManifestPath()
		if manifestPath != "" {
			manifestAuto = true
		}
	}

	project, err := loadProject(*updateSchema, projectLoadOptions{ManifestPath: manifestPath, ManifestAuto: manifestAuto, Provenance: true, Catalog: catalog})
	if err != nil {
		return presentSchemaLoadErrorFor("update", err, *updateJSON)
	}
	if err := validateToolSelectors(project.Schema, "", "", *updateProfile); err != nil {
		return err
	}
	project.presentWarnings("")
	*updateSchema = project.SchemaPath
	s, clan, facts, manifestCount := project.Schema, project.Clan, project.Facts, project.ManifestCount
	if manifestAuto && manifestCount > 0 {
		fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, manifestCount)
	}
	if project.CatalogCount > 0 {
		fmt.Fprintf(os.Stderr, "  catalog: %s (%s)\n", strings.Join(project.CatalogRoots, string(os.PathListSeparator)), catalogCountText(project.CatalogCount))
	}

	// Keep requested metadata for the complete effective schema before filtering.
	fullSchema := *s
	fullInstallTools := filterTools(s.Tools, "", "", "")
	projectionCoverage := universalLockCoverageNames(fullInstallTools)
	methodsHash, sourceHash, err := lock.SnapshotIntentMetadata(&fullSchema)
	if err != nil {
		return exitWithCode(1)
	}
	recipeDigests, err := lock.SnapshotRecipeDigests(&fullSchema)
	if err != nil {
		return exitWithCode(1)
	}
	s.Tools = filterTools(s.Tools, "", "", *updateProfile)
	// Aligned KV block up front — same header language as install — so the
	// plan (what schema, which target, how many tools) reads in one glance
	// before the spinner takes over the line below.
	c := newCLIStyle(os.Stderr)
	if !quiet {
		printKV(c, "depengine update",
			[2]string{"schema", *updateSchema},
			[2]string{"target", fmt.Sprintf("%s (%s) · %s", facts.DistroID, clan, facts.TargetArch)},
			[2]string{"tools", fmt.Sprintf("%d", len(s.Tools))},
		)
	}

	lockPath := lock.Path(*updateSchema, *updateLock)
	if *updateFrozen {
		if _, err := os.Stat(lockPath); err != nil { // #nosec G703 -- lockPath is derived from the validated schema path.
			lg.Error("frozen-lockfile: lockfile not found", "path", lockPath)
			return exitWithCode(2)
		}
	}
	// Keep a lock refresh from changing the desired target while a concurrent
	// install/upgrade is reconciling that target against the physical host.
	// All CLI writers acquire the host mutation guard before lockfile/state
	// transactions. Read-only previews do not need it.
	unlockMutations, err := holdHostMutations(ctx, *updateDryRun)
	if err != nil {
		return err
	}
	defer unlockMutations()

	// Hold the per-file OS lock across Load -> resolution -> Save. Locking
	// only Save would still lose concurrent profile updates.
	if !*updateDryRun {
		txn, err := lock.AcquireTransaction(ctx, lockPath)
		if err != nil {
			return fmt.Errorf("acquire update lock transaction: %w", err)
		}
		defer func() { _ = txn.Close() }()
	}
	oldLock, err := lock.Load(lockPath)
	if err != nil {
		lg.Warn("existing lock unreadable; pins not carried over", "path", lockPath, "error", err)
		oldLock = nil
	}
	done := func(string) {}
	if !quiet {
		done = spinner(ctx, "Resolving lockable selectors")
	}
	var newLock *lock.Lock
	legacy := !canPersistProjection(oldLock, *updateProfile)
	var nativeFallback []string
	var universalDocument *plan.LockDocument
	if !legacy {
		projectionSchema := fullSchema
		projectionSchema.Tools = make(map[string]*config.Tool, len(fullInstallTools))
		var previousDocument plan.LockDocument
		if oldLock != nil && oldLock.Version == lock.CurrentVersion {
			previousDocument, err = oldLock.ProjectionDocument()
			if err != nil {
				done("FAIL")
				lg.Error("decode previous universal lock", "error", err)
				return exitWithCode(2)
			}
		}
		for name, tool := range fullInstallTools {
			_, selected := s.Tools[name]
			if selected || universalLockIntentDrifted(oldLock, previousDocument, name, methodsHash, sourceHash, recipeDigests) {
				projectionSchema.Tools[name] = tool
			}
		}
		document, resolveErr := resolveUniversalLockDocument(ctx, &projectionSchema, clan, facts, *updateSchema, lg, oldLock, previousDocument, projectionCoverage, methodsHash)
		if resolveErr != nil {
			if names, ok := legacyNativeLockFallback(oldLock, resolveErr); ok {
				legacy = true
				nativeFallback = names
			} else {
				done("FAIL")
				lg.Error("resolve universal lock", "error", resolveErr)
				return exitWithCode(2)
			}
		}
		if !legacy {
			universalDocument = &document
			legacyTools := make(map[string]lock.ToolPin)
			if oldLock != nil {
				for key, pin := range oldLock.Tools {
					legacyTools[key] = pin
				}
			}
			newLock, err = lock.NewUniversal(document, methodsHash, sourceHash, legacyTools)
			if err == nil {
				err = newLock.SetRecipeDigests(recipeDigests)
			}
			if err != nil {
				done("FAIL")
				lg.Error(
					"build universal lock", "error", err, "hint",
					"depengine update needs an immutable identity for every selected tool; native package names alone are not version pins, so no partial v2 lock was written",
				)
				return exitWithCode(2)
			}
		}
	}
	if legacy {
		if err := lock.ValidateCatalogLockVersion(&fullSchema, oldLock); err != nil {
			done("FAIL")
			lg.Error("legacy lock cannot pin catalog recipes", "error", err, "hint", "run 'depengine update' without --profile")
			return exitWithCode(2)
		}
		newLock, err = lock.ResolveLegacyV1(ctx, s, run.OSExecRunner{})
		if err != nil {
			done("FAIL")
			lg.Error("resolve lock", "error", err)
			return exitWithCode(1)
		}
		newLock = lock.Merge(oldLock, newLock)
		if oldLock != nil {
			for name, oldHash := range oldLock.MethodsHash {
				if _, ok := newLock.MethodsHash[name]; !ok {
					newLock.MethodsHash[name] = oldHash
				}
			}
			if newLock.SourceHash == nil {
				newLock.SourceHash = make(map[string]string, len(oldLock.SourceHash))
			}
			for key, oldHash := range oldLock.SourceHash {
				if _, ok := newLock.SourceHash[key]; ok {
					continue
				}
				if _, toolWasResolved := s.Tools[lockCandidateToolName(key)]; toolWasResolved {
					continue
				}
				newLock.SourceHash[key] = oldHash
			}
		}
		lock.ApplyLegacyV1(s, newLock)
	}

	var prunedTools []string
	if *updatePrune {
		prunedTools = pruneRemovedLockEntries(newLock, fullSchema.Tools)
	}

	pinned := len(newLock.Tools)
	var projection *plan.LockDocument
	if newLock.Version == lock.CurrentVersion {
		if universalDocument == nil {
			done("FAIL")
			return fmt.Errorf("decode update projection: universal projection is unavailable")
		}
		projection = universalDocument
		pinned = len(projection.Entries)
	}
	if *updateDryRun {
		done(c.cyan("dry-run"))
		c.arrow("would write %d pins to %s", pinned, lockPath)
		if projection != nil {
			for _, entry := range projection.Entries {
				fmt.Fprintf(os.Stderr, "  %s  → %s\n", entry.Tool.Name, c.cyan(updateProjectionValue(entry)))
			}
		}
	} else {
		if err := lock.Save(lockPath, newLock); err != nil {
			done("FAIL")
			lg.Error("save lock", "error", err)
			return exitWithCode(1)
		}
		done(fmt.Sprintf("(%d pinned)", pinned))
	}
	if len(prunedTools) > 0 && !quiet {
		verb := "pruned"
		if *updateDryRun {
			verb = "would prune"
		}
		fmt.Fprintf(os.Stderr, "  %s obsolete lock metadata for: %s\n", verb, strings.Join(prunedTools, ", "))
	}
	if len(nativeFallback) > 0 {
		lg.Warn(
			"universal lock unavailable for native-only tools; using legacy v1 lock",
			"tools", strings.Join(nativeFallback, ", "),
			"hint", "native package names are not version pins; add an immutable method or a manager that exposes one to migrate this schema to v2",
		)
	}

	// Warn about installed tools whose versions no longer match the pins
	// that were just resolved. Warn-only: applying the new versions is a
	// separate install step.
	if !quiet {
		reportVersionDrift(ctx, s, newLock)
	}

	if *updateVerbose && projection != nil {
		for _, entry := range projection.Entries {
			fmt.Fprintf(os.Stderr, "  %s  → %s\n", entry.Tool.Name, c.cyan(updateProjectionValue(entry)))
		}
	}
	if *updateVerbose && projection == nil {
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
			fmt.Fprintf(os.Stderr, "  %s  → %s\n", padRight(key, keyW), c.cyan(updatePinValue(pin)))
			if pin.Checksum != "" {
				fmt.Fprintf(os.Stderr, "  %s  %s\n", padRight("", keyW), c.dim(pin.Checksum))
			}
		}
	}

	if !*updateDryRun {
		recordCommandJournal("update", state.JournalUpdated, s, "")
	}
	if hint := updateNextStepHint(*updateDryRun, newLock); hint != "" {
		printNextStep(c, quiet, false, hint)
	}
	if *updateJSON {
		report := buildUpdateMachineReport(lockPath, newLock, projection, *updateDryRun, nativeFallback, prunedTools)
		if err := writeMachineOutput(os.Stdout, "update", report); err != nil {
			return exitWithCode(3)
		}
	}
	return nil
}

// pruneRemovedLockEntries removes compatibility metadata for tools absent
// from the COMPLETE effective schema, not the selected --profile subset.
// Legacy pins/hashes for current tools are retained because their resolution
// is not necessarily part of this update. A v2 projection is authoritative
// and is never modified by this metadata-only cleanup.
func pruneRemovedLockEntries(updated *lock.Lock, fullTools map[string]*config.Tool) []string {
	if updated == nil {
		return nil
	}
	removed := make(map[string]struct{})
	obsolete := func(name string) bool {
		if name == "" {
			return false
		}
		if _, exists := fullTools[name]; exists {
			return false
		}
		removed[name] = struct{}{}
		return true
	}
	for key := range updated.Tools {
		if obsolete(lockCandidateToolName(key)) {
			delete(updated.Tools, key)
		}
	}
	for key := range updated.SourceHash {
		if obsolete(lockCandidateToolName(key)) {
			delete(updated.SourceHash, key)
		}
	}
	for name := range updated.MethodsHash {
		if obsolete(name) {
			delete(updated.MethodsHash, name)
		}
	}
	for name := range updated.RecipeDigest {
		if obsolete(name) {
			delete(updated.RecipeDigest, name)
		}
	}
	if len(removed) == 0 {
		return nil
	}
	names := make([]string, 0, len(removed))
	for name := range removed {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// updateMachineReport is the `update --format json` payload: every pin the
// resolved lock now carries, in stable tool order.
type updateMachineReport struct {
	LockPath       string             `json:"lock_path"`
	LockVersion    int                `json:"lock_version"`
	DryRun         bool               `json:"dry_run"`
	Pinned         int                `json:"pinned"`
	Tools          []updateMachinePin `json:"tools"`
	NativeFallback []string           `json:"native_fallback,omitempty"`
	PrunedTools    []string           `json:"pruned_tools,omitempty"`
}

type updateMachinePin struct {
	Tool string `json:"tool"`
	Pin  string `json:"pin"`
}

// buildUpdateMachineReport projects either lock representation into the same
// machine payload. Universal locks expose their projection entries; legacy v1
// locks expose sorted pin keys.
func buildUpdateMachineReport(lockPath string, newLock *lock.Lock, projection *plan.LockDocument, dryRun bool, nativeFallback, prunedTools []string) updateMachineReport {
	report := updateMachineReport{
		LockPath:       lockPath,
		LockVersion:    newLock.Version,
		DryRun:         dryRun,
		Tools:          []updateMachinePin{},
		NativeFallback: nativeFallback,
		PrunedTools:    prunedTools,
	}
	if projection != nil {
		for _, entry := range projection.Entries {
			report.Tools = append(report.Tools, updateMachinePin{Tool: entry.Tool.Name, Pin: updateProjectionValue(entry)})
		}
		report.Pinned = len(report.Tools)
	} else {
		keys := make([]string, 0, len(newLock.Tools))
		for key := range newLock.Tools {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			report.Tools = append(report.Tools, updateMachinePin{Tool: key, Pin: updatePinValue(newLock.Tools[key])})
		}
		report.Pinned = len(keys)
	}
	return report
}

// updateNextStepHint returns the command that applies a freshly written lock.
// A v2 projection can be consumed reproducibly with --frozen-lockfile; a v1
// legacy lock only supports a plain install. Pure.
func updateNextStepHint(dryRun bool, written *lock.Lock) string {
	if dryRun {
		return ""
	}
	if written != nil && written.Version == lock.CurrentVersion {
		return "run 'depengine install --frozen-lockfile' to apply the pinned versions reproducibly"
	}
	return "run 'depengine install' to apply the pinned versions"
}

func updateProjectionValue(entry plan.LockProjection) string {
	identity := entry.Identity
	if identity.Digest != "" {
		return identity.Digest
	}
	if identity.Revision != "" {
		return identity.Revision
	}
	if identity.Version != "" {
		return identity.Version
	}
	if identity.Package != "" {
		return identity.Package
	}
	return "locked"
}

func updatePinValue(pin lock.ToolPin) string {
	if pin.Latest != "" {
		return pin.Latest
	}
	if pin.Selector != "" && pin.Revision != "" {
		return pin.Selector + " @ " + pin.Revision
	}
	if pin.ContainerTag != "" && pin.ContainerDigest != "" {
		return "tag:" + pin.ContainerTag + " @ " + pin.ContainerDigest
	}
	if pin.PackageVersion != "" {
		return pin.PackageVersion
	}
	return pin.Revision
}

// reportVersionDrift compares freshly-resolved lock pins against the versions
// recorded in state and warns about installed tools that are now out of date.
// Warn-only by design: `depengine update` refreshes the lock; applying the new
// versions is a separate install step.
func reportVersionDrift(ctx context.Context, schema *config.Schema, newLock *lock.Lock) {
	if newLock == nil || (newLock.Version != lock.CurrentVersion && len(newLock.Tools) == 0) {
		return
	}
	var document plan.LockDocument
	if newLock.Version == lock.CurrentVersion {
		var err error
		document, err = newLock.ProjectionDocument()
		if err != nil {
			return
		}
	}
	ls, err := state.LoadSharedContext(ctx)
	if err != nil {
		return
	}
	defer func() { _ = ls.Close() }()
	st := ls.State()
	if len(st.Tools) == 0 {
		return
	}

	var drifted []string
	for name, ts := range st.Tools {
		if ts.Version == "" {
			continue
		}
		version := ""
		if newLock.Version == lock.CurrentVersion {
			if entry, ok := document.EntryForTool(name); ok {
				version = entry.Identity.Version
			}
		} else {
			tool := schema.Tools[name]
			pin, ok := legacyV1PinForToolState(newLock, name, tool, ts)
			if ok {
				version = legacyV1PinnedVersion(pin)
			}
		}
		if version != "" && state.VersionOutdated(ts.Version, version) {
			drifted = append(drifted, fmt.Sprintf("%s: installed %s, pinned %s", name, ts.Version, version))
		}
	}
	if len(drifted) == 0 {
		return
	}
	sort.Strings(drifted)
	c := newCLIStyle(os.Stderr)
	_, _ = fmt.Fprintln(c.w, c.yellow("  ⚠ version drift: installed versions differ from newly-pinned versions"))
	for _, d := range drifted {
		_, _ = fmt.Fprintf(c.w, "    %s\n", d)
	}
	_, _ = fmt.Fprintln(c.w, c.dim("    Run 'depengine upgrade' to apply."))
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
