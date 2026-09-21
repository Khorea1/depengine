package source

import (
	"context"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/pkg/config"
	"github.com/Khorea1/depengine/pkg/plan"
	"github.com/Khorea1/depengine/pkg/run"
)

func TestPreparationPlanProjectsMissingSourcesAsOwnedReversibleMutations(t *testing.T) {
	missing := []config.Source{
		{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.invalid/tools.git"},
		{Kind: "scoop-bucket", Name: "extras"},
	}
	got, err := PreparationPlan(missing)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Prepare) != 2 || len(got.Commit) != 1 || got.Commit[0].Kind != "install" {
		t.Fatalf("plan = %#v", got)
	}
	for i, mutation := range got.Prepare {
		wantIdentity, err := ResourceIdentity(missing[i])
		if err != nil {
			t.Fatal(err)
		}
		if mutation.Resource != wantIdentity || mutation.Ownership != plan.OwnershipDepengine || mutation.Policy != plan.RollbackSafe {
			t.Fatalf("mutation %d = %#v", i, mutation)
		}
		if mutation.Rollback == nil || mutation.Rollback.Kind != "remove-source" {
			t.Fatalf("mutation %d rollback = %#v", i, mutation.Rollback)
		}
	}
}

func TestMissingAndPresentAreObservational(t *testing.T) {
	runner := &scriptedRunner{outputs: []run.Result{
		{Stdout: []byte("vendor/existing\n")},
		{Stdout: []byte("vendor/existing\n")},
		{Stdout: []byte("vendor/existing\n")},
	}}
	manager := NewManager(runner, false)
	sources := []config.Source{{Kind: "brew-tap", Name: "vendor/existing"}, {Kind: "brew-tap", Name: "vendor/missing"}}
	missing, err := manager.Missing(context.Background(), sources)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(missing, []config.Source{sources[1]}) {
		t.Fatalf("missing = %#v", missing)
	}
	present, err := manager.Present(context.Background(), sources[0])
	if err != nil || !present {
		t.Fatalf("Present() = %v, %v", present, err)
	}
	for _, call := range runner.calls {
		if call.Name != "brew" || !reflect.DeepEqual(call.Args, []string{"tap"}) {
			t.Fatalf("observational probe mutated host: %#v", call)
		}
	}
}
