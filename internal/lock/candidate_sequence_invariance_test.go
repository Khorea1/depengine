package lock

// ADR-007 verification: the candidate sequence of a tool is host-independent.
//
// Loading the same inputs under different platform.Facts must yield the same
// candidate sequence and equal SnapshotIntentMetadata. The inputs are loaded the
// way production loads them (internal/app loadProject): ParseProjectSchema and
// ParseManifest receive config.BuildMap(facts), so every {placeholder} is
// expanded per host before candidates are ordered. After the load, the
// consumers of facts are applied too: FilteredTools, per-candidate eligibility
// (`when`), SpecializeDistribution, and the lookups identity-bearing code
// performs (pin keys, FindMethodCandidate).
//
// Three kinds of fixture:
//
//   - strict: no host placeholder reaches a candidate. The whole candidate
//     sequence must be deep-equal across hosts, except for the two fields the
//     parser stamps with the host on every candidate (TargetArch, TargetOS;
//     see invarianceHostStamps). They are not part of any identity hash. The enumerated and E form of
//     the same upstream are pinned as goldens: different recipes, hence
//     different identities, but each with exactly one identity on every host.
//   - content-expanded: a placeholder inside candidate config (for example an
//     asset template) is expanded per host by design. Content differs; the
//     identity projection (count, order, kind, label) and the hashes must not.
//   - known violation: placeholders in [defaults].method_order reorder the
//     sequence per host. That contradicts ADR-007 decision 1 and is pinned as a
//     strict expected failure, so it cannot be forgotten and cannot rot.
//
// A negative control derives the host-sliced view ADR-007 forbids and requires
// the same checker to flag it, so the test cannot pass vacuously.

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/platform"
)

const invarianceEnumerated = `schema_version = 1

[tools.helper]
requires = ["libfoo", "winhelper"]
requires_when = { winhelper = { target_family = ["windows"] } }

[tools.helper.aur]
pkg = "helper"

[tools.br.github-linux-glibc]
kind = "github"
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-linux.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br" }
when = { os = ["linux"], libc = ["glibc"], is_android = false }

[tools.br.github-linux-musl]
kind = "github"
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-linux-musl.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br" }
when = { os = ["linux"], libc = ["musl"], is_android = false }

[tools.br.github-darwin]
kind = "github"
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-darwin.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br" }
when = { os = ["darwin"] }

[tools.br.github-windows]
kind = "github"
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-windows.zip"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br.exe" }
when = { os = ["windows"], arch = ["x86_64"] }
`

const invarianceFormE = `schema_version = 1

[tools.helper]
requires = ["libfoo", "winhelper"]
requires_when = { winhelper = { target_family = ["windows"] } }

[tools.helper.aur]
pkg = "helper"

[tools.br.github]
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-{os_any}_{arch_any}.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br" }

[[tools.br.github.distributions]]
when = { os = ["darwin"] }

[[tools.br.github.distributions]]
when = { os = ["linux"], libc = ["glibc"], is_android = false }

[[tools.br.github.distributions]]
when = { os = ["linux"], libc = ["musl"], is_android = false }
asset = "br-{version}-{os_any}_musl_{arch_any}.tar.gz"

[[tools.br.github.distributions]]
when = { os = ["windows"], arch = ["x86_64"] }
asset = "br-{version}-{os_any}_{arch_any}.zip"
entrypoints = { br = "br.exe" }
`

// Layered form: the project schema declares only the base of `br`; a personal
// manifest layer supplies its host-keyed distributions. Layers are declared
// inputs, so merging them must not make the sequence depend on the host.
const invarianceLayeredSchema = `schema_version = 1

[tools.helper]
requires = ["libfoo", "winhelper"]
requires_when = { winhelper = { target_family = ["windows"] } }

[tools.helper.aur]
pkg = "helper"

[tools.br.github]
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-{os_any}_{arch_any}.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br" }
`

const invarianceManifest = `schema_version = 1

[packages.br.github]
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-{os_any}_{arch_any}.tar.gz"

[[packages.br.github.distributions]]
when = { os = ["darwin"] }

[[packages.br.github.distributions]]
when = { os = ["linux"], libc = ["glibc"], is_android = false }

[[packages.br.github.distributions]]
when = { os = ["linux"], libc = ["musl"], is_android = false }
asset = "br-{version}-{os_any}_musl_{arch_any}.tar.gz"

[[packages.br.github.distributions]]
when = { os = ["windows"], arch = ["x86_64"] }
asset = "br-{version}-{os_any}_{arch_any}.zip"
entrypoints = { br = "br.exe" }
`

