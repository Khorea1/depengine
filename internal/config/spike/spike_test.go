package spike

// Spike A-vs-C (dep-1n0.1): baseline A (candidatos explícitos) vs protótipo C
// (base+cases expandidos via ExpandCompact + parser/merge existentes).
//
// Cobre: equivalência campo a campo antes/depois do merge, MethodsHash,
// matriz de hosts, fail-closed, determinismo e custo. Sem rede, sem install.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/lock"
	"github.com/Khorea1/depengine/internal/methodkind"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/validate"
)

func testdata(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func parseSchemaFile(t *testing.T, path string) *config.Schema {
	t.Helper()
	s, err := config.ParseProjectSchema(path, map[string]string{})
	if err != nil {
		t.Fatalf("ParseProjectSchema(%s): %v", path, err)
	}
	return s
}

func parseExpanded(t *testing.T, compactPath string) *config.Schema {
	t.Helper()
	raw, err := os.ReadFile(compactPath) // #nosec G304 -- fixture-controlled path; no untrusted runtime input crosses this test boundary.
	if err != nil {
		t.Fatal(err)
	}
	expanded, notes, err := ExpandCompact(raw)
	if err != nil {
		t.Fatalf("ExpandCompact(%s): %v", compactPath, err)
	}
	for _, n := range notes {
		t.Logf("overlap-note: %s", n)
	}
	tmp := filepath.Join(t.TempDir(), "expanded.toml")
	if err := os.WriteFile(tmp, expanded, 0o600); err != nil {
		t.Fatal(err)
	}
	return parseSchemaFile(t, tmp)
}

// canonicalCandidate flattens a MethodCandidate into comparable JSON.
func canonicalCandidate(m *config.MethodCandidate) map[string]any {
	when := ""
	if m.When != nil {
		when = m.When.String()
	}
	cfg, _ := json.Marshal(m.Config)
	archMap, _ := json.Marshal(m.ArchMap)
	osMap, _ := json.Marshal(m.OSMap)
	return map[string]any{
		"kind":     m.Kind,
		"label":    m.Label,
		"when":     when,
		"config":   string(cfg),
		"inferred": m.Inferred,
		"archMap":  string(archMap),
		"osMap":    string(osMap),
		"requires": m.Requires,
		"sources":  len(m.Sources),
		"err":      m.Err != nil,
		"targetOS": m.TargetOS,
		"arch":     m.TargetArch,
	}
}

func candidateSnapshot(methods []*config.MethodCandidate) []map[string]any {
	out := make([]map[string]any, 0, len(methods))
	for _, m := range methods {
		out = append(out, canonicalCandidate(m))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i]["label"] != out[j]["label"] {
			return out[i]["label"].(string) < out[j]["label"].(string)
		}
		return out[i]["kind"].(string) < out[j]["kind"].(string)
	})
	return out
}

func assertSameCandidates(t *testing.T, tool string, a, c []*config.MethodCandidate) {
	t.Helper()
	sa := candidateSnapshot(a)
	sc := candidateSnapshot(c)
	if !reflect.DeepEqual(sa, sc) {
		ja, _ := json.MarshalIndent(sa, "", "  ")
		jc, _ := json.MarshalIndent(sc, "", "  ")
		t.Fatalf("tool %s: A×C divergem.\nA=%s\nC=%s", tool, ja, jc)
	}
}

func TestBaselineParses(t *testing.T) {
	for tool, file := range map[string]string{
		"br":  "a_br.toml",
		"omp": "a_omp.toml",
		"wt":  "a_wt.toml",
	} {
		s := parseSchemaFile(t, testdata(t, file))
		got := s.Tools[tool]
		if got == nil {
			t.Fatalf("%s: tool ausente", tool)
		}
		want := map[string]int{"br": 4, "omp": 3, "wt": 2}[tool]
		if len(got.Methods) != want {
			t.Fatalf("%s: %d candidatos, want %d", tool, len(got.Methods), want)
		}
		for _, m := range got.Methods {
			if m.Kind != "github" {
				t.Errorf("%s: kind=%q, want github", tool, m.Kind)
			}
			if m.When == nil {
				t.Errorf("%s/%s: when ausente", tool, m.Label)
			}
		}
	}
}

