package app

import (
	"context"
	"encoding/json"
	"testing"

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
