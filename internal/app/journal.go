package app

import (
	"context"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/log"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/state"
	"github.com/spf13/cobra"
)

// newJournalCmd builds `depengine journal`: a read-only query over the
// append-only mutation journal next to the state file.
func newJournalCmd() *cobra.Command {
	journalTool := new(string)
	journalCommand := new(string)
	journalResult := new(string)
	journalSince := new(string)
	journalUntil := new(string)
	journalLimit := new(int)
	journalJSON := new(bool)
	journalFormat := new(string)

	cmd := &cobra.Command{
		Use:   "journal",
		Short: ifPT("Consultar o journal append-only de mutações", "Query the append-only mutation journal"),
		Long: ifPT(
			"Mostra o histórico durável de install/remove/upgrade/update/undo, com filtro por ferramenta, comando, resultado e janela de tempo. O journal fica em journal.jsonl ao lado do state.json, sem segredos.",
			"Shows the durable install/remove/upgrade/update/undo history, filterable by tool, command, result, and time window. The journal lives in journal.jsonl next to state.json and contains no secrets.",
		),
		Example: `  depengine journal                          # recent mutations
  depengine journal --tool nvim --since 7d   # one tool, last week
  depengine journal --result failed --json   # failures as machine output`,
		GroupID: groupInspect,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := normalizeOutputFormat(journalFormat, journalJSON); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				return exitWithCode(2)
			}
			return runJournal(journalOptions{
				tool:    *journalTool,
				command: *journalCommand,
				result:  *journalResult,
				since:   *journalSince,
				until:   *journalUntil,
				limit:   *journalLimit,
				jsonOut: *journalJSON,
			})
		},
	}
	f := cmd.Flags()
	f.StringVar(journalTool, "tool", "", "only entries for this tool")
	f.StringVar(journalCommand, "command", "", "only entries from this command (install, remove, upgrade, update, undo)")
	f.StringVar(journalResult, "result", "", "only entries with this result (success, already, failed, skipped, updated, removed, reverted)")
	f.StringVar(journalSince, "since", "", "start of the time window (RFC3339, YYYY-MM-DD, or duration such as 7d/24h)")
	f.StringVar(journalUntil, "until", "", "end of the time window (same formats as --since)")
	f.IntVar(journalLimit, "limit", 50, "maximum entries to show, most recent first; 0 means all")
	f.BoolVar(journalJSON, "json", false, "shorthand for --format=json")
	f.StringVar(journalFormat, "format", "text", "output format: text or json")
	return cmd
}

type journalOptions struct {
	tool    string
	command string
	result  string
	since   string
	until   string
	limit   int
	jsonOut bool
}

// journalCommands are the commands that append journal entries. --command is
// validated against this closed set so a typo fails instead of matching
// nothing.
var journalCommands = []string{"install", "remove", "upgrade", "update", "undo"}

func runJournal(opts journalOptions) error {
	if opts.limit < 0 {
		fmt.Fprintln(os.Stderr, "error: --limit must not be negative")
		return exitWithCode(2)
	}
	if opts.command != "" && !slices.Contains(journalCommands, opts.command) {
		fmt.Fprintf(os.Stderr, "error: --command must be one of %s; got %q\n", strings.Join(journalCommands, ", "), opts.command)
		return exitWithCode(2)
	}
	if opts.result != "" && !state.IsJournalResult(opts.result) {
		fmt.Fprintf(os.Stderr, "error: --result must be one of %s; got %q\n", strings.Join(state.JournalResults(), ", "), opts.result)
		return exitWithCode(2)
	}
	now := time.Now().UTC()
	since, err := parseJournalBound(opts.since, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: --since: %v\n", err)
		return exitWithCode(2)
	}
	until, err := parseJournalBound(opts.until, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: --until: %v\n", err)
		return exitWithCode(2)
	}
	path := state.DefaultJournalPath()
	entries, err := state.QueryJournal(path, state.JournalFilter{
		Tool:    opts.tool,
		Command: opts.command,
		Result:  opts.result,
		Since:   since,
		Until:   until,
		Limit:   opts.limit,
	})
	if err != nil {
		log.Default.Error("read journal", "error", err)
		return exitWithCode(3)
	}
	if opts.jsonOut {
		return writeMachineOutput(os.Stdout, "journal", journalMachineReport{Path: path, Count: len(entries), Entries: entries})
	}
	printJournalEntries(path, entries)
	return nil
}

type journalMachineReport struct {
	Path    string               `json:"path"`
	Count   int                  `json:"count"`
	Entries []state.JournalEntry `json:"entries"`
}

func printJournalEntries(path string, entries []state.JournalEntry) {
	cs := newCLIStyle(os.Stdout)
	if len(entries) == 0 {
		fmt.Fprintln(os.Stderr, cs.dim("No journal entries match."))
		return
	}
	widths := [4]int{20, 9, 20, 9}
	for _, entry := range entries {
		widths[2] = max(widths[2], len(entry.Tool))
		widths[3] = max(widths[3], len(entry.Method))
	}
	_, _ = fmt.Fprintf(os.Stdout, "%-*s  %-*s  %-*s  %-*s  %s\n", widths[0], "TIME (UTC)", widths[1], "COMMAND", widths[2], "TOOL", widths[3], "METHOD", "RESULT")
	for _, entry := range entries {
		stamp := entry.Timestamp
		if parsed, err := time.Parse(time.RFC3339Nano, entry.Timestamp); err == nil {
			stamp = parsed.UTC().Format("2006-01-02 15:04:05")
		}
		_, _ = fmt.Fprintf(os.Stdout, "%-*s  %-*s  %-*s  %-*s  %s\n",
			widths[0], stamp, widths[1], entry.Command, widths[2], entry.Tool, widths[3], entry.Method, entry.Result)
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s\n", cs.dim(fmt.Sprintf("%d entries · %s", len(entries), path)))
}

// parseJournalBound accepts an absolute timestamp, a date, or a duration
// relative to now. Both --since and --until interpret a duration as "before
// now", so `--since 24h` and `--until 24h` both mean one day ago.
func parseJournalBound(raw string, now time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC(), nil
		}
	}
	if duration, err := time.ParseDuration(raw); err == nil {
		return now.Add(-duration), nil
	}
	return time.Time{}, fmt.Errorf("expected RFC3339, YYYY-MM-DD, or a duration like 24h; got %q", raw)
}

