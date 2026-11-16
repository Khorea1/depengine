package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/state"
)

func TestJournalCommandReportsFilteredEntries(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	install := state.NewJournalEntry("install")
	install.Tool = "nvim"
	install.Method = "github"
	install.Result = state.JournalSuccess
	remove := state.NewJournalEntry("remove")
	remove.Tool = "old"
	remove.Result = state.JournalRemoved
	if err := state.AppendJournal(context.Background(), install, remove); err != nil {
		t.Fatalf("seed journal: %v", err)
	}

	code, out := runCommand(t, "journal", []string{"XDG_STATE_HOME=" + stateHome}, "--tool", "nvim", "--json")
	if code != 0 {
		t.Fatalf("journal exit = %d, want 0 (output:\n%s)", code, out)
	}
	envelope := decodeEnvelope(t, out)
	if envelope.Command != "journal" {
		t.Fatalf("command = %q, want journal", envelope.Command)
	}
	var data struct {
		Path    string               `json:"path"`
		Count   int                  `json:"count"`
		Entries []state.JournalEntry `json:"entries"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("decode journal payload: %v", err)
	}
	if data.Count != 1 || len(data.Entries) != 1 {
		t.Fatalf("count = %d, entries = %d, want 1", data.Count, len(data.Entries))
	}
	if data.Entries[0].Tool != "nvim" || data.Entries[0].Command != "install" {
		t.Fatalf("unexpected entry: %+v", data.Entries[0])
	}
	if data.Path == "" {
		t.Fatal("journal path is empty")
	}
}

// seedCLIJournal writes fixed entries into a temp state home and returns the
// environment the journal subprocess needs to find them.
func seedCLIJournal(t *testing.T, entries ...state.JournalEntry) []string {
	t.Helper()
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)
	if err := state.AppendJournal(context.Background(), entries...); err != nil {
		t.Fatalf("seed journal: %v", err)
	}
	return []string{"XDG_STATE_HOME=" + stateHome}
}

func fixedCLIJournalEntry(command, tool, result, stamp string) state.JournalEntry {
	entry := state.NewJournalEntry(command)
	entry.Timestamp = stamp
	entry.Tool = tool
	entry.Method = "native"
	entry.Result = result
	return entry
}

func TestJournalTextOutputShowsEntriesAndSummary(t *testing.T) {
	env := seedCLIJournal(t,
		fixedCLIJournalEntry("install", "nvim", state.JournalSuccess, "2026-01-01T10:00:00Z"),
		fixedCLIJournalEntry("remove", "old", state.JournalRemoved, "2026-01-02T10:00:00Z"),
	)
	code, stdout, stderr := runCommandStreams(t, "journal", env)
	if code != 0 {
		t.Fatalf("journal exit = %d, stderr:\n%s", code, stderr)
	}
	for _, want := range []string{"TIME (UTC)", "COMMAND", "nvim", "install", "2026-01-01 10:00:00", "old", "removed"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stderr, "2 entries") || !strings.Contains(stderr, "journal.jsonl") {
		t.Errorf("stderr summary missing count or path:\n%s", stderr)
	}
}

func TestJournalTextOutputReportsNoMatches(t *testing.T) {
	env := seedCLIJournal(t, fixedCLIJournalEntry("install", "nvim", state.JournalSuccess, "2026-01-01T10:00:00Z"))
	code, stdout, stderr := runCommandStreams(t, "journal", env, "--tool", "absent")
	if code != 0 {
		t.Fatalf("journal exit = %d, stderr:\n%s", code, stderr)
	}
	if stdout != "" {
		t.Fatalf("stdout = %q, want empty for no matches", stdout)
	}
	if !strings.Contains(stderr, "No journal entries match.") {
		t.Fatalf("stderr = %q, want the no-match message", stderr)
	}
}

func TestJournalFiltersByCommandResultAndTimeWindow(t *testing.T) {
	env := seedCLIJournal(t,
		fixedCLIJournalEntry("install", "first", state.JournalSuccess, "2026-01-01T10:00:00Z"),
		fixedCLIJournalEntry("install", "second", state.JournalFailed, "2026-01-02T10:00:00Z"),
		fixedCLIJournalEntry("remove", "third", state.JournalRemoved, "2026-01-03T10:00:00Z"),
	)
	cases := []struct {
		name string
		args []string
		want []string
		drop []string
	}{
		{name: "command", args: []string{"--command", "remove"}, want: []string{"third"}, drop: []string{"first", "second"}},
		{name: "result", args: []string{"--result", state.JournalFailed}, want: []string{"second"}, drop: []string{"first", "third"}},
		{name: "since RFC3339", args: []string{"--since", "2026-01-02T00:00:00Z"}, want: []string{"second", "third"}, drop: []string{"first"}},
		{name: "since date", args: []string{"--since", "2026-01-03"}, want: []string{"third"}, drop: []string{"first", "second"}},
		{name: "until date", args: []string{"--until", "2026-01-02"}, want: []string{"first"}, drop: []string{"second", "third"}},
		{name: "since and until", args: []string{"--since", "2026-01-02", "--until", "2026-01-03"}, want: []string{"second"}, drop: []string{"first", "third"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runCommand(t, "journal", env, tc.args...)
			if code != 0 {
				t.Fatalf("journal %v exit = %d: %s", tc.args, code, out)
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("output missing %q:\n%s", want, out)
				}
			}
			for _, dropped := range tc.drop {
				if strings.Contains(out, dropped) {
					t.Errorf("output kept filtered-out %q:\n%s", dropped, out)
				}
			}
		})
	}
}

func TestJournalSinceDurationUsesRelativeWindow(t *testing.T) {
	recent := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	env := seedCLIJournal(t,
		fixedCLIJournalEntry("install", "old", state.JournalSuccess, "2026-01-01T10:00:00Z"),
		fixedCLIJournalEntry("install", "recent", state.JournalSuccess, recent),
	)
	code, out := runCommand(t, "journal", env, "--since", "24h")
	if code != 0 {
		t.Fatalf("journal exit = %d: %s", code, out)
	}
	if !strings.Contains(out, "recent") || strings.Contains(out, "old") {
		t.Fatalf("--since 24h window = %s, want only the recent entry", out)
	}
	code, out = runCommand(t, "journal", env, "--until", "24h")
	if code != 0 {
		t.Fatalf("journal exit = %d: %s", code, out)
	}
	if !strings.Contains(out, "old") || strings.Contains(out, "recent") {
		t.Fatalf("--until 24h window = %s, want only the older entry", out)
	}
}

func TestJournalLimitBoundsAndJSONPayload(t *testing.T) {
	env := seedCLIJournal(t,
		fixedCLIJournalEntry("install", "first", state.JournalSuccess, "2026-01-01T10:00:00Z"),
		fixedCLIJournalEntry("install", "second", state.JournalSuccess, "2026-01-02T10:00:00Z"),
		fixedCLIJournalEntry("install", "third", state.JournalSuccess, "2026-01-03T10:00:00Z"),
	)
	decode := func(t *testing.T, out string) (int, []string, string) {
		t.Helper()
		envelope := decodeEnvelope(t, out)
		var data struct {
			Path    string               `json:"path"`
			Count   int                  `json:"count"`
			Entries []state.JournalEntry `json:"entries"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			t.Fatalf("decode journal payload: %v", err)
		}
		if data.Count != len(data.Entries) {
			t.Fatalf("count = %d, entries = %d", data.Count, len(data.Entries))
		}
		if data.Path == "" {
			t.Fatal("journal path is empty")
		}
		tools := make([]string, len(data.Entries))
		for i, entry := range data.Entries {
			tools[i] = entry.Tool
		}
		return data.Count, tools, envelope.Command
	}

	code, out := runCommand(t, "journal", env, "--limit", "0", "--json")
	if code != 0 {
		t.Fatalf("--limit 0 exit = %d: %s", code, out)
	}
	count, tools, command := decode(t, out)
	if count != 3 || strings.Join(tools, ",") != "first,second,third" || command != "journal" {
		t.Fatalf("--limit 0 = %d %v %q, want all three in order", count, tools, command)
	}

	code, out = runCommand(t, "journal", env, "--limit", "1", "--json")
	if code != 0 {
		t.Fatalf("--limit 1 exit = %d: %s", code, out)
	}
	count, tools, _ = decode(t, out)
	if count != 1 || tools[0] != "third" {
		t.Fatalf("--limit 1 = %d %v, want only the most recent entry", count, tools)
	}

	code, out = runCommand(t, "journal", env, "--limit", "-1")
	if code != 2 {
		t.Fatalf("--limit -1 exit = %d, want 2: %s", code, out)
	}
	if !strings.Contains(out, "--limit must not be negative") {
		t.Fatalf("--limit -1 output = %q", out)
	}
}

