package source

import (
	"context"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func ownedSourceFixture(t *testing.T, ownership plan.OwnershipKind, dependents ...string) plan.OwnedResourceState {
	t.Helper()
	identity, err := ResourceIdentity(config.Source{Kind: "brew-tap", Name: "vendor/tools"})
	if err != nil {
		t.Fatal(err)
	}
	return plan.OwnedResourceState{Resource: identity, Ownership: ownership, Dependents: dependents}
}

func TestCleanupReleasedSourcesWaitsForLastDependent(t *testing.T) {
	owned := []plan.OwnedResourceState{ownedSourceFixture(t, plan.OwnershipDepengine, "tool-a", "tool-b")}
	release, err := plan.ReleaseDependentResources(owned, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	runner := &run.FakeRunner{ExitCode: 0}
	manager := NewManager(runner, false)
	next, err := manager.CleanupReleasedSources(context.Background(), release)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("source cleanup ran before last dependent: %#v", runner.Calls)
	}
	if len(next) != 1 || !reflect.DeepEqual(next[0].Dependents, []string{"tool-b"}) {
		t.Fatalf("ownership = %#v, want tool-b claim", next)
	}
}

func TestCleanupReleasedSourcesRemovesLastOwnedSource(t *testing.T) {
	owned := []plan.OwnedResourceState{ownedSourceFixture(t, plan.OwnershipDepengine, "tool-a")}
	release, err := plan.ReleaseDependentResources(owned, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	runner := &scriptedRunner{outputs: []run.Result{
		{Stdout: []byte("vendor/tools\n")},
		{},
	}}
	manager := NewManager(runner, false)
	next, err := manager.CleanupReleasedSources(context.Background(), release)
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 0 {
		t.Fatalf("ownership = %#v, want source record finalized", next)
	}
	if len(runner.calls) != 2 || runner.calls[1].Name != "brew" || !reflect.DeepEqual(runner.calls[1].Args, []string{"untap", "vendor/tools"}) {
		t.Fatalf("calls = %#v, want brew tap probe then untap", runner.calls)
	}
}

func TestCleanupReleasedSourcesNeverRemovesExternalSource(t *testing.T) {
	owned := []plan.OwnedResourceState{ownedSourceFixture(t, plan.OwnershipExternal, "tool-a")}
	release, err := plan.ReleaseDependentResources(owned, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	runner := &run.FakeRunner{ExitCode: 0}
	next, err := NewManager(runner, false).CleanupReleasedSources(context.Background(), release)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("external source cleanup calls = %#v", runner.Calls)
	}
	if len(next) != 1 || next[0].Ownership != plan.OwnershipExternal || len(next[0].Dependents) != 0 {
		t.Fatalf("ownership = %#v, want zero-ref external state retained", next)
	}
}

func TestCleanupReleasedSourcesRetainsOwnershipWhenHostCleanupFails(t *testing.T) {
	owned := []plan.OwnedResourceState{ownedSourceFixture(t, plan.OwnershipDepengine, "tool-a")}
	release, err := plan.ReleaseDependentResources(owned, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	runner := &scriptedRunner{outputs: []run.Result{
		{Stdout: []byte("vendor/tools\n")},
		{ExitCode: 1},
	}}
	next, err := NewManager(runner, false).CleanupReleasedSources(context.Background(), release)
	if err == nil {
		t.Fatal("cleanup unexpectedly succeeded")
	}
	if !reflect.DeepEqual(next, release.Updated) {
		t.Fatalf("ownership after failure = %#v, want %#v", next, release.Updated)
	}
	if len(next) != 1 || next[0].Ownership != plan.OwnershipDepengine || len(next[0].Dependents) != 0 {
		t.Fatalf("failed cleanup must retain explicit zero-ref owned source: %#v", next)
	}
}

func TestCleanupReleasedSourcesLeavesOtherResourceKindsExplicit(t *testing.T) {
	resource := plan.OwnedResourceState{
		Resource:   plan.ResourceIdentity{Kind: plan.ResourcePrerequisite, Key: "helper"},
		Ownership:  plan.OwnershipDepengine,
		Dependents: []string{"tool-a"},
	}
	release, err := plan.ReleaseDependentResources([]plan.OwnedResourceState{resource}, "tool-a")
	if err != nil {
		t.Fatal(err)
	}
	runner := &run.FakeRunner{ExitCode: 0}
	next, err := NewManager(runner, false).CleanupReleasedSources(context.Background(), release)
	if err != nil {
		t.Fatal(err)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("unexpected cleanup calls: %#v", runner.Calls)
	}
	if !reflect.DeepEqual(next, release.Updated) || len(next) != 1 || len(next[0].Dependents) != 0 {
		t.Fatalf("non-source state = %#v, want explicit zero-ref retention", next)
	}
}
