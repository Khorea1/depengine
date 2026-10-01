package state

import (
	"bytes"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Khorea1/depengine/internal/plan"
)

func replacementProjection(t *testing.T) plan.LockProjection {
	t.Helper()
	resolved := plan.New("demo", "http", true)
	resolved.Identity.Version = "2.0.0"
	projection, err := plan.ProjectLock(resolved)
	if err != nil {
		t.Fatal(err)
	}
	return projection
}

func beginReplacementForTest(ls *LockedState, toolName, methodKind string, candidate plan.CandidateIdentity, previous ToolState, desired plan.LockProjection, preparationKey string) error {
	selected := ReplacementCandidate{Identity: candidate}
	return ls.BeginReplacement(toolName, methodKind, selected, selected, previous, desired, preparationKey, nil)
}

func TestReplacementTransactionPersistsEveryWriteAheadBoundary(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	previous := ToolState{Method: "http", MethodKind: "http", Version: "1.0.0", Config: map[string]any{"url": "https://example.test/old"}}
	ls.State().Tools["demo"] = previous
	desired := replacementProjection(t)
	candidate := desired.Candidate
	if err := beginReplacementForTest(ls, "demo", "http", candidate, previous, desired, ""); err != nil {
		t.Fatal(err)
	}
	checkPhase := func(want plan.ReplacementPhase) {
		t.Helper()
		loaded, err := LoadFrom(DefaultPath())
		if err != nil {
			t.Fatal(err)
		}
		tx, ok := loaded.ReplacementTransactions["demo"]
		if !ok || tx.Journal.Phase != want {
			t.Fatalf("persisted transaction = %+v, present=%t; want phase %q", tx, ok, want)
		}
		if tx.PreviousCandidate != candidate || tx.Candidate != candidate {
			t.Fatalf("persisted old/new candidates = %+v/%+v, want %+v", tx.PreviousCandidate, tx.Candidate, candidate)
		}
	}
	checkPhase(plan.ReplacementPlanned)
	if err := ls.PlanReplacementRemoval("demo"); err != nil {
		t.Fatal(err)
	}
	checkPhase(plan.ReplacementRemoving)
	if err := ls.RecordReplacementRemoved("demo"); err != nil {
		t.Fatal(err)
	}
	checkPhase(plan.ReplacementRemoved)
	if err := ls.PlanReplacementInstall("demo"); err != nil {
		t.Fatal(err)
	}
	checkPhase(plan.ReplacementInstalling)

	next := ToolState{Method: "http", MethodKind: "http", Version: "2.0.0", Config: map[string]any{"url": "https://example.test/new"}}
	owned := []plan.OwnedResourceState{}
	if err := ls.CommitReplacementInstallWithPreparation("demo", next, owned, "", plan.PreparationPlan{}, "demo"); err != nil {
		t.Fatal(err)
	}
	installed, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if tx := installed.ReplacementTransactions["demo"]; tx.Journal.Phase != plan.ReplacementInstalled {
		t.Fatalf("verified install phase = %q, want %q", tx.Journal.Phase, plan.ReplacementInstalled)
	}
	if got := installed.Tools["demo"]; !reflect.DeepEqual(got, next) {
		t.Fatalf("committed install state = %+v, want %+v", got, next)
	}
	if err := ls.CompleteReplacement("demo", next); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded.ReplacementTransactions["demo"]; ok {
		t.Fatal("successful replacement retained active transaction")
	}
	if got := loaded.Tools["demo"]; !reflect.DeepEqual(got, next) {
		t.Fatalf("final tool state = %+v, want %+v", got, next)
	}
}

