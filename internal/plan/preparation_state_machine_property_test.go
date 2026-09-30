package plan

import (
	"reflect"
	"testing"
)

// FuzzPreparationJournalSuccessfulTransitionsStayValid exercises the preparation
// WAL as a state machine. Every successful transition must return a journal
// that validates against the exact plan, preserve canonical ownership state,
// and leave its inputs untouched so retry/recovery code can safely retain the
// pre-transition snapshot.
func FuzzPreparationJournalSuccessfulTransitionsStayValid(f *testing.F) {
	f.Add([]byte{0, 1, 0, 1, 4, 6})
	f.Add([]byte{0, 2, 0, 3, 0, 1, 7, 8, 9, 12})
	f.Add([]byte{0, 1, 0, 1, 7, 8, 10, 12})

	plan := PreparationPlan{
		Prepare: []PreparationMutation{
			{ID: "source", Resource: ResourceIdentity{Kind: ResourceSource, Key: "repo:stable"}, Ownership: OwnershipDepengine, Apply: Operation{Kind: "source-add", Effect: EffectMutation}, Rollback: &Operation{Kind: "source-remove", Effect: EffectMutation}, Policy: RollbackSafe},
			{ID: "prereq", Resource: ResourceIdentity{Kind: ResourcePrerequisite, Key: "tool:compiler"}, Ownership: OwnershipDepengine, Apply: Operation{Kind: "prereq-install", Effect: EffectMutation}, Policy: RollbackRetain},
		},
		Commit: []Operation{{Kind: "install", Effect: EffectMutation}},
	}
	if err := plan.Validate(); err != nil {
		f.Fatal(err)
	}

	f.Fuzz(func(t *testing.T, actions []byte) {
		if len(actions) > 128 {
			return
		}
		j := NewPreparationJournal()
		var owned []OwnedResourceState
		for step, b := range actions {
			beforeJ := clonePreparationJournalForFuzz(j)
			beforeOwned := cloneOwnedForFuzz(owned)
			var next PreparationJournal
			var nextOwned []OwnedResourceState
			var err error
			changedOwned := false
			switch b % 13 {
			case 0:
				if len(j.Applied) >= len(plan.Prepare) {
					continue
				}
				next, err = j.PlanApply(plan, plan.Prepare[len(j.Applied)].ID)
			case 1:
				if j.Applying == "" {
					continue
				}
				next, err = j.RecordApplied(plan, j.Applying)
			case 2:
				if j.Applying == "" {
					continue
				}
				next, err = j.ResolveApplying(plan, j.Applying, PreparationMutationApplied)
			case 3:
				if j.Applying == "" {
					continue
				}
				next, err = j.ResolveApplying(plan, j.Applying, PreparationMutationNotApplied)
			case 4:
				next, err = j.PlanCommit(plan)
			case 5:
				next, err = j.ResolveCommitNotApplied(plan)
			case 6:
				next, nextOwned, err = j.FinalizeCommit(plan, owned, "demo")
				changedOwned = err == nil
			case 7:
				next, _, err = j.PlanRollback(plan, owned)
			case 8:
				if j.Status != PreparationRollingBack || j.RollbackApplying != "" {
					continue
				}
				decision, e := j.remainingRollback(plan, owned)
				if e != nil || len(decision.OperationIDs) == 0 {
					continue
				}
				next, _, err = j.PlanRollbackApply(plan, owned, decision.OperationIDs[0])
			case 9:
				if j.RollbackApplying == "" {
					continue
				}
				next, err = j.RecordRollbackApplied(plan, owned, j.RollbackApplying)
			case 10:
				if j.RollbackApplying == "" {
					continue
				}
				next, err = j.ResolveRollbackApplying(plan, owned, j.RollbackApplying, PreparationMutationApplied)
			case 11:
				if j.RollbackApplying == "" {
					continue
				}
				next, err = j.ResolveRollbackApplying(plan, owned, j.RollbackApplying, PreparationMutationNotApplied)
			case 12:
				next, nextOwned, err = j.FinalizeRollback(plan, owned)
				changedOwned = err == nil
			}
			if !reflect.DeepEqual(j, beforeJ) || !reflect.DeepEqual(owned, beforeOwned) {
				t.Fatalf("step %d action %d mutated input\nbefore journal=%#v\nafter journal=%#v\nbefore owned=%#v\nafter owned=%#v", step, b%13, beforeJ, j, beforeOwned, owned)
			}
			if err != nil {
				continue
			}
			if err := next.Validate(plan); err != nil {
				t.Fatalf("step %d action %d returned invalid journal: %v\n%#v", step, b%13, err, next)
			}
			j = next
			if changedOwned {
				if err := ValidateOwnedResourceSnapshot(nextOwned); err != nil {
					t.Fatalf("step %d action %d returned invalid ownership: %v", step, b%13, err)
				}
				owned = nextOwned
			}
		}
	})
}

func clonePreparationJournalForFuzz(j PreparationJournal) PreparationJournal {
	out := j
	out.Applied = append([]string(nil), j.Applied...)
	out.RollbackApplied = append([]string(nil), j.RollbackApplied...)
	return out
}

func cloneOwnedForFuzz(in []OwnedResourceState) []OwnedResourceState {
	out := append([]OwnedResourceState(nil), in...)
	for i := range out {
		out[i].Dependents = append([]string(nil), in[i].Dependents...)
	}
	return out
}
