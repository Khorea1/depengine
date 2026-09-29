package ecosystem

import (
	"context"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

const nixFlags = "--extra-experimental-features nix-command flakes "

// Nix >= 2.20 layout: elements keyed by name.
const nixListObject = `{"version":3,"elements":{
  "hello":{"active":true,"attrPath":"legacyPackages.x86_64-linux.hello","originalUrl":"flake:nixpkgs","url":"github:NixOS/nixpkgs/abc"},
  "requests":{"active":true,"attrPath":"legacyPackages.x86_64-linux.python3Packages.requests","originalUrl":"flake:nixpkgs","url":"github:NixOS/nixpkgs/abc"},
  "legacy":{"storePaths":["/nix/store/x-legacy"]}
}}`

// Older layout: elements as an array, removed by index.
const nixListArray = `{"version":2,"elements":[
  {"active":true,"attrPath":"legacyPackages.x86_64-linux.ripgrep","originalUrl":"flake:nixpkgs","url":"github:NixOS/nixpkgs/abc"},
  {"active":true,"attrPath":"packages.x86_64-linux.hello","originalUrl":"github:example/flake","url":"github:example/flake/def"}
]}`

// nixRunner answers `nix profile list --json` with a fixed document and
// records every call. Everything else exits 0 with no output.
type nixRunner struct {
	*run.FakeRunner
	list     string
	listExit int
	calls    []string
}

func newNixRunner(list string) *nixRunner {
	return &nixRunner{FakeRunner: &run.FakeRunner{LookPaths: map[string]bool{"nix": true}}, list: list}
}

func (r *nixRunner) Run(ctx context.Context, name string, args ...string) run.Result {
	call := strings.Join(append([]string{name}, args...), " ")
	r.calls = append(r.calls, call)
	if strings.HasSuffix(call, "profile list --json") {
		return run.Result{Stdout: []byte(r.list), ExitCode: r.listExit}
	}
	return run.Result{}
}

func (r *nixRunner) mutations() []string {
	var out []string
	for _, c := range r.calls {
		if !strings.HasSuffix(c, "profile list --json") {
			out = append(out, c)
		}
	}
	return out
}

func nixMethod(cfg map[string]any) (*config.Tool, *config.MethodCandidate) {
	return &config.Tool{Name: "demo"}, &config.MethodCandidate{Kind: "nix", Config: cfg}
}

func TestNixParseProfileListBothLayouts(t *testing.T) {
	obj, err := parseNixProfileList([]byte(nixListObject))
	if err != nil || len(obj) != 3 {
		t.Fatalf("object layout: %v, %d elements", err, len(obj))
	}
	if obj[0].Ref != "hello" || obj[1].Ref != "legacy" || obj[2].Ref != "requests" {
		t.Fatalf("object refs must be sorted element names, got %q %q %q", obj[0].Ref, obj[1].Ref, obj[2].Ref)
	}
	arr, err := parseNixProfileList([]byte(nixListArray))
	if err != nil || len(arr) != 2 || arr[0].Ref != "0" || arr[1].Ref != "1" {
		t.Fatalf("array layout: %v, %+v", err, arr)
	}
	if got, err := parseNixProfileList([]byte(`{"version":3,"elements":{}}`)); err != nil || len(got) != 0 {
		t.Fatalf("empty profile: %v, %v", err, got)
	}
	if _, err := parseNixProfileList([]byte(`not json`)); err == nil {
		t.Fatal("malformed JSON must be an error")
	}
}

func TestNixObserveMatchesSourceAndAttribute(t *testing.T) {
	cases := []struct {
		name string
		list string
		cfg  map[string]any
		want plan.PresenceState
	}{
		{"default source present", nixListObject, map[string]any{"pkg": "hello"}, plan.PresencePresent},
		{"dotted attribute present", nixListObject, map[string]any{"pkg": "python3Packages.requests"}, plan.PresencePresent},
		{"attribute suffix is not a match", nixListObject, map[string]any{"pkg": "requests"}, plan.PresenceAbsent},
		{"different source is absent", nixListObject, map[string]any{"pkg": "hello", "source": "github:example/flake"}, plan.PresenceAbsent},
		{"flake: prefix is equivalent", nixListObject, map[string]any{"pkg": "hello", "source": "flake:nixpkgs"}, plan.PresencePresent},
		{"non-flake element never matches", nixListObject, map[string]any{"pkg": "legacy"}, plan.PresenceAbsent},
		{"array layout with explicit source", nixListArray, map[string]any{"pkg": "hello", "source": "github:example/flake"}, plan.PresencePresent},
		{"array layout wrong source", nixListArray, map[string]any{"pkg": "hello"}, plan.PresenceAbsent},
		{"fully qualified attribute present", nixListObject, map[string]any{"pkg": "legacyPackages.x86_64-linux.hello"}, plan.PresencePresent},
		{"inactive element is absent", `{"elements":{"hello":{"active":false,"attrPath":"legacyPackages.x86_64-linux.hello","originalUrl":"flake:nixpkgs"}}}`, map[string]any{"pkg": "hello"}, plan.PresenceAbsent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tl, mc := nixMethod(tc.cfg)
			got, err := NewNixAdapter().Observe(context.Background(), newNixRunner(tc.list), tl, mc)
			if err != nil {
				t.Fatalf("Observe() error = %v", err)
			}
			if got.Presence != tc.want {
				t.Fatalf("Observe() presence = %s, want %s", got.Presence, tc.want)
			}
		})
	}
}