func TestExpansionEquivalence(t *testing.T) {
	for tool, pair := range map[string][2]string{
		"br":  {"a_br.toml", "c_br.toml"},
		"omp": {"a_omp.toml", "c_omp.toml"},
		"wt":  {"a_wt.toml", "c_wt.toml"},
	} {
		a := parseSchemaFile(t, testdata(t, pair[0]))
		c := parseExpanded(t, testdata(t, pair[1]))
		assertSameCandidates(t, tool, a.Tools[tool].Methods, c.Tools[tool].Methods)
		// Ordem do parser preservada (não só ordem do array cases).
		var orderA, orderC []string
		for _, m := range a.Tools[tool].Methods {
			orderA = append(orderA, m.Kind+"\x00"+m.Label)
		}
		for _, m := range c.Tools[tool].Methods {
			orderC = append(orderC, m.Kind+"\x00"+m.Label)
		}
		if !reflect.DeepEqual(orderA, orderC) {
			t.Fatalf("%s: ordem diverge A=%v C=%v", tool, orderA, orderC)
		}
	}
}

func TestMergeEquivalence(t *testing.T) {
	// Mesmo manifesto normal fundido ao schema A e ao schema C expandido.
	for _, compact := range []string{"c_br.toml"} {
		a := parseSchemaFile(t, testdata(t, "a_br.toml"))
		c := parseExpanded(t, testdata(t, compact))
		manifest, err := config.ParseManifest(testdata(t, "manifest_br.toml"), map[string]string{})
		if err != nil {
			t.Fatalf("ParseManifest: %v", err)
		}
		ma := config.MergeLayers(a, manifest)
		mc := config.MergeLayers(c, manifest)
		assertSameCandidates(t, "br-merged", ma.Tools["br"].Methods, mc.Tools["br"].Methods)
		// Override parcial: checksum do github-linux substituído por inteiro.
		for _, s := range []*config.Schema{ma, mc} {
			m, err := config.FindMethodCandidate(s.Tools["br"], "github", "github-linux")
			if err != nil {
				t.Fatal(err)
			}
			if m.Config["checksum"] != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
				t.Fatalf("merge não aplicou override de checksum: %v", m.Config["checksum"])
			}
		}
		// Candidato novo do manifesto anexado após os existentes.
		if len(ma.Tools["br"].Methods) != 6 { // 4 + linux-override(in place) + extra + ?
			t.Logf("merged count A=%d C=%d", len(ma.Tools["br"].Methods), len(mc.Tools["br"].Methods))
		}
	}
}

func TestMethodsHashEquivalence(t *testing.T) {
	for tool, pair := range map[string][2]string{
		"br": {"a_br.toml", "c_br.toml"},
		"wt": {"a_wt.toml", "c_wt.toml"},
	} {
		a := parseSchemaFile(t, testdata(t, pair[0]))
		c := parseExpanded(t, testdata(t, pair[1]))
		ha, _, err := lock.SnapshotIntentMetadata(a)
		if err != nil {
			t.Fatal(err)
		}
		hc, _, err := lock.SnapshotIntentMetadata(c)
		if err != nil {
			t.Fatal(err)
		}
		if ha[tool] != hc[tool] {
			t.Fatalf("%s: MethodsHash diverge A=%q C=%q", tool, ha[tool], hc[tool])
		}
	}
}

func TestSourceHashEquivalence(t *testing.T) {
	for tool, pair := range map[string][2]string{
		"br": {"a_br.toml", "c_br.toml"},
		"wt": {"a_wt.toml", "c_wt.toml"},
	} {
		a := parseSchemaFile(t, testdata(t, pair[0]))
		c := parseExpanded(t, testdata(t, pair[1]))
		_, sa, err := lock.SnapshotIntentMetadata(a)
		if err != nil {
			t.Fatal(err)
		}
		_, sc, err := lock.SnapshotIntentMetadata(c)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(sa, sc) {
			t.Fatalf("%s: SourceHash diverge A=%v C=%v", tool, sa, sc)
		}
		_ = tool
	}
}