// content-expanded: {arch} sits inside candidate config. The loader expands it
// per host by design (internal/config/placeholder.go), so the candidate's
// content differs, while its identity (kind, label, position) must not.
const invarianceContentExpanded = `schema_version = 1

[tools.foo.github]
repo = "o/foo"
asset = "foo-{version}-{arch}.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { foo = "foo" }
`

// known violation: [defaults].method_order is expanded before candidates are
// ordered, and the labels below are named after distro families, so the
// candidate order follows the host. ADR-007 decision 1 forbids exactly this.
const invarianceMethodOrderPlaceholder = `schema_version = 1

[defaults]
method_order = ["{distro_family}", "github-fallback"]

[tools.foo.debian]
kind = "apt"
pkg = "foo"

[tools.foo.arch]
kind = "pacman"
pkg = "foo"

[tools.foo.github-fallback]
kind = "github"
repo = "o/foo"
asset = "foo-{version}.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { foo = "foo" }
`

type invarianceHost struct {
	name  string
	facts *platform.Facts
}

func invarianceHosts() []invarianceHost {
	return []invarianceHost{
		{"linux-glibc", &platform.Facts{OS: "linux", DistroID: "arch", TargetArch: "x86_64", Libc: "glibc 2.35", TargetFamily: "unix"}},
		{"linux-musl", &platform.Facts{OS: "linux", DistroID: "debian", TargetArch: "x86_64", Libc: "musl 1.2.4", TargetFamily: "unix"}},
		{"darwin-arm64", &platform.Facts{OS: "darwin", TargetArch: "arm64", TargetFamily: "unix"}},
		{"windows", &platform.Facts{OS: "windows", TargetArch: "x86_64", TargetFamily: "windows"}},
		{"android", &platform.Facts{OS: "linux", DistroID: "fedora", TargetArch: "aarch64", Libc: "bionic", IsAndroid: true, TargetFamily: "unix"}},
		// Detection failed or partial: nothing known about the host.
		{"unknown", &platform.Facts{}},
		{"nil-facts", nil},
	}
}