func TestReplacementAndPreparationFinalizeAtomically(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	previous := ToolState{Method: "http", MethodKind: "http", Version: "1.0.0", Config: map[string]any{}}
	ls.State().Tools["demo"] = previous
	desired := replacementProjection(t)
	prep := preparationTransactionPlan("repo:stable")
	const prepKey = "candidate/v1/demo/http/atomic"
	if _, err := ls.BeginPreparation(prepKey, prep); err != nil {
		t.Fatal(err)
	}
	persistPreparationMutation(t, ls, prepKey, prep, "source-add")
	if _, err := ls.PlanPreparationCommitWithUses(prepKey, prep, []plan.ResourceUse{{
		Resource: plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable"}, Created: true,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := beginReplacementForTest(ls, "demo", "http", desired.Candidate, previous, desired, prepKey); err != nil {
		t.Fatal(err)
	}
	if err := ls.PlanReplacementRemoval("demo"); err != nil {
		t.Fatal(err)
	}
	if err := ls.RecordReplacementRemoved("demo"); err != nil {
		t.Fatal(err)
	}
	if err := ls.PlanReplacementInstall("demo"); err != nil {
		t.Fatal(err)
	}
	next := ToolState{Method: "http", MethodKind: "http", Version: "2.0.0", PostinstallDone: true, Config: map[string]any{}}
	storedPrep, _, err := ls.PreparationTransaction(prepKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := ls.CommitReplacementInstallWithPreparation("demo", next, nil, prepKey, storedPrep, "demo"); err != nil {
		t.Fatal(err)
	}
	committed, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if tx := committed.ReplacementTransactions["demo"]; tx.Journal.Phase != plan.ReplacementInstalled {
		t.Fatalf("replacement phase = %q, want installed", tx.Journal.Phase)
	}
	if _, ok := committed.PreparationJournals[prepKey]; ok {
		t.Fatal("preparation WAL remained after verified install commit")
	}
	if err := ls.CompleteReplacement("demo", next); err != nil {
		t.Fatal(err)
	}

	persisted, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := persisted.ReplacementTransactions["demo"]; ok {
		t.Fatal("replacement WAL remained after atomic finalization")
	}
	if _, ok := persisted.PreparationJournals[prepKey]; ok {
		t.Fatal("preparation WAL remained after atomic finalization")
	}
	if _, ok := persisted.PreparationPlans[prepKey]; ok {
		t.Fatal("preparation plan remained after atomic finalization")
	}
	if got := persisted.Tools["demo"]; !reflect.DeepEqual(got, next) {
		t.Fatalf("tool state = %+v, want %+v", got, next)
	}
	if len(persisted.OwnedResources) != 1 || persisted.OwnedResources[0].Resource.Key != "repo:stable" {
		t.Fatalf("owned resources = %+v, want prepared source ownership", persisted.OwnedResources)
	}
}

func TestReplacementTransactionRejectsDuplicateAndIllegalTransitions(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()
	previous := ToolState{Method: "http", MethodKind: "http", Version: "1.0.0", Config: map[string]any{}}
	ls.State().Tools["demo"] = previous
	desired := replacementProjection(t)
	if err := beginReplacementForTest(ls, "demo", "http", desired.Candidate, previous, desired, ""); err != nil {
		t.Fatal(err)
	}
	if err := beginReplacementForTest(ls, "demo", "http", desired.Candidate, previous, desired, ""); err == nil {
		t.Fatal("duplicate replacement transaction was accepted")
	}
	if err := ls.PlanReplacementInstall("demo"); err == nil {
		t.Fatal("install boundary before removal was accepted")
	}
}

func TestReplacementTransactionRejectsSecretAndChecksumTampering(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()
	previous := ToolState{Method: "http", MethodKind: "http", Version: "1.0.0", Config: map[string]any{"token": "secret"}}
	ls.State().Tools["demo"] = previous
	desired := replacementProjection(t)
	if err := beginReplacementForTest(ls, "demo", "http", desired.Candidate, previous, desired, ""); err == nil {
		t.Fatal("replacement WAL persisted a credential-bearing previous state")
	}
	if _, exists := ls.State().ReplacementTransactions["demo"]; exists {
		t.Fatal("failed replacement begin left an in-memory transaction")
	}

	previous.Config = map[string]any{"url": "https://example.test/old"}
	ls.State().Tools["demo"] = previous
	if err := beginReplacementForTest(ls, "demo", "http", desired.Candidate, previous, desired, ""); err != nil {
		t.Fatal(err)
	}
	path := DefaultPath()
	// #nosec G304 -- path is inside the test-owned XDG state directory.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte("removal_planned"), []byte("installing____"), 1)
	// #nosec G703 -- path is inside the test-owned XDG state directory.
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFrom(path); err == nil {
		t.Fatal("LoadFrom accepted replacement WAL checksum tampering")
	}
}

func TestReplacementWALSerializesConcurrentStateWriters(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	first, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	previous := ToolState{Method: "http", MethodKind: "http", Version: "1.0.0", Config: map[string]any{}}
	first.State().Tools["demo"] = previous
	desired := replacementProjection(t)
	started := make(chan struct{})
	result := make(chan *LockedState, 1)
	go func() {
		close(started)
		second, err := LoadLocked()
		if err != nil {
			result <- nil
			return
		}
		result <- second
	}()
	<-started
	select {
	case <-result:
		_ = first.Close()
		t.Fatal("second state writer acquired the exclusive lock concurrently")
	case <-time.After(50 * time.Millisecond):
	}
	if err := beginReplacementForTest(first, "demo", "http", desired.Candidate, previous, desired, ""); err != nil {
		_ = first.Close()
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second := <-result
	if second == nil {
		t.Fatal("second writer failed after lock release")
	}
	defer func() { _ = second.Close() }()
	if _, err := second.ReplacementTransaction("demo"); err != nil {
		t.Fatalf("second writer did not observe durable replacement: %v", err)
	}
}

func TestReplacementSaveFailuresPreserveDurableBoundary(t *testing.T) {
	cases := []struct {
		name, from string
		advance    func(*LockedState) error
		wantPhase  plan.ReplacementPhase
	}{
		{"removal-boundary", string(plan.ReplacementPlanned), func(ls *LockedState) error { return ls.PlanReplacementRemoval("demo") }, plan.ReplacementPlanned},
		{"removed-state", "removing", func(ls *LockedState) error { return ls.RecordReplacementRemoved("demo") }, plan.ReplacementRemoving},
		{"install-boundary", "removed", func(ls *LockedState) error { return ls.PlanReplacementInstall("demo") }, plan.ReplacementRemoved},
		{"installed-state-resource-commit", "installing", func(ls *LockedState) error {
			return ls.CommitReplacementInstallWithPreparation("demo", ToolState{Method: "http", MethodKind: "http", Version: "2.0.0", Config: map[string]any{}}, nil, "", plan.PreparationPlan{}, "demo")
		}, plan.ReplacementInstalling},
		{"post-hook-boundary", "installed", func(ls *LockedState) error { return ls.PlanReplacementPostHook("demo") }, plan.ReplacementInstalled},
		{"wal-completion", string(plan.ReplacementPostHookRunning), func(ls *LockedState) error {
			return ls.CompleteReplacement("demo", ToolState{Method: "http", MethodKind: "http", Version: "2.0.0", Config: map[string]any{}})
		}, plan.ReplacementPostHookRunning},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			ls := replacementAtPhase(t, plan.ReplacementPhase(tc.from))
			defer func() { _ = ls.Close() }()
			restore := blockStateSave(t)
			if err := tc.advance(ls); err == nil {
				restore()
				t.Fatal("replacement transition succeeded despite an unwriteable state destination")
			}
			restore()
			persisted, err := LoadFrom(DefaultPath())
			if err != nil {
				t.Fatal(err)
			}
			tx, ok := persisted.ReplacementTransactions["demo"]
			if !ok || tx.Journal.Phase != tc.wantPhase {
				t.Fatalf("durable phase = %q, present=%t; want %q", tx.Journal.Phase, ok, tc.wantPhase)
			}
		})
	}
}

func TestBeginReplacementSaveFailureLeavesNoDurableWAL(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()
	previous := ToolState{Method: "http", MethodKind: "http", Version: "1.0.0", Config: map[string]any{}}
	ls.State().Tools["demo"] = previous
	if err := ls.Save(); err != nil {
		t.Fatal(err)
	}
	desired := replacementProjection(t)
	candidate := ReplacementCandidate{Identity: desired.Candidate}
	restore := blockStateSave(t)
	err = ls.BeginReplacement("demo", "http", candidate, candidate, previous, desired, "", nil)
	if err == nil {
		restore()
		t.Fatal("BeginReplacement succeeded despite an unwriteable state destination")
	}
	restore()
	persisted, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := persisted.ReplacementTransactions["demo"]; ok {
		t.Fatal("failed BeginReplacement left a durable WAL")
	}
	if got := persisted.Tools["demo"]; !reflect.DeepEqual(got, previous) {
		t.Fatalf("tracked state after failed BeginReplacement = %+v, want %+v", got, previous)
	}
}

func replacementAtPhase(t *testing.T, phase plan.ReplacementPhase) *LockedState {
	t.Helper()
	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	previous := ToolState{Method: "http", MethodKind: "http", Version: "1.0.0", Config: map[string]any{}}
	ls.State().Tools["demo"] = previous
	desired := replacementProjection(t)
	candidate := ReplacementCandidate{Identity: desired.Candidate}
	if err := ls.BeginReplacement("demo", "http", candidate, candidate, previous, desired, "", nil); err != nil {
		_ = ls.Close()
		t.Fatal(err)
	}
	if phase == plan.ReplacementPlanned {
		return ls
	}
	if err := ls.PlanReplacementRemoval("demo"); err != nil {
		_ = ls.Close()
		t.Fatal(err)
	}
	if phase == plan.ReplacementRemoving {
		return ls
	}
	if err := ls.RecordReplacementRemoved("demo"); err != nil {
		_ = ls.Close()
		t.Fatal(err)
	}
	if phase == plan.ReplacementRemoved {
		return ls
	}
	if err := ls.PlanReplacementInstall("demo"); err != nil {
		_ = ls.Close()
		t.Fatal(err)
	}
	if phase == plan.ReplacementInstalling {
		return ls
	}
	installed := ToolState{Method: "http", MethodKind: "http", Version: "2.0.0", Config: map[string]any{}}
	if err := ls.CommitReplacementInstallWithPreparation("demo", installed, nil, "", plan.PreparationPlan{}, "demo"); err != nil {
		_ = ls.Close()
		t.Fatal(err)
	}
	if phase == plan.ReplacementInstalled {
		return ls
	}
	if err := ls.PlanReplacementPostHook("demo"); err != nil {
		_ = ls.Close()
		t.Fatal(err)
	}
	if phase == plan.ReplacementPostHookRunning {
		return ls
	}
	_ = ls.Close()
	t.Fatalf("unsupported fixture phase %q", phase)
	return nil
}

func blockStateSave(t *testing.T) func() {
	t.Helper()
	path := DefaultPath()
	original, err := os.ReadFile(path) // #nosec G304 -- callers set XDG_STATE_HOME to t.TempDir, so this fixture path is isolated and test-controlled.
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	return func() {
		t.Helper()
		if err := os.RemoveAll(path); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, original, 0600); err != nil { // #nosec G703 -- path is derived from the isolated t.TempDir-backed state fixture.
			t.Fatal(err)
		}
	}
}

func TestReplacementTransactionPersistsCandidateLabelsAndResourceUses(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()

	previous := ToolState{Method: "http", MethodKind: "http", Version: "1.0.0", Config: map[string]any{"url": "https://example.test/old"}}
	ls.State().Tools["demo"] = previous
	desired := replacementProjection(t)
	uses := []plan.ResourceUse{
		{Resource: plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable"}, Created: true},
		{Resource: plan.ResourceIdentity{Kind: plan.ResourcePrerequisite, Key: "compiler:go"}},
	}
	if err := ls.BeginReplacement(
		"demo", "http",
		ReplacementCandidate{Identity: plan.CandidateIdentity{Method: "http", Explicit: true}, Label: "old-source"},
		ReplacementCandidate{Identity: desired.Candidate, Label: "new-source"},
		previous, desired, "", uses,
	); err != nil {
		t.Fatal(err)
	}

	uses[0].Resource.Key = "mutated-after-save"
	tx, err := ls.ReplacementTransaction("demo")
	if err != nil {
		t.Fatal(err)
	}
	if tx.PreviousCandidateLabel != "old-source" || tx.CandidateLabel != "new-source" {
		t.Fatalf("candidate labels = %q/%q, want old-source/new-source", tx.PreviousCandidateLabel, tx.CandidateLabel)
	}
	want := []plan.ResourceUse{
		{Resource: plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable"}, Created: true},
		{Resource: plan.ResourceIdentity{Kind: plan.ResourcePrerequisite, Key: "compiler:go"}},
	}
	if !reflect.DeepEqual(tx.ResourceUses, want) {
		t.Fatalf("transaction resource uses = %+v, want %+v", tx.ResourceUses, want)
	}
	tx.ResourceUses[0].Resource.Key = "mutated-return-value"
	loaded, err := LoadFrom(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	persisted := loaded.ReplacementTransactions["demo"]
	if persisted.PreviousCandidateLabel != "old-source" || persisted.CandidateLabel != "new-source" || !reflect.DeepEqual(persisted.ResourceUses, want) {
		t.Fatalf("reloaded replacement transaction = %+v", persisted)
	}
}

func TestReplacementTransactionRejectsDuplicateResourceUsesBeforePersistence(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ls, err := LoadLocked()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ls.Close() }()
	previous := ToolState{Method: "http", MethodKind: "http", Version: "1.0.0", Config: map[string]any{}}
	ls.State().Tools["demo"] = previous
	desired := replacementProjection(t)
	use := plan.ResourceUse{Resource: plan.ResourceIdentity{Kind: plan.ResourceSource, Key: "repo:stable"}}
	err = ls.BeginReplacement(
		"demo", "http",
		ReplacementCandidate{Identity: desired.Candidate},
		ReplacementCandidate{Identity: desired.Candidate, Label: "http-new"},
		previous, desired, "", []plan.ResourceUse{use, use},
	)
	if err == nil {
		t.Fatal("duplicate resource uses were accepted")
	}
	if _, ok := ls.State().ReplacementTransactions["demo"]; ok {
		t.Fatal("invalid resource ownership left a replacement WAL")
	}
}