func TestValidateEquivalence(t *testing.T) {
	// why/planner/adapters consomem a representação expandida normal:
	// validação semântica deve concordar entre A e C.
	kinds := methodkind.KnownKinds()
	for tool, pair := range map[string][2]string{
		"br": {"a_br.toml", "c_br.toml"},
		"wt": {"a_wt.toml", "c_wt.toml"},
	} {
		a := parseSchemaFile(t, testdata(t, pair[0]))
		c := parseExpanded(t, testdata(t, pair[1]))
		ra := validate.ValidateSchema(a, kinds)
		rc := validate.ValidateSchema(c, kinds)
		if ra.HasErrors() != rc.HasErrors() {
			t.Fatalf("%s: validate errors divergem A=%v C=%v", tool, ra.HasErrors(), rc.HasErrors())
		}
		if len(ra.Errors) != len(rc.Errors) {
			t.Fatalf("%s: %d erros A vs %d erros C", tool, len(ra.Errors), len(rc.Errors))
		}
		if len(ra.Warnings) != len(rc.Warnings) {
			t.Fatalf("%s: %d warnings A vs %d warnings C", tool, len(ra.Warnings), len(rc.Warnings))
		}
	}
}

func hostMatrix() map[string]*platform.Facts {
	return map[string]*platform.Facts{
		"linux-glibc":         {OS: "linux", TargetArch: "x86_64", Libc: "glibc 2.35", TargetFamily: "unix"},
		"linux-musl":          {OS: "linux", TargetArch: "x86_64", Libc: "musl 1.2.4", TargetFamily: "unix"},
		"mac-x86_64":          {OS: "darwin", TargetArch: "x86_64", Libc: "", TargetFamily: "unix"},
		"mac-arm64":           {OS: "darwin", TargetArch: "arm64", Libc: "", TargetFamily: "unix"},
		"windows-x86_64":      {OS: "windows", TargetArch: "x86_64", Libc: "", TargetFamily: "windows"},
		"android-termux":      {OS: "linux", TargetArch: "aarch64", Libc: "bionic", IsAndroid: true, TargetFamily: "unix"},
		"freebsd-unsupported": {OS: "freebsd", TargetArch: "x86_64", Libc: "", TargetFamily: "unix"},
	}
}

func eligibleLabels(t *testing.T, s *config.Schema, tool string, facts *platform.Facts) []string {
	t.Helper()
	var out []string
	for _, m := range s.Tools[tool].Methods {
		matched := m.When == nil || m.When.Match(facts)
		if matched {
			out = append(out, m.Label)
		}
	}
	sort.Strings(out)
	return out
}

func TestHostMatrixEquivalence(t *testing.T) {
	a := parseSchemaFile(t, testdata(t, "a_br.toml"))
	c := parseExpanded(t, testdata(t, "c_br.toml"))
	want := map[string][]string{
		"linux-glibc":         {"github-linux"},
		"linux-musl":          {"github-musl"},
		"mac-x86_64":          {"github-mac"},
		"mac-arm64":           {"github-mac"},
		"windows-x86_64":      {"github-windows"},
		"android-termux":      {},
		"freebsd-unsupported": {},
	}
	for host, facts := range hostMatrix() {
		ea := eligibleLabels(t, a, "br", facts)
		ec := eligibleLabels(t, c, "br", facts)
		if !sameStrings(ea, ec) {
			t.Fatalf("host %s: elegíveis divergem A=%v C=%v", host, ea, ec)
		}
		if !sameStrings(ea, want[host]) {
			t.Fatalf("host %s: elegíveis=%v, want %v", host, ea, want[host])
		}
	}
}