func TestJournalRejectsInvalidFilters(t *testing.T) {
	env := seedCLIJournal(t, fixedCLIJournalEntry("install", "nvim", state.JournalSuccess, "2026-01-01T10:00:00Z"))
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "result", args: []string{"--result", "nope"}, want: "--result must be one of"},
		{name: "command", args: []string{"--command", "nope"}, want: "--command must be one of"},
		{name: "since", args: []string{"--since", "not-a-time"}, want: "--since:"},
		{name: "until", args: []string{"--until", "not-a-time"}, want: "--until:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, out := runCommand(t, "journal", env, tc.args...)
			if code != 2 {
				t.Fatalf("journal %v exit = %d, want 2: %s", tc.args, code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("output = %q, want %q", out, tc.want)
			}
		})
	}
}

func TestRecordInstallJournalWritesPerToolEntries(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	intent := plan.New("jq", "native", true)
	intent.Identity.Package = "jq"
	intent.Identity.Version = "1.7.1"
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation, Elevation: plan.ElevationRequired}}
	report := &exec.ExecReport{Tools: []exec.ToolResult{
		{Tool: "jq", Status: exec.StatusInstalled, Method: "native", PlanIntent: &intent},
		{Tool: "broken", Status: exec.StatusFailed, Method: "http", Error: "checksum mismatch"},
	}}
	recordInstallJournal(report, &config.Schema{Tools: map[string]*config.Tool{"jq": {Name: "jq"}}})

	entries, err := state.QueryJournal(state.DefaultJournalPath(), state.JournalFilter{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if entries[0].Tool != "jq" || entries[0].Result != state.JournalSuccess || !entries[0].Elevated {
		t.Fatalf("unexpected success entry: %+v", entries[0])
	}
	if entries[0].Identity == nil || entries[0].Identity.Version != "1.7.1" {
		t.Fatalf("missing identity: %+v", entries[0].Identity)
	}
	if entries[0].SchemaHash == "" {
		t.Fatal("schema hash is empty")
	}
	if entries[1].Result != state.JournalFailed || entries[1].ExitCode != 1 {
		t.Fatalf("unexpected failure entry: %+v", entries[1])
	}
}

func TestRecordUpgradeJournalUsesStableResultVocabulary(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	recordUpgradeJournal([]upgradeResult{
		{Tool: "newer", Status: "upgraded", NewVer: "v2"},
		{Tool: "same", Status: "already-current", NewVer: "v1"},
		{Tool: "skipped", Status: "skipped"},
		{Tool: "broken", Status: "failed", Error: "upgrade failed"},
		{Tool: "preview", Status: "would_upgrade"},
	}, nil, false)

	for _, tc := range []struct {
		result string
		tool   string
	}{
		{result: state.JournalUpdated, tool: "newer"},
		{result: state.JournalAlready, tool: "same"},
		{result: state.JournalSkipped, tool: "skipped"},
		{result: state.JournalFailed, tool: "broken"},
	} {
		t.Run(tc.result, func(t *testing.T) {
			got, err := state.QueryJournal(state.DefaultJournalPath(), state.JournalFilter{Command: "upgrade", Result: tc.result})
			if err != nil || len(got) != 1 || got[0].Tool != tc.tool {
				t.Fatalf("filter %q: entries = %+v, err=%v", tc.result, got, err)
			}
			if tc.result == state.JournalFailed && got[0].ExitCode != 1 {
				t.Errorf("failed upgrade exit code = %d, want 1", got[0].ExitCode)
			}
		})
	}
	all, err := state.QueryJournal(state.DefaultJournalPath(), state.JournalFilter{})
	if err != nil || len(all) != 4 {
		t.Fatalf("non-dry-run entries = %+v, err=%v", all, err)
	}
	recordUpgradeJournal([]upgradeResult{{Tool: "dry", Status: "upgraded"}}, nil, true)
	all, err = state.QueryJournal(state.DefaultJournalPath(), state.JournalFilter{})
	if err != nil || len(all) != 4 {
		t.Fatalf("dry-run appended an entry: %+v, err=%v", all, err)
	}
}

func TestRecordRemoveJournalUsesRevertedForUndo(t *testing.T) {
	stateHome := t.TempDir()
	t.Setenv("XDG_STATE_HOME", stateHome)

	removed := map[string]bool{"success": true}
	requested := map[string]bool{"success": true, "failure": true}
	recordRemoveJournal("undo", nil, removed, requested, nil)
	recordRemoveJournal("remove", nil, removed, requested, nil)

	for _, tc := range []struct {
		command string
		result  string
		tool    string
	}{
		{command: "undo", result: state.JournalReverted, tool: "success"},
		{command: "undo", result: state.JournalFailed, tool: "failure"},
		{command: "remove", result: state.JournalRemoved, tool: "success"},
		{command: "remove", result: state.JournalFailed, tool: "failure"},
	} {
		t.Run(tc.command+"/"+tc.result, func(t *testing.T) {
			got, err := state.QueryJournal(state.DefaultJournalPath(), state.JournalFilter{Command: tc.command, Result: tc.result})
			if err != nil || len(got) != 1 || got[0].Tool != tc.tool {
				t.Fatalf("filter %s/%s: entries = %+v, err=%v", tc.command, tc.result, got, err)
			}
		})
	}
}
