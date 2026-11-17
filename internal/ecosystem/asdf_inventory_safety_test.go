package ecosystem

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/run"
)

// inventoryRunner models independent asdf and mise inventories; it records any
// attempted mutation, which must never happen on unknown ownership.
type inventoryRunner struct {
	inventories map[string]run.Result
	calls       []run.FakeCall
}

func (r *inventoryRunner) LookPath(_ context.Context, name string) bool {
	_, ok := r.inventories[name]
	return ok
}

func (r *inventoryRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: append([]string(nil), args...)})
	if len(args) > 0 && (args[0] == "list" || args[0] == "ls") {
		return r.inventories[name]
	}
	return run.Result{}
}

func (r *inventoryRunner) mutations() []run.FakeCall {
	var out []run.FakeCall
	for _, call := range r.calls {
		if len(call.Args) > 0 && call.Args[0] == "uninstall" {
			out = append(out, call)
		}
	}
	return out
}

func TestMiseInventoryRejectsIncompleteRows(t *testing.T) {
	for _, tc := range []struct{ name, payload string }{
		{"null", `null`},
		{"null row", `[null]`},
		{"missing version", `[{"installed":true}]`},
		{"blank version", `[{"version":" ","installed":true}]`},
		{"missing installed", `[{"version":"20.0.0"}]`},
		{"null installed", `[{"version":"20.0.0","installed":null}]`},
		{"trailing garbage", `[] []`},
		{"invalid after valid", `[{"version":"20.0.0","installed":true},null]`},
		{"duplicate installed", `[{"version":"20.0.0","installed":true},{"version":"20.0.0","installed":true}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			versions, err := miseInstalledVersions([]byte(tc.payload))
			if err == nil {
				t.Fatalf("versions=%v; invalid inventory must fail", versions)
			}
		})
	}
	for _, tc := range []struct {
		payload string
		want    []string
	}{
		{`[]`, []string{}},
		{`[{"version":"20.0.0","installed":false},{"version":"21.0.0","installed":true}]`, []string{"21.0.0"}},
	} {
		versions, err := miseInstalledVersions([]byte(tc.payload))
		if err != nil || !reflect.DeepEqual(versions, tc.want) {
			t.Fatalf("payload=%s versions=%v err=%v want=%v", tc.payload, versions, err, tc.want)
		}
	}
}

func TestAsdfMiseQueryErrorsCannotProveAbsence(t *testing.T) {
	cases := []struct {
		name       string
		asdf, mise run.Result
	}{
		{"asdf empty mise failed", run.Result{}, run.Result{ExitCode: 1}},
		{"asdf empty mise truncated", run.Result{}, run.Result{Stdout: []byte(`[]`), WaitErr: errors.New("incomplete capture")}},
		{"asdf malformed mise empty", run.Result{Stdout: []byte("warning: broken inventory\n")}, run.Result{Stdout: []byte(`[]`)}},
		{"asdf empty mise null", run.Result{}, run.Result{Stdout: []byte(`null`)}},
		{"asdf corrupt after valid mise empty", run.Result{Stdout: []byte("  18.0.0\ncorrupt output line\n")}, run.Result{Stdout: []byte(`[]`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &inventoryRunner{inventories: map[string]run.Result{"asdf": tc.asdf, "mise": tc.mise}}
			tool, mc := asdfTool("node", "nodejs")
			observation, err := NewAsdfAdapter().Observe(context.Background(), r, tool, mc)
			if err == nil || observation.Presence != plan.PresenceBroken {
				t.Fatalf("Observe=%+v err=%v, want Broken with error", observation, err)
			}
			r.calls = nil
			if err := NewAsdfAdapter().Remove(context.Background(), r, tool, mc); err == nil {
				t.Fatal("Remove succeeded with incomplete inventory")
			}
			if muts := r.mutations(); len(muts) != 0 {
				t.Fatalf("unsafe uninstall: %+v", muts)
			}
		})
	}
}

func TestAsdfMiseRemovalRequiresVerifiedBackendOwnership(t *testing.T) {
	cases := []struct {
		name, version, asdf, mise string
		miseError                 bool
		wantBackend               string
		wantError                 string
	}{
		{name: "exact version belongs to mise", version: "20.0.0", asdf: "  18.0.0\n", mise: `[{"version":"20.0.0","installed":true}]`, wantBackend: "mise"},
		{name: "exact version belongs to asdf", version: "18.0.0", asdf: "  18.0.0\n", mise: `[]`, wantBackend: "asdf"},
		{name: "latest resolves mise", asdf: "", mise: `[{"version":"20.0.0","installed":true}]`, wantBackend: "mise"},
		{name: "exact version missing", version: "21.0.0", asdf: "  18.0.0\n", mise: `[{"version":"20.0.0","installed":true}]`, wantError: "not installed"},
		{name: "latest multiple backends", asdf: "  18.0.0\n", mise: `[{"version":"20.0.0","installed":true}]`, wantError: "ambiguous"},
		{name: "exact duplicate ownership", version: "18.0.0", asdf: "  18.0.0\n", mise: `[{"version":"18.0.0","installed":true}]`, wantError: "ambiguous"},
		{name: "exact query error", version: "18.0.0", asdf: "  18.0.0\n", miseError: true, wantError: "observe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mise := run.Result{Stdout: []byte(tc.mise)}
			if tc.miseError {
				mise = run.Result{ExitCode: 1}
			}
			r := &inventoryRunner{inventories: map[string]run.Result{"asdf": {Stdout: []byte(tc.asdf)}, "mise": mise}}
			tool, mc := asdfTool("node", "nodejs")
			if tc.version != "" {
				mc.Config["version"] = tc.version
			}
			err := NewAsdfAdapter().Remove(context.Background(), r, tool, mc)
			mutations := r.mutations()
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("Remove err=%v, want %q", err, tc.wantError)
				}
				if len(mutations) != 0 {
					t.Fatalf("unexpected uninstall: %+v", mutations)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(mutations) != 1 || mutations[0].Name != tc.wantBackend {
				t.Fatalf("uninstall calls=%+v, want single %s", mutations, tc.wantBackend)
			}
		})
	}
}

func TestAsdfInventoryParsesOnlyCompleteVersionLines(t *testing.T) {
	for _, tc := range []struct {
		out  string
		want []string
		bad  bool
	}{
		{out: "", want: []string{}},
		{out: "  * 18.0.0\n  20.0.0\r\n", want: []string{"18.0.0", "20.0.0"}},
		{out: "  ref:release-1\n", want: []string{"ref:release-1"}},
		{out: "  18.0.0\n warning: failed\n", bad: true},
		{out: "  18.0.0\n  *18.0.0\n", bad: true},
		{out: "*\n", bad: true},
	} {
		versions, err := asdfInstalledVersions(tc.out)
		if tc.bad && err == nil {
			t.Fatalf("output=%q accepted as %+v", tc.out, versions)
		}
		if !tc.bad && (err != nil || !reflect.DeepEqual(versions, tc.want)) {
			t.Fatalf("output=%q got %v %v, want %v", tc.out, versions, err, tc.want)
		}
	}
}

func TestAsdfMiseSelectsRequestedVersionAcrossBackends(t *testing.T) {
	r := &inventoryRunner{inventories: map[string]run.Result{
		"asdf": {Stdout: []byte("  18.0.0\n")},
		"mise": {Stdout: []byte(`[{"version":"20.0.0","installed":true}]`)},
	}}
	tool, mc := asdfTool("node", "nodejs")
	mc.Config["version"] = "20.0.0"
	adapter := NewAsdfAdapter()
	if !adapter.Check(context.Background(), r, tool, mc) {
		t.Fatal("Check did not find desired version in mise")
	}
	obs, err := adapter.Observe(context.Background(), r, tool, mc)
	if err != nil || obs.Presence != plan.PresencePresent || obs.Identity.Version != "20.0.0" {
		t.Fatalf("Observe=%+v err=%v", obs, err)
	}
	version, err := adapter.InstalledVersion(context.Background(), r, tool, mc)
	if err != nil || version != "20.0.0" {
		t.Fatalf("InstalledVersion=%q err=%v", version, err)
	}
}

func TestAsdfMiseUnverifiedDriftDoesNotTriggerUpgrade(t *testing.T) {
	r := &inventoryRunner{inventories: map[string]run.Result{
		"asdf": {Stdout: []byte("  18.0.0\n")},
		"mise": {ExitCode: 1},
	}}
	tool, mc := asdfTool("node", "nodejs")
	mc.Config["version"] = "20.0.0"
	adapter := NewAsdfAdapter()
	obs, err := adapter.Observe(context.Background(), r, tool, mc)
	if err == nil || obs.Presence != plan.PresenceBroken {
		t.Fatalf("Observe=%+v err=%v; want broken", obs, err)
	}
	if adapter.Check(context.Background(), r, tool, mc) {
		t.Fatal("Check claims exact version present")
	}
	if _, err := adapter.InstalledVersion(context.Background(), r, tool, mc); err == nil {
		t.Fatal("InstalledVersion masked incomplete backend inventory")
	}
}
