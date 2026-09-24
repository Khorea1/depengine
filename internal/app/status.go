package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/engine"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

// newStatusCmd builds `depengine status`.
func newStatusCmd() *cobra.Command {
	statusSchema := new(string)
	statusManifest := new(string)
	statusNoManifest := new(bool)
	statusFormat := new(string)
	statusJSON := new(bool)
	statusOrphans := new(bool)

	cmd := &cobra.Command{
		Use:     "status",
		Aliases: []string{"st"},
		Short:   ifPT("Mostrar estado das ferramentas em relação ao schema", "Show tool installation state vs schema"),
		Long:    ifPT("Reconcilia o estado rastreado com a identidade observada no host. Os status incluem installed, missing, outdated, unknown e broken.", "Reconciles tracked state with identity observed on the host. Status values include installed, missing, outdated, unknown, and broken."),
		GroupID: groupInspect,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd.Context(), statusSchema, statusManifest, statusNoManifest, statusFormat, statusJSON, statusOrphans)
		},
	}
	f := cmd.Flags()
	f.StringVar(statusSchema, "schema", "", "override schema path")
	f.StringVar(statusManifest, "manifest", "", "path to personal manifest (default: $XDG_CONFIG_HOME/depengine/manifest.toml)")
	f.BoolVar(statusNoManifest, "no-manifest", false, "disable personal manifest (default: auto-detect)")
	f.StringVar(statusFormat, "format", "text", "output format: text or json")
	f.BoolVar(statusJSON, "json", false, "JSON output (shorthand for --format=json)")
	f.BoolVar(statusOrphans, "orphans", false, "show only orphaned tools")
	return cmd
}

// runStatus compares tracked and observed host state with the current schema.
func runStatus(ctx context.Context, statusSchema, statusManifest *string, statusNoManifest *bool, statusFormat *string, statusJSON, statusOrphans *bool) error {
	normalizeStatusFormat(statusFormat, statusJSON)

	ls, err := openStatusState()
	if err != nil {
		return err
	}
	defer ls.Close()

	st := ls.State()

	schemaPath, stop := resolveStatusSchemaPath(st, statusSchema)
	if stop {
		return nil
	}

	s, lk := loadStatusSchema(schemaPath, statusManifest, statusNoManifest)

	tools := classifyStatusTools(st.Tools, s, lk, *statusOrphans)
	if s != nil && !*statusOrphans {
		facts, factsErr := engine.GatherFacts(run.OSExecRunner{})
		if factsErr == nil {
			ex := exec.New()
			exec.WithRunner(run.OSExecRunner{})(ex)
			exec.WithFacts(facts)(ex)
			exec.WithDefaultMethodOrder(s.Defaults.MethodOrder)(ex)
			tools = reconcileStatusTools(ctx, tools, st.Tools, s, lk, ex, engine.ResolveFamily(facts))
		} else {
			log.Default.Warn("gather host facts for status", "error", factsErr)
			for i := range tools {
				if tools[i].Status == "orphaned" || tools[i].Status == "missing" {
					continue
				}
				tools[i].Status = "unknown"
				verification := plan.VerificationResult{State: plan.StateUnknown, Detail: factsErr.Error()}
				tools[i].Verification = &verification
			}
		}
	}

	if *statusFormat == "json" {
		return renderStatusJSON(tools)
	}
	return renderStatusTable(tools, *statusOrphans)
}

// normalizeStatusFormat folds the deprecated --json shorthand into --format.
func normalizeStatusFormat(statusFormat *string, statusJSON *bool) {
	if *statusJSON {
		if *statusFormat == "text" {
			*statusFormat = "json"
		}
		fmt.Fprintln(os.Stderr, "depengine: --json is deprecated; use --format=json instead")
	}
}

// openStatusState loads the shared state file for reading.
func openStatusState() (*state.LockedState, error) {
	ls, err := state.LoadShared()
	if err != nil {
		log.Default.Error("state lock", "error", err)
		return nil, exitWithCode(3)
	}
	return ls, nil
}

// resolveStatusSchemaPath picks the schema to compare against: the explicit
// --schema flag, else the path recorded in state. It reports stop=true when
// there is nothing to compare against (empty state, no schema given).
func resolveStatusSchemaPath(st *state.State, statusSchema *string) (string, bool) {
	schemaPath := st.SchemaPath
	if *statusSchema != "" {
		schemaPath = *statusSchema
	}
	if schemaPath == "" {
		if len(st.Tools) == 0 {
			fmt.Fprintln(os.Stderr, "No tools in state (nothing installed yet). Use --schema to compare against a schema.")
			return "", true
		}
	}
	return schemaPath, false
}

