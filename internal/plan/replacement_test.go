package plan

import "testing"

func TestReplacementJournalWriteAheadTransitions(t *testing.T) {
	journal := NewReplacementJournal()
	if journal.Phase != ReplacementPlanned {
		t.Fatalf("initial phase = %q", journal.Phase)
	}
	journal, err := journal.PlanRemove()
	if err != nil || journal.Phase != ReplacementRemoving {
		t.Fatalf("PlanRemove() = %+v, %v", journal, err)
	}
	journal, err = journal.RecordRemoved()
	if err != nil || journal.Phase != ReplacementRemoved {
		t.Fatalf("RecordRemoved() = %+v, %v", journal, err)
	}
	journal, err = journal.PlanInstall()
	if err != nil || journal.Phase != ReplacementInstalling {
		t.Fatalf("PlanInstall() = %+v, %v", journal, err)
	}
	journal, err = journal.RecordInstalled()
	if err != nil || journal.Phase != ReplacementInstalled {
		t.Fatalf("RecordInstalled() = %+v, %v", journal, err)
	}
	journal, err = journal.PlanPostHook()
	if err != nil || journal.Phase != ReplacementPostHookRunning {
		t.Fatalf("PlanPostHook() = %+v, %v", journal, err)
	}
	if _, err := journal.RecordRemoved(); err == nil {
		t.Fatal("RecordRemoved() accepted a non-removal phase")
	}
	if _, err := journal.RecordInstalled(); err == nil {
		t.Fatal("RecordInstalled() accepted a phase after the hook boundary")
	}
}

func TestReplacementRecoveryDecisions(t *testing.T) {
	absent := VerificationResult{State: StateAbsent}
	present := VerificationResult{State: StateSatisfied}
	unknown := VerificationResult{State: StateUnknown, Detail: "identity unavailable", Unverifiable: []IdentityField{FieldVersion}}
	broken := VerificationResult{State: StateBroken, Detail: "probe failed"}
	drifted := VerificationResult{State: StateDrifted, Detail: "old version is no longer present", Observed: ObservedIdentity{Version: "2.0.0"}, Drift: []IdentityDrift{{Field: FieldVersion, Desired: "1.0.0", Observed: "2.0.0"}}, KnownFields: []IdentityField{FieldVersion}}
	cases := []struct {
		name, phase  string
		old, desired VerificationResult
		want         ReplacementRecoveryAction
	}{
		{"planned-old-present", string(ReplacementPlanned), present, absent, ReplacementRetryRemoval},
		{"removing-old-present", string(ReplacementRemoving), present, absent, ReplacementRetryRemoval},
		{"removing-both-absent", string(ReplacementRemoving), absent, absent, ReplacementRecordRemoved},
		{"removed-target-absent", string(ReplacementRemoved), absent, absent, ReplacementStartInstall},
		{"removed-target-satisfied", string(ReplacementRemoved), absent, present, ReplacementRecordInstalled},
		{"installing-target-satisfied", string(ReplacementInstalling), absent, present, ReplacementRecordInstalled},
		{"installing-exact-target-replaces-old-version", string(ReplacementInstalling), drifted, present, ReplacementRecordInstalled},
		{"removed-exact-target-is-recorded-before-hook-decision", string(ReplacementRemoved), absent, present, ReplacementRecordInstalled},
		{"removed-exact-target-replaces-old-version", string(ReplacementRemoved), drifted, present, ReplacementRecordInstalled},
		{"installed-target-continues-lifecycle", string(ReplacementInstalled), absent, present, ReplacementContinueInstalled},
		{"installed-target-with-old-drift-continues-lifecycle", string(ReplacementInstalled), drifted, present, ReplacementContinueInstalled},
		{"installed-target-absent-blocks-reinstall", string(ReplacementInstalled), absent, absent, ReplacementBlocked},
		{"post-hook-running-blocks-replay", string(ReplacementPostHookRunning), absent, present, ReplacementBlocked},
		{"post-hook-running-target-absent-blocks", string(ReplacementPostHookRunning), absent, absent, ReplacementBlocked},
		{"installing-target-absent-resumes-exact-target", string(ReplacementInstalling), absent, absent, ReplacementStartInstall},
		{"installing-target-unknown", string(ReplacementInstalling), absent, unknown, ReplacementBlocked},
		{"installing-target-broken", string(ReplacementInstalling), absent, broken, ReplacementBlocked},
		{"removing-old-unknown", string(ReplacementRemoving), unknown, absent, ReplacementBlocked},
		{"planned-old-and-desired-present-is-ambiguous", string(ReplacementPlanned), present, present, ReplacementBlocked},
		{"removed-old-still-present-is-ambiguous", string(ReplacementRemoved), present, absent, ReplacementBlocked},
		{"installing-old-and-desired-present-is-ambiguous", string(ReplacementInstalling), present, present, ReplacementBlocked},
		{"removing-desired-present-is-ambiguous", string(ReplacementRemoving), absent, present, ReplacementBlocked},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ReplacementRecoveryFor(ReplacementPhase(tc.phase), tc.old, tc.desired)
			if err != nil {
				t.Fatalf("ReplacementRecoveryFor() error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("action = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReplacementRecoveryRejectsInvalidPhase(t *testing.T) {
	got, err := ReplacementRecoveryFor("committed", VerificationResult{State: StateAbsent}, VerificationResult{State: StateAbsent})
	if err == nil || got != ReplacementBlocked {
		t.Fatalf("invalid phase result = %q, %v; want blocked and error", got, err)
	}
}

func TestReplacementRecoveryDoesNotFinalizeSatisfiedInstallBeforeHookResolution(t *testing.T) {
	action, err := ReplacementRecoveryFor(
		ReplacementInstalling,
		VerificationResult{State: StateAbsent},
		VerificationResult{State: StateSatisfied},
	)
	if err != nil {
		t.Fatal(err)
	}
	if action != ReplacementRecordInstalled {
		t.Fatalf("satisfied install action = %q; want the installed boundary before resolving the after-upgrade hook", action)
	}
}
