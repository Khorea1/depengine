package ecosystem

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

func TestAsdfAdapterV2ResolvesAndInstallsAsdf(t *testing.T) {
	tool, mc := asdfTool("node", "nodejs")
	mc.Config["version"] = "20.1.0"
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "nodejs"
	intent.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: "20.1.0"}
	intent.Identity.Version = "20.1.0"
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	a := NewAsdfAdapter()
	resolved, err := a.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil || resolved.Identity.Package != "nodejs" || resolved.Identity.Version != "20.1.0" {
		t.Fatalf("ResolvePlan() = %#v, %v", resolved, err)
	}
	runner := &run.FakeRunner{ExitCode: 0}
	if err := a.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatal(err)
	}
	want := []run.FakeCall{{Name: "which", Args: []string{"asdf"}}, {Name: "asdf", Args: []string{"plugin", "list"}}, {Name: "asdf", Args: []string{"plugin-add", "nodejs"}}, {Name: "asdf", Args: []string{"install", "nodejs", "20.1.0"}}, {Name: "asdf", Args: []string{"global", "nodejs", "20.1.0"}}}
	if !reflect.DeepEqual(runner.Calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.Calls, want)
	}
}

func TestAsdfAdapterV2InstallsMise(t *testing.T) {
	tool, mc := asdfTool("node", "nodejs")
	mc.Config["version"] = "20.1.0"
	intent := plan.New(tool.Name, mc.Kind, true)
	intent.Identity.Package = "nodejs"
	intent.Identity.RequestedVersion = &plan.VersionIntent{Mode: plan.VersionExact, Value: "20.1.0"}
	intent.Identity.Version = "20.1.0"
	intent.Operations = []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}}
	a := NewAsdfAdapter()
	resolved, err := a.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatal(err)
	}
	runner := &run.FakeRunner{ExitCode: 0, LookPaths: map[string]bool{"asdf": false, "mise": true}}
	if err := a.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatal(err)
	}
	want := []run.FakeCall{{Name: "which", Args: []string{"asdf"}}, {Name: "which", Args: []string{"mise"}}, {Name: "mise", Args: []string{"install", "nodejs@20.1.0"}}, {Name: "mise", Args: []string{"use", "-g", "nodejs@20.1.0"}}}
	if !reflect.DeepEqual(runner.Calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.Calls, want)
	}
}

func TestAsdfAdapterV2ObserveAbsentAndBackendError(t *testing.T) {
	a := NewAsdfAdapter()
	tool, mc := asdfTool("node", "nodejs")
	obs, err := a.Observe(context.Background(), &run.FakeRunner{ExitCode: 0, Stdout: ""}, tool, mc)
	if err != nil || obs.Presence != plan.PresenceAbsent {
		t.Fatalf("absent observation = %#v, %v", obs, err)
	}
	wantErr := errors.New("backend unavailable")
	obs, err = a.Observe(context.Background(), &run.FakeRunner{ExitCode: 1, Err: wantErr, LookPaths: map[string]bool{"asdf": true}}, tool, mc)
	if err == nil || obs.Presence != plan.PresenceBroken {
		t.Fatalf("error observation = %#v, %v", obs, err)
	}
}

func TestAsdfAdapterV2RejectsOperations(t *testing.T) {
	intent := plan.New("node", "asdf", true)
	intent.Operations = []plan.Operation{{Kind: "arbitrary", Effect: plan.EffectMutation, Command: []string{"sh"}, ArbitraryCode: true}}
	err := NewAsdfAdapter().InstallResolved(context.Background(), &run.FakeRunner{}, &config.Tool{Name: "node"}, nil, &intent)
	if err == nil || err.Error() != "asdf: resolved operations are unsupported" {
		t.Fatalf("error = %v", err)
	}
}