func TestNixObserveAmbiguousIsBroken(t *testing.T) {
	list := `{"elements":{"a":{"active":true,"attrPath":"legacyPackages.x86_64-linux.hello","originalUrl":"flake:nixpkgs"},"b":{"active":true,"attrPath":"legacyPackages.aarch64-linux.hello","originalUrl":"flake:nixpkgs"}}}`
	tl, mc := nixMethod(map[string]any{"pkg": "hello"})
	got, err := NewNixAdapter().Observe(context.Background(), newNixRunner(list), tl, mc)
	if err != nil || got.Presence != plan.PresenceBroken {
		t.Fatalf("Observe() = %+v, %v; want broken", got, err)
	}
}

func TestNixRemoveUsesObservedElementReference(t *testing.T) {
	cases := []struct {
		name string
		list string
		cfg  map[string]any
		want string
	}{
		{"element name", nixListObject, map[string]any{"pkg": "python3Packages.requests"}, nixFlags + "profile remove requests"},
		{"array index", nixListArray, map[string]any{"pkg": "hello", "source": "github:example/flake"}, nixFlags + "profile remove 1"},
		{"inactive element", `{"elements":{"hello":{"active":false,"attrPath":"legacyPackages.x86_64-linux.hello","originalUrl":"flake:nixpkgs"}}}`, map[string]any{"pkg": "hello"}, nixFlags + "profile remove hello"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rn := newNixRunner(tc.list)
			tl, mc := nixMethod(tc.cfg)
			if err := NewNixAdapter().Remove(context.Background(), rn, tl, mc); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
			got := rn.mutations()
			if len(got) != 1 || got[0] != "nix "+tc.want {
				t.Fatalf("mutations = %q, want [nix %s]", got, tc.want)
			}
		})
	}
}

func TestNixRemoveAbsentIsNoOpAndAmbiguousIsRefused(t *testing.T) {
	rn := newNixRunner(nixListObject)
	tl, mc := nixMethod(map[string]any{"pkg": "ripgrep"})
	if err := NewNixAdapter().Remove(context.Background(), rn, tl, mc); err != nil || len(rn.mutations()) != 0 {
		t.Fatalf("absent target: err=%v mutations=%q, want no-op", err, rn.mutations())
	}

	list := `{"elements":{"a":{"active":true,"attrPath":"legacyPackages.x86_64-linux.hello","originalUrl":"flake:nixpkgs"},"b":{"active":true,"attrPath":"legacyPackages.aarch64-linux.hello","originalUrl":"flake:nixpkgs"}}}`
	rn = newNixRunner(list)
	tl, mc = nixMethod(map[string]any{"pkg": "hello"})
	if err := NewNixAdapter().Remove(context.Background(), rn, tl, mc); err == nil || len(rn.mutations()) != 0 {
		t.Fatalf("ambiguous target: err=%v mutations=%q, want refusal without mutation", err, rn.mutations())
	}
}

func TestNixRemoveRefusesFlagLikeReference(t *testing.T) {
	list := `{"elements":{"--impure":{"active":true,"attrPath":"legacyPackages.x86_64-linux.hello","originalUrl":"flake:nixpkgs"}}}`
	rn := newNixRunner(list)
	tl, mc := nixMethod(map[string]any{"pkg": "hello"})
	if err := NewNixAdapter().Remove(context.Background(), rn, tl, mc); err == nil || len(rn.mutations()) != 0 {
		t.Fatalf("err=%v mutations=%q, want refusal", err, rn.mutations())
	}
}

func TestNixListFailureIsSurfacedNotTreatedAsAbsent(t *testing.T) {
	rn := newNixRunner(nixListObject)
	rn.listExit = 1
	tl, mc := nixMethod(map[string]any{"pkg": "hello"})
	if _, err := NewNixAdapter().Observe(context.Background(), rn, tl, mc); err == nil {
		t.Fatal("Observe() must surface a failing `nix profile list`")
	}
	if err := NewNixAdapter().Remove(context.Background(), rn, tl, mc); err == nil {
		t.Fatal("Remove() must surface a failing `nix profile list`")
	}
	if NewNixAdapter().Check(context.Background(), rn, tl, mc) {
		t.Fatal("Check() must be false when the profile cannot be listed")
	}
}

