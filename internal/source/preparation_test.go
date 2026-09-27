package source

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
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

func TestPreparationPlanPersistsCredentialFreeSourceURLForRecovery(t *testing.T) {
	configured := config.Source{
		Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.invalid/vendor/tools.git",
		SecretRef: &config.SecretReference{Provider: "env", Name: "CORP_TOKEN"},
	}
	preparation, err := PreparationPlan([]config.Source{configured})
	if err != nil {
		t.Fatal(err)
	}
	if len(preparation.Prepare) != 1 {
		t.Fatalf("prepare = %#v", preparation.Prepare)
	}
	mutation := preparation.Prepare[0]
	if strings.Contains(mutation.Apply.Description, "CORP_TOKEN") {
		t.Fatalf("preparation descriptor leaked secret reference: %q", mutation.Apply.Description)
	}
	recovered, err := SourceFromPreparationMutation(mutation)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Kind != configured.Kind || recovered.Name != configured.Name || recovered.URL != configured.URL || recovered.SecretRef != nil {
		t.Fatalf("recovered = %#v, want kind/name/url without secret reference", recovered)
	}
}

func TestSourceFromPreparationMutationReadsLegacyIdentityOnlyJournal(t *testing.T) {
	configured := config.Source{Kind: "brew-tap", Name: "vendor/tools"}
	identity, err := ResourceIdentity(configured)
	if err != nil {
		t.Fatal(err)
	}
	mutation := plan.PreparationMutation{
		Resource: identity,
		Apply: plan.Operation{
			Kind:        "add-source",
			Description: identity.Key,
			Effect:      plan.EffectMutation,
		},
	}
	recovered, err := SourceFromPreparationMutation(mutation)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Kind != "brew-tap" || recovered.Name != "vendor/tools" || recovered.URL != "" {
		t.Fatalf("legacy recovered source = %#v", recovered)
	}
}

func TestSourceFromPreparationMutationRejectsNonCanonicalDescriptor(t *testing.T) {
	configured := config.Source{Kind: "brew-tap", Name: "vendor/tools", URL: "https://example.test/vendor/tools.git"}
	identity, err := ResourceIdentity(configured)
	if err != nil {
		t.Fatal(err)
	}
	mutation := plan.PreparationMutation{
		Resource: identity,
		Apply: plan.Operation{
			Kind:        "add-source",
			Description: "source/v1?name=vendor%2Ftools&kind=brew-tap&url=https%3A%2F%2Fexample.test%2Fvendor%2Ftools.git&extra=ignored",
			Effect:      plan.EffectMutation,
		},
	}
	if _, err := SourceFromPreparationMutation(mutation); err == nil {
		t.Fatal("SourceFromPreparationMutation() accepted non-canonical descriptor")
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
