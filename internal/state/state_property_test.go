package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

// FuzzStatePersistenceRoundTripPreservesInvariants exercises the complete
// persisted-state envelope rather than isolated plan helpers. Valid ownership
// and active preparation-WAL state must survive Save/Load without semantic or
// byte-level drift, and re-saving an unchanged state must be deterministic.
func FuzzStatePersistenceRoundTripPreservesInvariants(f *testing.F) {
	f.Add("alpha", uint8(0), uint8(0))
	f.Add("repo/tool", uint8(1), uint8(3))
	f.Add("unicode-λ", uint8(2), uint8(7))
	f.Add("stable", uint8(3), uint8(15))

	f.Fuzz(func(t *testing.T, raw string, phase, flags uint8) {
		if len(raw) > 512 {
			return
		}
		t.Setenv("XDG_STATE_HOME", t.TempDir())

		st := fuzzStateFixture(t, raw, phase, flags)
		if err := Save(st); err != nil {
			t.Fatalf("Save(valid state): %v\nstate=%#v", err, st)
		}
		firstBytes, err := os.ReadFile(DefaultPath()) // #nosec G304 -- DefaultPath is the fixture-controlled state path under t.TempDir.
		if err != nil {
			t.Fatal(err)
		}
		firstChecksum := st.Checksum

		loaded, err := Load()
		if err != nil {
			t.Fatalf("Load(valid state): %v", err)
		}
		if err := validateStateSemantics(loaded); err != nil {
			t.Fatalf("round-tripped state violates semantics: %v", err)
		}
		if err := ValidateNoSecrets(loaded); err != nil {
			t.Fatalf("round-tripped state violates secret boundary: %v", err)
		}
		if !reflect.DeepEqual(loaded, st) {
			t.Fatalf("round trip changed state\nwritten=%#v\nloaded=%#v", st, loaded)
		}

		if err := Save(loaded); err != nil {
			t.Fatalf("Save(round-tripped state): %v", err)
		}
		secondBytes, err := os.ReadFile(DefaultPath()) // #nosec G304 -- DefaultPath is the fixture-controlled state path under t.TempDir.
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Checksum != firstChecksum {
			t.Fatalf("checksum changed across identical save: %q != %q", loaded.Checksum, firstChecksum)
		}
		if !bytes.Equal(secondBytes, firstBytes) {
			t.Fatal("identical state produced different persisted bytes")
		}
	})
}

// FuzzRejectedStateNeverReplacesPersistedState guards the validation-before-
// mutation boundary. Every deliberately invalid state, including nil, must be
// rejected without replacing the last known-good state file.
func FuzzRejectedStateNeverReplacesPersistedState(f *testing.F) {
	f.Add("bad", uint8(0))
	f.Add("credential", uint8(1))
	f.Add("ownership", uint8(2))
	f.Add("transaction", uint8(3))
	f.Add("terminal", uint8(4))
	f.Add("version", uint8(5))

	f.Fuzz(func(t *testing.T, raw string, variant uint8) {
		if len(raw) > 512 {
			return
		}
		t.Setenv("XDG_STATE_HOME", t.TempDir())

		baseline := fuzzStateFixture(t, "baseline", 0, 0)
		baseline.PreparationPlans = map[string]plan.PreparationPlan{}
		baseline.PreparationJournals = map[string]plan.PreparationJournal{}
		if err := Save(baseline); err != nil {
			t.Fatalf("Save(baseline): %v", err)
		}
		before, err := os.ReadFile(DefaultPath()) // #nosec G304 -- DefaultPath is the fixture-controlled state path under t.TempDir.
		if err != nil {
			t.Fatal(err)
		}

		candidate := invalidFuzzState(t, raw, variant)
		if err := Save(candidate); err == nil {
			t.Fatalf("Save(invalid variant %d) unexpectedly succeeded: %#v", variant%6, candidate)
		}

		after, err := os.ReadFile(DefaultPath()) // #nosec G304 -- DefaultPath is the fixture-controlled state path under t.TempDir.
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(after, before) {
			t.Fatalf("rejected state variant %d replaced the persisted baseline", variant%6)
		}
		loaded, err := Load()
		if err != nil {
			t.Fatalf("baseline no longer loads after rejected save: %v", err)
		}
		if !reflect.DeepEqual(loaded, baseline) {
			t.Fatalf("baseline changed after rejected save\nwant=%#v\ngot=%#v", baseline, loaded)
		}
	})
}