// loadStatusSchema loads the lockfile alongside the schema for
// installed-vs-pinned version comparisons (outdated detection). A missing
// lock is fine. A schema that fails to parse (or a manifest that fails to
// merge) degrades to state-only reporting with a warning.
func loadStatusSchema(schemaPath string, statusManifest *string, statusNoManifest *bool) (*config.Schema, *lock.Lock) {
	var lk *lock.Lock
	if schemaPath != "" {
		lk, _ = lock.Load(lock.DefaultPath(schemaPath))
	}

	var s *config.Schema
	if schemaPath != "" {
		var err error
		s, err = config.ParseProjectSchema(schemaPath, nil)
		if err != nil {
			log.Default.Warn("load schema for comparison", "error", err)
			s = nil
		}

		if s != nil {
			noManifest := *statusNoManifest
			manifestPath := *statusManifest
			manifestAuto := false
			if !noManifest && manifestPath == "" {
				manifestPath = config.DefaultManifestPath()
				if manifestPath != "" {
					manifestAuto = true
				}
			}
			if manifestPath != "" {
				merged, count, merr := mergeManifest(s, manifestPath, false)
				if merr != nil {
					log.Default.Warn("load manifest", "error", merr)
				} else if count > 0 {
					s = merged
					if manifestAuto {
						fmt.Fprintf(os.Stderr, "  manifest: %s (%d tools merged)\n", manifestPath, count)
					}
				}
			}
		}
	}
	return s, lk
}

// toolStatus is one row of the status report.
type toolStatus struct {
	Name         string                   `json:"name"`
	Status       string                   `json:"status"`
	Method       string                   `json:"method,omitempty"`
	Version      string                   `json:"version,omitempty"`
	Updated      string                   `json:"updated,omitempty"`
	Verification *plan.VerificationResult `json:"verification,omitempty"`
}

// statusToolOutdated reports whether an installed tool drifted from the
// schema: either its definition changed since install, or its installed
// version differs from the pinned one.
func statusToolOutdated(ts state.ToolState, stTool *config.Tool, lk *lock.Lock, name string) bool {
	// Definition drift: the schema definition changed since install.
	if ts.DefinitionHash != "" && state.DefinitionHash(stTool) != ts.DefinitionHash {
		return true
	}
	// Version drift: the installed version differs from the pinned one.
	if ts.Version != "" {
		if pin, ok := lockPinForToolState(lk, name, stTool, ts); ok && state.VersionOutdated(ts.Version, pin.Latest) {
			return true
		}
	}
	return false
}

func reconcileStatusTools(ctx context.Context, rows []toolStatus, installed map[string]state.ToolState, schema *config.Schema, lk *lock.Lock, ex *exec.Executor, clan string) []toolStatus {
	for i := range rows {
		row := &rows[i]
		if row.Status == "orphaned" || row.Status == "missing" {
			continue
		}
		tool := schema.Tools[row.Name]
		ts := installed[row.Name]
		definitionDrift := ts.DefinitionHash != "" && state.DefinitionHash(tool) != ts.DefinitionHash
		method, methodErr := findTrackedMethodCandidate(tool, ts, ex.DefaultMethodOrder(), ex.NativeManagerName())
		if methodErr != nil {
			row.Status = "unknown"
			verification := plan.VerificationResult{State: plan.StateUnknown, Detail: methodErr.Error()}
			row.Verification = &verification
			continue
		}
		ex.SetHostContext(clan)
		desiredVersion := ""
		if pin, ok := lockPinForCandidate(lk, row.Name, tool, method); ok {
			desiredVersion = pin.Latest
		}
		_, verification, err := ex.ResolveAndVerifyCandidateAtVersion(ctx, tool, method, desiredVersion)
		if err != nil {
			row.Status = "unknown"
			v := plan.VerificationResult{State: plan.StateUnknown, Detail: err.Error()}
			row.Verification = &v
			continue
		}
		row.Verification = &verification
		if slices.Contains(verification.KnownFields, plan.FieldVersion) {
			row.Version = verification.Observed.Version
		}
		switch verification.State {
		case plan.StateSatisfied:
			if definitionDrift {
				row.Status = "outdated"
			} else {
				row.Status = "installed"
			}
		case plan.StateAbsent:
			row.Status = "missing"
		case plan.StateDrifted:
			row.Status = "outdated"
		case plan.StateUnknown:
			row.Status = "unknown"
		case plan.StateBroken:
			row.Status = "broken"
		}
	}
	return rows
}

