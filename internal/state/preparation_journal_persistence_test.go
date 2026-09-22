package state

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func preparationPersistenceFixture() (map[string]plan.PreparationPlan, map[string]plan.PreparationJournal) {
	makePlan := func(includePrerequisite bool) plan.PreparationPlan {
		sourceRollback := plan.Operation{Kind: "remove-source", Effect: plan.EffectMutation}
		prepare := []plan.PreparationMutation{{
			ID:        "source-add",
			Resource:  plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable"},
			Ownership: plan.OwnershipDepengine,
			Apply:     plan.Operation{Kind: "add-source", Effect: plan.EffectMutation},
			Rollback:  &sourceRollback,
			Policy:    plan.RollbackSafe,
		}}
		if includePrerequisite {
			prerequisiteRollback := plan.Operation{Kind: "remove-prerequisite", Effect: plan.EffectMutation}
			prepare = append(prepare, plan.PreparationMutation{
				ID:        "prereq-install",
				Resource:  plan.ResourceIdentity{Kind: plan.ResourcePrerequisite, Key: "helper"},
				Ownership: plan.OwnershipDepengine,
				Apply:     plan.Operation{Kind: "install-prerequisite", Effect: plan.EffectMutation},
				Rollback:  &prerequisiteRollback,
				Policy:    plan.RollbackSafe,
			})
		}
		return plan.PreparationPlan{
			Prepare: prepare,
			Commit:  []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}},
		}
	}
	plans := map[string]plan.PreparationPlan{
		"tool-a/native#0": makePlan(false),
		"tool-c/native#0": makePlan(false),
		"tool-b/http#1":   makePlan(true),
		"tool-d/native#0": makePlan(true),
	}
	journals := map[string]plan.PreparationJournal{
		"tool-a/native#0": {
			Status:  plan.PreparationReady,
			Applied: []string{"source-add"},
		},
		"tool-c/native#0": {
			Status:   plan.PreparationApplying,
			Applying: "source-add",
		},
		"tool-b/http#1": {
			Status:  plan.PreparationCommitting,
			Applied: []string{"source-add", "prereq-install"},
		},
		"tool-d/native#0": {
			Status:           plan.PreparationRollingBack,
			Applied:          []string{"source-add", "prereq-install"},
			RollbackApplied:  []string{"prereq-install"},
			RollbackApplying: "source-add",
		},
	}
	return plans, journals
}

func TestPreparationJournalsPersistThroughStateAndSnapshot(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)
	wantPlans, wantJournals := preparationPersistenceFixture()
	st := &State{
		Version:             currentStateVersion,
		Tools:               map[string]ToolState{},
		PreparationPlans:    wantPlans,
		PreparationJournals: wantJournals,
	}
	if err := Save(st); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.PreparationPlans, wantPlans) {
		t.Fatalf("preparation plans = %#v, want %#v", loaded.PreparationPlans, wantPlans)
	}
	if !reflect.DeepEqual(loaded.PreparationJournals, wantJournals) {
		t.Fatalf("preparation journals = %#v, want %#v", loaded.PreparationJournals, wantJournals)
	}

	info, err := SaveSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := LoadSnapshot(info.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.PreparationPlans, wantPlans) {
		t.Fatalf("snapshot preparation plans = %#v, want %#v", snapshot.PreparationPlans, wantPlans)
	}
	if !reflect.DeepEqual(snapshot.PreparationJournals, wantJournals) {
		t.Fatalf("snapshot preparation journals = %#v, want %#v", snapshot.PreparationJournals, wantJournals)
	}
}

func TestFreshAndPersistedStateInitializePreparationJournals(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)
	fresh, err := LoadFrom(filepath.Join(td, "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fresh.PreparationPlans == nil {
		t.Fatal("fresh state has nil preparation plan map")
	}
	if fresh.PreparationJournals == nil {
		t.Fatal("fresh state has nil preparation journal map")
	}

	st := &State{Version: currentStateVersion, Tools: map[string]ToolState{}}
	if err := Save(st); err != nil {
		t.Fatal(err)
	}
	persisted, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.PreparationPlans == nil {
		t.Fatal("persisted state has nil preparation plan map")
	}
	if persisted.PreparationJournals == nil {
		t.Fatal("persisted state has nil preparation journal map")
	}
}