func invarianceWrite(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// invarianceFixture holds one set of input files. Every load reads the same
// paths: ProjectRoot embeds the schema directory, so loads from different
// directories would differ for reasons unrelated to host facts.
type invarianceFixture struct {
	schemaPath    string
	manifestPaths []string
}

func newInvarianceFixture(t *testing.T, schemaBody string, manifestBodies ...string) invarianceFixture {
	t.Helper()
	dir := t.TempDir()
	f := invarianceFixture{schemaPath: invarianceWrite(t, dir, "schema.toml", schemaBody)}
	for i, body := range manifestBodies {
		f.manifestPaths = append(f.manifestPaths, invarianceWrite(t, dir, fmt.Sprintf("manifest-%d.toml", i), body))
	}
	return f
}

// load mirrors internal/app loadProject: the project and the manifest are
// expanded with the host map derived from facts, manifest-only tools are
// filtered, and the layers are merged. Nil facts load with no host map.
func (f invarianceFixture) load(t *testing.T, facts *platform.Facts) *config.Schema {
	t.Helper()
	var hostMap map[string]string
	if facts != nil {
		hostMap = config.BuildMap(facts, platform.ResolveFamily(facts))
	}
	schema, err := config.ParseProjectSchema(f.schemaPath, hostMap)
	if err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	layers := make([]*config.Schema, 0, len(f.manifestPaths)+1)
	for _, path := range f.manifestPaths {
		manifest, err := config.ParseManifest(path, hostMap)
		if err != nil {
			t.Fatalf("parse manifest: %v", err)
		}
		config.FilterManifestTools(schema, manifest)
		if err := config.ValidateManifestLayer(manifest); err != nil {
			t.Fatalf("manifest layer: %v", err)
		}
		if err := config.ValidateManifestNewTools(schema, manifest); err != nil {
			t.Fatalf("manifest new tools: %v", err)
		}
		layers = append(layers, manifest)
	}
	return config.MergeLayers(append(layers, schema)...)
}

// invarianceView is everything identity-bearing code can observe about a schema
// once host facts have been applied by the consumers between load and identity.
type invarianceView struct {
	host string
	// sequence is the deep candidate sequence per tool, in declared order,
	// with the host-stamped fields cleared (invarianceHostStamps).
	sequence map[string][]*config.MethodCandidate
	// raw is the candidate sequence exactly as loaded, host stamps included.
	raw map[string][]*config.MethodCandidate
	// shape is the identity projection of the sequence: "kind/label" per
	// candidate, in order.
	shape map[string][]string
	// methodsHash and sourceHash come from SnapshotIntentMetadata.
	methodsHash map[string]string
	sourceHash  map[string]string
	// keys are the lock v1 pin keys of every candidate, in order.
	keys map[string][]string
	// unresolved lists tracked candidates FindMethodCandidate could not find.
	unresolved []string
}

// invarianceHostEffects records what facts DO change, to prove the test is not
// vacuous: per-candidate eligibility and the effective specialization.
type invarianceHostEffects struct {
	eligible  map[string]bool   // "<tool>/<label>" -> when matched
	effective map[string]string // "<tool>/<label>" -> effective asset ("" = ineligible)
	requires  string            // helper's Requires after FilteredTools
}

// applyHost runs every fact consumer that sits between the loader and
// identity, and returns the identity-visible view plus the host effects.
// pristine is an independent load of the same inputs used to prove that
// specialization returns a copy instead of mutating the shared candidate.
func applyHost(t *testing.T, host invarianceHost, loaded, pristine *config.Schema) (invarianceView, invarianceHostEffects) {
	t.Helper()
	view := invarianceView{
		host:        host.name,
		sequence:    map[string][]*config.MethodCandidate{},
		raw:         map[string][]*config.MethodCandidate{},
		shape:       map[string][]string{},
		keys:        map[string][]string{},
		methodsHash: nil,
	}
	effects := invarianceHostEffects{eligible: map[string]bool{}, effective: map[string]string{}}

	filtered := &config.Schema{Tools: config.FilteredTools(loaded.Tools, host.facts)}
	if helper := filtered.Tools["helper"]; helper != nil {
		effects.requires = strings.Join(helper.Requires, ",")
	}

	for _, name := range sortedToolNames(filtered.Tools) {
		tool := filtered.Tools[name]
		view.sequence[name] = withoutHostStamps(tool.Methods)
		view.raw[name] = tool.Methods
		for _, method := range tool.Methods {
			view.shape[name] = append(view.shape[name], method.Kind+"/"+method.Label)
		}
		counts := map[string]int{}
		for _, method := range tool.Methods {
			idx := counts[method.Kind]
			counts[method.Kind] = idx + 1
			view.keys[name] = append(view.keys[name], toolKey(name, method.Kind, idx)+" ["+method.Label+"]")

			id := name + "/" + method.Label
			effects.eligible[id] = method.When.Match(host.facts)

			effective, err := config.SpecializeDistribution(method, host.facts)
			if err != nil {
				t.Fatalf("%s: specialize %s: %v", host.name, id, err)
			}
			if effective != nil {
				if effective.Kind != method.Kind || effective.Label != method.Label {
					t.Errorf("%s: specialization changed identity of %s: %s/%q -> %s/%q",
						host.name, id, method.Kind, method.Label, effective.Kind, effective.Label)
				}
				effects.effective[id], _ = effective.Config["asset"].(string)
			} else {
				effects.effective[id] = ""
			}

			found, err := config.FindMethodCandidate(tool, method.Kind, method.Label)
			if err != nil || found == nil {
				view.unresolved = append(view.unresolved, id)
			}
		}
	}

	// Specialization must return a copy: the shared candidates are unchanged
	// compared with an independent, never-specialized load.
	for _, name := range sortedToolNames(loaded.Tools) {
		if !reflect.DeepEqual(loaded.Tools[name].Methods, pristine.Tools[name].Methods) {
			t.Errorf("%s: specialization or filtering mutated the shared candidates of %s", host.name, name)
		}
	}

	var err error
	view.methodsHash, view.sourceHash, err = SnapshotIntentMetadata(filtered)
	if err != nil {
		t.Fatalf("%s: SnapshotIntentMetadata: %v", host.name, err)
	}
	return view, effects
}

// invarianceHostStamps are the only MethodCandidate fields the loader fills
// from host facts on candidates that carry no placeholder. They are resolved
// aliases for later asset matching, not identity. The strict fixtures assert
// that nothing else differs; if a new field starts varying by host, the test
// fails and the field must be classified (identity or stamp) deliberately.
var invarianceHostStamps = []string{"TargetArch", "TargetOS"}

// withoutHostStamps returns shallow copies with the host-stamped fields cleared.
func withoutHostStamps(methods []*config.MethodCandidate) []*config.MethodCandidate {
	out := make([]*config.MethodCandidate, len(methods))
	for i, method := range methods {
		cp := *method
		cp.TargetArch, cp.TargetOS = "", ""
		out[i] = &cp
	}
	return out
}

// differingFields names the exported MethodCandidate fields whose values differ.
func differingFields(a, b *config.MethodCandidate) []string {
	var out []string
	va, vb := reflect.ValueOf(*a), reflect.ValueOf(*b)
	for i := 0; i < va.NumField(); i++ {
		if !va.Type().Field(i).IsExported() {
			continue
		}
		if !reflect.DeepEqual(va.Field(i).Interface(), vb.Field(i).Interface()) {
			out = append(out, va.Type().Field(i).Name)
		}
	}
	return out
}

// hostStampViolations returns every field that differs between hosts and is
// not a declared host stamp.
func hostStampViolations(views []invarianceView) []string {
	stamps := map[string]bool{}
	for _, name := range invarianceHostStamps {
		stamps[name] = true
	}
	var out []string
	ref := views[0]
	for _, v := range views[1:] {
		for tool, methods := range ref.raw {
			other := v.raw[tool]
			if len(other) != len(methods) {
				continue // reported by the shape comparison
			}
			for i := range methods {
				for _, field := range differingFields(methods[i], other[i]) {
					if !stamps[field] {
						out = append(out, fmt.Sprintf("%s vs %s: %s[%d].%s varies with host facts but is not a declared host stamp", ref.host, v.host, tool, i, field))
					}
				}
			}
		}
	}
	return out
}

func sortedToolNames(tools map[string]*config.Tool) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// invarianceViolations compares every view with the first one and returns a
// description of each divergence. Empty means the invariant holds.
func invarianceViolations(views []invarianceView, deep bool) []string {
	var out []string
	if len(views) < 2 {
		return []string{"need at least two hosts to compare"}
	}
	ref := views[0]
	for _, v := range views {
		if len(v.unresolved) > 0 {
			out = append(out, fmt.Sprintf("%s: tracked candidates not found by FindMethodCandidate: %v", v.host, v.unresolved))
		}
	}
	for _, v := range views[1:] {
		if !reflect.DeepEqual(ref.shape, v.shape) {
			out = append(out, fmt.Sprintf("%s vs %s: candidate order/kind/label differs: %v != %v", ref.host, v.host, ref.shape, v.shape))
		}
		if deep && !reflect.DeepEqual(ref.sequence, v.sequence) {
			out = append(out, fmt.Sprintf("%s vs %s: candidate sequence differs", ref.host, v.host))
		}
		if !reflect.DeepEqual(ref.methodsHash, v.methodsHash) {
			out = append(out, fmt.Sprintf("%s vs %s: MethodsHash differs: %v != %v", ref.host, v.host, ref.methodsHash, v.methodsHash))
		}
		if !reflect.DeepEqual(ref.sourceHash, v.sourceHash) {
			out = append(out, fmt.Sprintf("%s vs %s: SourceHash differs", ref.host, v.host))
		}
		if !reflect.DeepEqual(ref.keys, v.keys) {
			out = append(out, fmt.Sprintf("%s vs %s: pin keys differ: %v != %v", ref.host, v.host, ref.keys, v.keys))
		}
	}
	return out
}

type invarianceGolden struct {
	tool       string
	candidates int
	keys       []string
	methodsSHA string
}

func TestCandidateSequenceIsHostIndependent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		schema    string
		manifests []string
		golden    invarianceGolden
		// identityOnly compares the identity projection instead of deep
		// candidate equality (content is expanded per host by design).
		identityOnly bool
		// differsByHost: the form must show host-dependent behavior somewhere
		// (eligibility for enumerated, effective asset for E), or the test
		// proves nothing.
		effectsKey func(invarianceHostEffects) map[string]string
	}{
		{
			name:   "enumerated",
			schema: invarianceEnumerated,
			golden: invarianceGolden{
				tool:       "br",
				candidates: 4,
				keys: []string{
					"br/github/0 [github-darwin]",
					"br/github/1 [github-linux-glibc]",
					"br/github/2 [github-linux-musl]",
					"br/github/3 [github-windows]",
				},
				methodsSHA: "546a24f6fe177036dfdd3e256ac2e77bbbbe05a89c54faeb89105ab4a7b78f38",
			},
			effectsKey: func(e invarianceHostEffects) map[string]string {
				out := map[string]string{}
				for k, v := range e.eligible {
					out[k] = fmt.Sprint(v)
				}
				return out
			},
		},
		{
			name:   "form-E",
			schema: invarianceFormE,
			golden: invarianceGolden{
				tool:       "br",
				candidates: 1,
				keys:       []string{"br/github/0 []"},
				methodsSHA: "510e044ce338d75de44fc6248981dcaca21b5f35e34fab0352a57b5c917818db",
			},
			effectsKey: func(e invarianceHostEffects) map[string]string { return e.effective },
		},
		{
			name:      "form-E+manifest-layer",
			schema:    invarianceLayeredSchema,
			manifests: []string{invarianceManifest},
			golden: invarianceGolden{
				tool:       "br",
				candidates: 1,
				keys:       []string{"br/github/0 []"},
				methodsSHA: "510e044ce338d75de44fc6248981dcaca21b5f35e34fab0352a57b5c917818db",
			},
			effectsKey: func(e invarianceHostEffects) map[string]string { return e.effective },
		},
		{
			name:         "content-expanded",
			schema:       invarianceContentExpanded,
			identityOnly: true,
			golden: invarianceGolden{
				tool:       "foo",
				candidates: 1,
				keys:       []string{"foo/github/0 []"},
				methodsSHA: "510e044ce338d75de44fc6248981dcaca21b5f35e34fab0352a57b5c917818db",
			},
			effectsKey: func(e invarianceHostEffects) map[string]string { return e.effective },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newInvarianceFixture(t, tc.schema, tc.manifests...)
			var views []invarianceView
			distinct := map[string]map[string]bool{} // "<tool>/<label>" -> set of host-dependent outcomes
			requiresSeen := map[string]bool{}
			requiresVaries := false
			hasHelper := false
			for _, host := range invarianceHosts() {
				// Each host gets its own load: nothing may leak through shared
				// state or ambient process environment into the sequence.
				loaded := fixture.load(t, host.facts)
				pristine := fixture.load(t, host.facts)
				view, effects := applyHost(t, host, loaded, pristine)
				views = append(views, view)
				if loaded.Tools["helper"] != nil {
					hasHelper = true
					requiresSeen[effects.requires] = true
					requiresVaries = len(requiresSeen) > 1
				}
				for id, outcome := range tc.effectsKey(effects) {
					if distinct[id] == nil {
						distinct[id] = map[string]bool{}
					}
					distinct[id][outcome] = true
				}
			}

			for _, violation := range invarianceViolations(views, !tc.identityOnly) {
				t.Error(violation)
			}
			if !tc.identityOnly {
				for _, violation := range hostStampViolations(views) {
					t.Error(violation)
				}
			}

			// Non-vacuity: facts really do change per-host behavior.
			varies := false
			for _, outcomes := range distinct {
				if len(outcomes) > 1 {
					varies = true
				}
			}
			if !varies {
				t.Fatal("host facts changed nothing observable; the invariance check would be vacuous")
			}

			// Goldens: pinned sequence, keys and hash of the "br" recipe.
			ref := views[0]
			name := tc.golden.tool
			if got := len(ref.sequence[name]); got != tc.golden.candidates {
				t.Errorf("%s candidates = %d, want %d", name, got, tc.golden.candidates)
			}
			if !reflect.DeepEqual(ref.keys[name], tc.golden.keys) {
				t.Errorf("%s pin keys = %v, want %v", name, ref.keys[name], tc.golden.keys)
			}
			if ref.methodsHash[name] != tc.golden.methodsSHA {
				t.Errorf("%s MethodsHash = %s, want %s (changing it re-identifies every lock; see ADR-001/ADR-007)", name, ref.methodsHash[name], tc.golden.methodsSHA)
			}
			// FilteredTools is the one consumer that really rewrites a tool under
			// facts (Requires). It must do so without touching the candidates.
			if hasHelper && !requiresVaries {
				t.Error("FilteredTools changed no Requires across hosts; the gated-dependency fixture is vacuous")
			}
		})
	}
}