func fuzzStateFixture(t *testing.T, raw string, phase, flags uint8) *State {
	t.Helper()
	token := fuzzStateToken(raw)
	toolName := "tool-" + token
	transactionKey := toolName + "/native#0"
	preparationPlan := fuzzStatePreparationPlan()
	journal := fuzzStateJournal(t, preparationPlan, phase)

	dependents := []string{"dep-a-" + token}
	if flags&1 != 0 {
		dependents = append(dependents, "dep-b-"+token)
	}
	ownership := plan.OwnershipDepengine
	if flags&2 != 0 {
		ownership = plan.OwnershipExternal
	}

	return &State{
		Version:          currentStateVersion,
		SchemaPath:       "/tmp/schema-" + token + ".toml",
		SchemaModifiedAt: "2026-09-30T00:00:00Z",
		Tools: map[string]ToolState{
			toolName: {
				Method:           "native",
				MethodKind:       "native",
				InstalledAt:      "2026-09-30T00:00:00Z",
				PostinstallDone:  flags&4 != 0,
				DefinitionHash:   "definition-" + token,
				DesiredStateHash: "desired-" + token,
				Version:          "1." + token[:2],
				RootRequested:    flags&8 != 0,
				Config: map[string]any{
					"pkg": "pkg-" + token,
					"metadata": map[string]any{
						"channel": "stable-" + token,
					},
					"values": []any{"value-" + token, flags&1 != 0, float64(flags % 17)},
				},
			},
		},
		OwnedResources: []plan.OwnedResourceState{
			{
				Resource:   plan.ResourceIdentity{Kind: plan.ResourcePrerequisite, Key: "tool:compiler-" + token},
				Ownership:  ownership,
				Dependents: dependents,
			},
			{
				Resource:   plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable-" + token},
				Ownership:  plan.OwnershipDepengine,
				Dependents: []string{toolName},
			},
		},
		PreparationPlans: map[string]plan.PreparationPlan{
			transactionKey: preparationPlan,
		},
		PreparationJournals: map[string]plan.PreparationJournal{
			transactionKey: journal,
		},
	}
}

func invalidFuzzState(t *testing.T, raw string, variant uint8) *State {
	t.Helper()
	st := fuzzStateFixture(t, raw, 0, 0)
	key := "txn-" + fuzzStateToken(raw)
	p := fuzzStatePreparationPlan()

	switch variant % 6 {
	case 0:
		return nil
	case 1:
		st.PreparationPlans = map[string]plan.PreparationPlan{}
		st.PreparationJournals = map[string]plan.PreparationJournal{}
		st.Tools = map[string]ToolState{
			"tool": {Config: map[string]any{"build": "Authorization: Bearer secret-" + fuzzStateToken(raw)}},
		}
	case 2:
		st.PreparationPlans = map[string]plan.PreparationPlan{}
		st.PreparationJournals = map[string]plan.PreparationJournal{}
		resource := plan.OwnedResourceState{
			Resource:  plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:" + fuzzStateToken(raw)},
			Ownership: plan.OwnershipDepengine,
		}
		st.OwnedResources = []plan.OwnedResourceState{resource, resource}
	case 3:
		st.PreparationPlans = map[string]plan.PreparationPlan{key: p}
		st.PreparationJournals = map[string]plan.PreparationJournal{}
	case 4:
		st.PreparationPlans = map[string]plan.PreparationPlan{key: p}
		st.PreparationJournals = map[string]plan.PreparationJournal{
			key: {Status: plan.PreparationCommitted, Applied: []string{"source"}},
		}
	case 5:
		st.Version = currentStateVersion + 1
	}
	return st
}

func fuzzStatePreparationPlan() plan.PreparationPlan {
	rollback := plan.Operation{Kind: "source-remove", Effect: plan.EffectMutation}
	return plan.PreparationPlan{
		Prepare: []plan.PreparationMutation{{
			ID:        "source",
			Resource:  plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:transaction"},
			Ownership: plan.OwnershipDepengine,
			Apply:     plan.Operation{Kind: "source-add", Effect: plan.EffectMutation},
			Rollback:  &rollback,
			Policy:    plan.RollbackSafe,
		}},
		Commit: []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}},
	}
}

func fuzzStateJournal(t *testing.T, p plan.PreparationPlan, phase uint8) plan.PreparationJournal {
	t.Helper()
	j := plan.NewPreparationJournal()
	if phase%4 == 0 {
		return j
	}
	var err error
	j, err = j.PlanApply(p, "source")
	if err != nil {
		t.Fatalf("build applying journal: %v", err)
	}
	if phase%4 == 1 {
		return j
	}
	j, err = j.RecordApplied(p, "source")
	if err != nil {
		t.Fatalf("build ready journal: %v", err)
	}
	if phase%4 == 2 {
		return j
	}
	j, err = j.PlanCommit(p)
	if err != nil {
		t.Fatalf("build committing journal: %v", err)
	}
	return j
}

func fuzzStateToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:6])
}