func TestStateRejectsMalformedPreparationJournalOnSaveAndLoad(t *testing.T) {
	td := t.TempDir()
	t.Setenv("XDG_STATE_HOME", td)
	st := &State{
		Version: currentStateVersion,
		Tools:   map[string]ToolState{},
		PreparationPlans: map[string]plan.PreparationPlan{
			"tool-a/native#0": preparationTransactionPlan("repo:stable"),
		},
		PreparationJournals: map[string]plan.PreparationJournal{
			"tool-a/native#0": {Status: plan.PreparationPreparing},
		},
	}
	if err := Save(st); err == nil || !strings.Contains(err.Error(), "requires at least one applied mutation") {
		t.Fatalf("Save error = %v, want malformed journal rejection", err)
	}

	path := filepath.Join(td, "malformed.json")
	writeChecksummedStateForTest(t, path, State{
		Version: currentStateVersion,
		Tools:   map[string]ToolState{},
		PreparationPlans: map[string]plan.PreparationPlan{
			"tool-a/native#0": preparationTransactionPlan("repo:stable"),
		},
		PreparationJournals: map[string]plan.PreparationJournal{
			"tool-a/native#0": {Status: plan.PreparationPreparing},
		},
	})
	if _, err := LoadFrom(path); err == nil || !strings.Contains(err.Error(), "requires at least one applied mutation") {
		t.Fatalf("LoadFrom error = %v, want malformed journal rejection", err)
	}
}

func TestStateRejectsUnpairedPreparationTransactionState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	cases := []State{
		{
			Version: currentStateVersion,
			Tools:   map[string]ToolState{},
			PreparationJournals: map[string]plan.PreparationJournal{
				"tool-a/native#0": {Status: plan.PreparationPending},
			},
		},
		{
			Version: currentStateVersion,
			Tools:   map[string]ToolState{},
			PreparationPlans: map[string]plan.PreparationPlan{
				"tool-a/native#0": preparationTransactionPlan("repo:stable"),
			},
		},
	}
	for i := range cases {
		if err := Save(&cases[i]); err == nil || !strings.Contains(err.Error(), "preparation transactions") {
			t.Fatalf("Save case %d error = %v, want unpaired transaction rejection", i, err)
		}
	}
}

func TestStateRejectsCredentialBearingPreparationPlan(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := preparationTransactionPlan("repo:stable")
	p.Commit[0].Command = []string{"installer", "--token", "supersecret"}
	p.Commit[0].ArbitraryCode = true
	st := &State{
		Version: currentStateVersion,
		Tools:   map[string]ToolState{},
		PreparationPlans: map[string]plan.PreparationPlan{
			"tool-a/native#0": p,
		},
		PreparationJournals: map[string]plan.PreparationJournal{
			"tool-a/native#0": {Status: plan.PreparationPending},
		},
	}
	if err := Save(st); err == nil || !strings.Contains(err.Error(), "credential-bearing") {
		t.Fatalf("Save error = %v, want credential-bearing plan rejection", err)
	}
}

func TestStateRejectsInvalidPreparationTransactionIdentity(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	p := preparationTransactionPlan("repo:stable")
	st := &State{
		Version: currentStateVersion,
		Tools:   map[string]ToolState{},
		PreparationPlans: map[string]plan.PreparationPlan{
			" tool-a": p,
		},
		PreparationJournals: map[string]plan.PreparationJournal{
			" tool-a": {Status: plan.PreparationPending},
		},
	}
	if err := Save(st); err == nil {
		t.Fatal("Save unexpectedly accepted invalid transaction key")
	}
}

func TestStateRejectsTerminalPreparationJournalAsActive(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	for _, status := range []plan.PreparationStatus{plan.PreparationCommitted, plan.PreparationRolledBack} {
		st := &State{
			Version: currentStateVersion,
			Tools:   map[string]ToolState{},
			PreparationPlans: map[string]plan.PreparationPlan{
				"tool-a/native#0": preparationTransactionPlan("repo:stable"),
			},
			PreparationJournals: map[string]plan.PreparationJournal{
				"tool-a/native#0": {Status: status, Applied: []string{"source-add"}},
			},
		}
		if err := Save(st); err == nil || !strings.Contains(err.Error(), "terminal") {
			t.Fatalf("Save status %q error = %v, want terminal journal rejection", status, err)
		}
	}
}