// Negative control. The optimization ADR-007 rejects (return only the
// candidates that match the host) must be flagged by the very checker above.
// If this stops failing, the invariance test has lost its teeth.
func TestHostSlicedCandidateListViolatesInvariance(t *testing.T) {
	fixture := newInvarianceFixture(t, invarianceEnumerated)
	var views []invarianceView
	for _, host := range invarianceHosts() {
		loaded := fixture.load(t, host.facts)
		br := loaded.Tools["br"]
		var sliced []*config.MethodCandidate
		for _, method := range br.Methods {
			if method.When.Match(host.facts) {
				sliced = append(sliced, method)
			}
		}
		br.Methods = sliced
		view := invarianceView{
			host:     host.name,
			sequence: map[string][]*config.MethodCandidate{"br": sliced},
			shape:    map[string][]string{},
			keys:     map[string][]string{},
		}
		counts := map[string]int{}
		for _, method := range sliced {
			view.shape["br"] = append(view.shape["br"], method.Kind+"/"+method.Label)
			idx := counts[method.Kind]
			counts[method.Kind] = idx + 1
			view.keys["br"] = append(view.keys["br"], toolKey("br", method.Kind, idx)+" ["+method.Label+"]")
		}
		view.methodsHash, view.sourceHash, _ = SnapshotIntentMetadata(&config.Schema{Tools: loaded.Tools})
		views = append(views, view)
	}
	violations := invarianceViolations(views, true)
	if len(violations) == 0 {
		t.Fatal("host-sliced candidate lists were not flagged: the invariance checker cannot detect the forbidden optimization")
	}
	var sawHash, sawKeys bool
	for _, v := range violations {
		sawHash = sawHash || strings.Contains(v, "MethodsHash differs")
		sawKeys = sawKeys || strings.Contains(v, "pin keys differ")
	}
	if !sawHash || !sawKeys {
		t.Errorf("expected both MethodsHash and pin-key divergence, got: %v", violations)
	}
	// And the lookup failure ADR-007 documents: a candidate tracked on one
	// host is gone from another host's sliced list.
	loaded := fixture.load(t, invarianceHosts()[0].facts)
	musl := loaded.Tools["br"]
	var glibcOnly []*config.MethodCandidate
	for _, method := range musl.Methods {
		if method.When.Match(invarianceHosts()[0].facts) {
			glibcOnly = append(glibcOnly, method)
		}
	}
	musl.Methods = glibcOnly
	if found, err := config.FindMethodCandidate(musl, "github", "github-linux-musl"); err == nil && found != nil {
		t.Error("expected the musl candidate to be missing from the glibc-sliced list")
	}
}