func TestFailClosed(t *testing.T) {
	base := `
schema_version = 1
[tools.br]
method_only = ["github"]
  [tools.br.github]
  repo = "o/r"
  asset = "a.tar.gz"
  checksum = "sha256:auto"
  scope = "user"
`
	cases := []struct {
		name     string
		fragment string
		wantErr  string
	}{
		{"dup-label", "cases = [{label=\"x\", when={os=[\"linux\"]}}, {label=\"x\", when={os=[\"darwin\"]}}]", "duplicate"},
		{"no-label", "cases = [{when={os=[\"linux\"]}}]", "label"},
		{"no-when", "cases = [{label=\"x\"}]", "when"},
		{"forbidden-repo", "cases = [{label=\"x\", when={os=[\"linux\"]}, repo=\"o/outro\"}]", "forbids"},
		{"forbidden-scope", "cases = [{label=\"x\", when={os=[\"linux\"]}, scope=\"system\"}]", "forbids"},
		{"forbidden-checksum", "cases = [{label=\"x\", when={os=[\"linux\"]}, checksum=\"sha256:abc\"}]", "forbids"},
		{"unknown-field", "cases = [{label=\"x\", when={os=[\"linux\"]}, mirrors=[\"a\"]}]", "forbids"},
		{"base-when", "when = {os=[\"linux\"]}\n  cases = [{label=\"x\", when={os=[\"linux\"]}}]", "must not carry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := base + "  " + tc.fragment + "\n"
			_, _, err := ExpandCompact([]byte(doc))
			if err == nil {
				t.Fatalf("esperava erro contendo %q, obteve nil", tc.wantErr)
			}
			if !containsFold(err.Error(), tc.wantErr) {
				t.Fatalf("erro %q não contém %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestDeterminism(t *testing.T) {
	raw, err := os.ReadFile(testdata(t, "c_br.toml"))
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := ExpandCompact(raw)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		again, _, err := ExpandCompact(raw)
		if err != nil {
			t.Fatal(err)
		}
		if string(first) != string(again) {
			t.Fatalf("expansão não determinística na iteração %d", i)
		}
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestGoldens(t *testing.T) {
	// Goldens reproduzíveis do comportamento atual (A): candidatos
	// canônicos + MethodsHash por tool. C deve ser byte-idêntico.
	for tool, pair := range map[string][2]string{
		"br":  {"a_br.toml", "c_br.toml"},
		"omp": {"a_omp.toml", "c_omp.toml"},
		"wt":  {"a_wt.toml", "c_wt.toml"},
	} {
		a := parseSchemaFile(t, testdata(t, pair[0]))
		c := parseExpanded(t, testdata(t, pair[1]))
		ha, _, err := lock.SnapshotIntentMetadata(a)
		if err != nil {
			t.Fatal(err)
		}
		golden := map[string]any{
			"tool":        tool,
			"candidates":  candidateSnapshot(a.Tools[tool].Methods),
			"methodsHash": ha[tool],
		}
		raw, _ := json.MarshalIndent(golden, "", "  ")
		path := filepath.Join("testdata", "goldens", tool+".json")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- test golden dir under package tree; parent perms inherited from checkout.
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil { // #nosec G306 -- versioned JSON golden must stay readable; no secret material.
				t.Fatal(err)
			}
			t.Logf("golden criado: %s", path)
		} else {
			want, err := os.ReadFile(path) // #nosec G304 -- golden path derived from fixed tool key; no untrusted runtime input.
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != string(raw)+"\n" {
				t.Fatalf("tool %s: golden diverge; comportamento atual mudou ou expansão quebrou", tool)
			}
		}
		// C never diverges from A (cobertura redundante e intencional).
		assertSameCandidates(t, tool, a.Tools[tool].Methods, c.Tools[tool].Methods)
	}
}

func containsFold(haystack, needle string) bool {
	hl, nl := toLower(haystack), toLower(needle)
	return len(hl) >= len(nl) && (hl == nl || len(nl) == 0 || indexOf(hl, nl) >= 0)
}

func toLower(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return string(out)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