// classifyStatusTools compares installed state against the schema (plus lock)
// and labels every tool as installed, orphaned, outdated, or missing. A nil
// schema means state-only reporting: everything counts as installed.
func classifyStatusTools(installed map[string]state.ToolState, s *config.Schema, lk *lock.Lock, orphansOnly bool) []toolStatus {
	var tools []toolStatus

	for name, ts := range installed {
		status := "installed"
		if s != nil {
			if _, inSchema := s.Tools[name]; !inSchema {
				status = "orphaned"
			}
		}
		if orphansOnly && status != "orphaned" {
			continue
		}
		if status == "installed" && s != nil && !orphansOnly {
			if stTool, inSchema := s.Tools[name]; inSchema {
				if statusToolOutdated(ts, stTool, lk, name) {
					status = "outdated"
				}
			}
		}
		tools = append(tools, toolStatus{
			Name:    name,
			Status:  status,
			Method:  ts.Method,
			Version: ts.Version,
			Updated: ts.InstalledAt,
		})
	}

	if s != nil && !orphansOnly {
		for name := range s.Tools {
			if _, inState := installed[name]; !inState {
				tools = append(tools, toolStatus{
					Name:   name,
					Status: "missing",
				})
			}
		}
	}
	return tools
}

// renderStatusJSON emits the report as indented JSON on stdout.
func renderStatusJSON(tools []toolStatus) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(tools); err != nil {
		log.Default.Error("json output", "error", err)
		return exitWithCode(3)
	}
	return nil
}

// sortStatusTools orders the report for actionability: outdated tools need
// attention, missing ones block the schema, orphaned ones are cleanup
// candidates. Installed and healthy come last — they're the background noise.
func sortStatusTools(tools []toolStatus) {
	sort.SliceStable(tools, func(i, j int) bool {
		pi, pj := statusRank(tools[i].Status), statusRank(tools[j].Status)
		if pi != pj {
			return pi < pj
		}
		return tools[i].Name < tools[j].Name
	})
}

// renderStatusTable emits the human-readable report table on stderr plus a
// summary line of per-status counts.
func renderStatusTable(tools []toolStatus, orphansOnly bool) error {
	if len(tools) == 0 {
		if orphansOnly {
			fmt.Fprintln(os.Stderr, "No orphan tools.")
		} else {
			fmt.Fprintln(os.Stderr, "No tools in state. Run 'depengine install' first.")
		}
		return nil
	}

	sortStatusTools(tools)

	c := newCLIStyle(os.Stderr)
	nameW, stW, methW, verW := len("Tool"), len("Status"), len("Method"), len("Version")
	for _, t := range tools {
		if len(t.Name) > nameW {
			nameW = len(t.Name)
		}
		if len(t.Status) > stW {
			stW = len(t.Status)
		}
		if len(t.Method) > methW {
			methW = len(t.Method)
		}
		if len(t.Version) > verW {
			verW = len(t.Version)
		}
	}

	fmt.Fprintf(c.w, "  %s  %s  %s  %s  %s\n",
		c.dim(padRight("Tool", nameW)), c.dim(padRight("Status", stW)),
		c.dim(padRight("Method", methW)), c.dim(padRight("Version", verW)), c.dim("Installed"))
	counts := map[string]int{}
	for _, t := range tools {
		counts[t.Status]++
		method := t.Method
		if method == "" {
			method = "—"
		}
		version := t.Version
		if version == "" {
			version = "—"
		}
		// Relative time scans faster than an RFC3339 timestamp for a "how old
		// is this install" question the status table answers constantly.
		installed := "—"
		if ts, err := time.Parse(time.RFC3339, t.Updated); err == nil {
			installed = relativeTime(ts)
		} else if t.Updated != "" {
			installed = t.Updated
		}
		fmt.Fprintf(c.w, "  %s  %s  %s  %s  %s\n",
			padRight(t.Name, nameW), statusStyled(c, padRight(t.Status, stW), t.Status),
			padRight(method, methW), c.dim(padRight(version, verW)), c.dim(installed))
	}

	fmt.Fprintln(c.w)
	var parts []string
	for _, st := range []string{"broken", "outdated", "missing", "unknown", "orphaned", "installed"} {
		if n := counts[st]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, st))
		}
	}
	fmt.Fprintf(c.w, "  %s\n", c.dim(strings.Join(parts, "  ·  ")))
	return nil
}

func statusRank(s string) int {
	switch s {
	case "broken":
		return 0
	case "outdated":
		return 1
	case "missing":
		return 2
	case "unknown":
		return 3
	case "orphaned":
		return 4
	default: // installed
		return 5
	}
}

// statusStyled colors a status word by severity. The string must already be
// padded — the caller pads before colorizing so ANSI escapes don't break
// column alignment.
func statusStyled(c *cliStyle, padded, status string) string {
	switch status {
	case "broken", "missing":
		return c.red(padded)
	case "outdated":
		return c.yellow(padded)
	case "unknown", "orphaned":
		return c.yellow(padded)
	default: // installed
		return c.green(padded)
	}
}