func TestNixInstallResolvedBuildsInstallable(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"default source", map[string]any{"pkg": "hello"}, "nix " + nixFlags + "profile install nixpkgs#hello"},
		{"explicit flake", map[string]any{"pkg": "hello", "source": "github:NixOS/nixpkgs/nixos-24.05"}, "nix " + nixFlags + "profile install github:NixOS/nixpkgs/nixos-24.05#hello"},
		{"dotted attribute", map[string]any{"pkg": "python3Packages.requests"}, "nix " + nixFlags + "profile install nixpkgs#python3Packages.requests"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tl, mc := nixMethod(tc.cfg)
			a := NewNixAdapter()
			rn := newNixRunner(nixListObject)
			intent := plan.ResolvedInstallPlan{
				Identity:   plan.ResolvedIdentity{Package: tc.cfg["pkg"].(string)},
				Operations: []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}},
			}
			if s, ok := tc.cfg["source"].(string); ok {
				intent.Identity.Source = s
			}
			resolved, err := a.ResolvePlan(context.Background(), rn, tl, mc, &intent)
			if err != nil {
				t.Fatalf("ResolvePlan() error = %v", err)
			}
			if err := a.InstallResolved(context.Background(), rn, tl, mc, resolved); err != nil {
				t.Fatalf("InstallResolved() error = %v", err)
			}
			if got := rn.mutations(); len(got) != 1 || got[0] != tc.want {
				t.Fatalf("mutations = %q, want [%s]", got, tc.want)
			}
		})
	}
}

func TestNixRejectsUnsafeIdentityBeforeRunning(t *testing.T) {
	bad := []map[string]any{
		{"pkg": "--impure"},
		{"pkg": "hello#other"},
		{"pkg": "hello world"},
		{"pkg": `"quoted"`},
		{"pkg": "hello", "source": "--flake"},
		{"pkg": "hello", "source": "nixpkgs#evil"},
		{"pkg": "hello", "source": "nix pkgs"},
	}
	for _, cfg := range bad {
		tl, mc := nixMethod(cfg)
		rn := newNixRunner(nixListObject)
		a := NewNixAdapter()
		if _, err := a.Observe(context.Background(), rn, tl, mc); err == nil {
			t.Errorf("Observe(%v) must reject", cfg)
		}
		if err := a.Remove(context.Background(), rn, tl, mc); err == nil {
			t.Errorf("Remove(%v) must reject", cfg)
		}
		intent := plan.ResolvedInstallPlan{Identity: plan.ResolvedIdentity{Package: cfg["pkg"].(string)}}
		if _, err := a.ResolvePlan(context.Background(), rn, tl, mc, &intent); err == nil {
			t.Errorf("ResolvePlan(%v) must reject", cfg)
		}
		if len(rn.calls) != 0 {
			t.Errorf("no command may run for %v, got %q", cfg, rn.calls)
		}
	}
}

func TestNixRequiresBinary(t *testing.T) {
	rn := newNixRunner(nixListObject)
	rn.LookPaths["nix"] = false
	tl, mc := nixMethod(map[string]any{"pkg": "hello"})
	a := NewNixAdapter()
	if a.Available(context.Background(), rn) {
		t.Fatal("Available() must be false without nix")
	}
	if _, err := a.Observe(context.Background(), rn, tl, mc); err == nil {
		t.Fatal("Observe() must fail without nix")
	}
}

func TestNixInstallFailureIsSurfaced(t *testing.T) {
	rn := &failingInstallNixRunner{nixRunner: newNixRunner(nixListObject)}
	tl, mc := nixMethod(map[string]any{"pkg": "hello"})
	intent := plan.ResolvedInstallPlan{
		Identity:   plan.ResolvedIdentity{Package: "hello"},
		Operations: []plan.Operation{{Kind: "install", Effect: plan.EffectMutation}},
	}
	a := NewNixAdapter()
	resolved, err := a.ResolvePlan(context.Background(), rn, tl, mc, &intent)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.InstallResolved(context.Background(), rn, tl, mc, resolved); err == nil {
		t.Fatal("InstallResolved() must surface a non-zero exit")
	}
}

type failingInstallNixRunner struct{ *nixRunner }

func (r *failingInstallNixRunner) Run(ctx context.Context, name string, args ...string) run.Result {
	res := r.nixRunner.Run(ctx, name, args...)
	if strings.Contains(strings.Join(args, " "), "profile install") {
		res.ExitCode = 1
	}
	return res
}