// Known violation of ADR-007 decision 1, pinned as a strict expected failure.
//
// [defaults].method_order is expanded with the host map before candidates are
// ordered (internal/config/parse.go: ExpandAll on the document, then
// OrderMethods(..., defaults.MethodOrder)). With labels named after distro
// families, the candidate order, the pin keys and MethodsHash follow the host.
// A lock written on one host then reports drift on another.
//
// This test PASSES while the violation exists and FAILS when it is fixed. When
// it fails, the fix landed: delete this test and add the fixture to the strict
// table in TestCandidateSequenceIsHostIndependent. Do not "fix" it by loosening
// the assertion.
func TestKnownViolationMethodOrderPlaceholderReordersCandidates(t *testing.T) {
	fixture := newInvarianceFixture(t, invarianceMethodOrderPlaceholder)
	var views []invarianceView
	for _, host := range invarianceHosts() {
		view, _ := applyHost(t, host, fixture.load(t, host.facts), fixture.load(t, host.facts))
		views = append(views, view)
	}
	violations := invarianceViolations(views, false)
	if len(violations) == 0 {
		t.Fatal("candidate order no longer depends on host facts: the ADR-007 violation through " +
			"[defaults].method_order placeholders is fixed. Move this fixture into the strict table of " +
			"TestCandidateSequenceIsHostIndependent and delete this test.")
	}
	var order, hash bool
	for _, v := range violations {
		order = order || strings.Contains(v, "order/kind/label differs")
		hash = hash || strings.Contains(v, "MethodsHash differs")
	}
	if !order || !hash {
		t.Errorf("expected both candidate order and MethodsHash to diverge, got: %v", violations)
	}
	t.Logf("known violation reproduced: %d divergences across %d hosts, e.g. %s", len(violations), len(views), violations[0])
}