// recordJournal appends entries without ever failing the command that produced
// them: the mutation already happened and must not be reported as failed just
// because its audit record could not be written.
func recordJournal(entries ...state.JournalEntry) {
	if err := state.AppendJournal(context.Background(), entries...); err != nil {
		log.Default.Warn("journal append failed", "error", err)
	}
}

func journalIdentity(p *plan.ResolvedInstallPlan) *state.JournalIdentity {
	if p == nil {
		return nil
	}
	identity := &state.JournalIdentity{
		Package:  p.Identity.Package,
		Version:  p.Identity.Version,
		Revision: p.Identity.Revision,
		Source:   p.Identity.Source,
		Digest:   p.Identity.Digest,
	}
	if *identity == (state.JournalIdentity{}) {
		return nil
	}
	return identity
}

func journalElevated(p *plan.ResolvedInstallPlan) bool {
	if p == nil {
		return false
	}
	for _, operation := range p.Operations {
		if operation.Elevation == plan.ElevationRequired {
			return true
		}
	}
	return false
}

func commandSchemaHash(tools map[string]*config.Tool) string {
	if len(tools) == 0 {
		return ""
	}
	return state.SchemaHash(&config.Schema{Tools: tools})
}

func recordInstallJournal(report *exec.ExecReport, schema *config.Schema) {
	if report == nil {
		return
	}
	hash := state.SchemaHash(schema)
	var entries []state.JournalEntry
	for _, result := range report.Tools {
		resultName, exitCode, ok := journalInstallResult(result.Status)
		if !ok {
			continue
		}
		entry := state.NewJournalEntry("install")
		entry.Tool = result.Tool
		entry.Method = result.Method
		entry.Provider = result.Provider
		entry.Result = resultName
		entry.ExitCode = exitCode
		entry.SchemaHash = hash
		entry.Identity = journalIdentity(result.PlanIntent)
		entry.Elevated = journalElevated(result.PlanIntent)
		entry.Detail = result.Error
		entries = append(entries, entry)
	}
	recordJournal(entries...)
}

func journalInstallResult(status exec.StatusEnum) (string, int, bool) {
	switch status {
	case exec.StatusInstalled:
		return state.JournalSuccess, 0, true
	case exec.StatusAlready:
		return state.JournalAlready, 0, true
	case exec.StatusFailed:
		return state.JournalFailed, 1, true
	case exec.StatusSkippedWhen, exec.StatusSkippedUnavailable:
		return state.JournalSkipped, 0, true
	default:
		return "", 0, false
	}
}

func recordRemoveJournal(command string, tools map[string]*config.Tool, removed map[string]bool, requested map[string]bool, states map[string]state.ToolState) {
	hash := commandSchemaHash(tools)
	var entries []state.JournalEntry
	names := make([]string, 0, len(requested))
	for name := range requested {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		entry := state.NewJournalEntry(command)
		entry.Tool = name
		entry.Method = states[name].Method
		entry.SchemaHash = hash
		if removed[name] {
			entry.Result = state.JournalRemoved
			if command == "undo" {
				entry.Result = state.JournalReverted
			}
		} else {
			entry.Result = state.JournalFailed
			entry.ExitCode = 1
		}
		entries = append(entries, entry)
	}
	recordJournal(entries...)
}

func recordUpgradeJournal(results []upgradeResult, schema *config.Schema, dryRun bool) {
	if dryRun {
		return
	}
	hash := state.SchemaHash(schema)
	var entries []state.JournalEntry
	for _, result := range results {
		if result.Status == "would_upgrade" {
			continue
		}
		entry := state.NewJournalEntry("upgrade")
		entry.Tool = result.Tool
		entry.Method = result.Method
		// Upgrade's CLI statuses are not the journal's stable result values.
		switch result.Status {
		case "upgraded":
			entry.Result = state.JournalUpdated
		case "already-current":
			entry.Result = state.JournalAlready
		case "skipped":
			entry.Result = state.JournalSkipped
		case "failed":
			entry.Result = state.JournalFailed
		default:
			continue
		}
		entry.Identity = &state.JournalIdentity{Version: result.NewVer}
		entry.SchemaHash = hash
		entry.Detail = result.Error
		if result.Status == "failed" {
			entry.ExitCode = 1
		}
		entries = append(entries, entry)
	}
	recordJournal(entries...)
}

func recordCommandJournal(command, result string, schema *config.Schema, detail string) {
	entry := state.NewJournalEntry(command)
	entry.Result = result
	entry.Detail = detail
	if schema != nil {
		entry.SchemaHash = state.SchemaHash(schema)
	}
	if result == state.JournalFailed {
		entry.ExitCode = 1
	}
	recordJournal(entry)
}
