package spike

// Spike A-vs-C (dep-1n0.1, rodada 2): baseline A (candidatos explícitos,
// receitas reais de br/omp/wt em [packages]) vs protótipo C (base+cases
// expandidos via ExpandCompact + parser/merge existentes).
//
// Sem rede, sem install. Projeções v2 com resolução remota ficam como
// limitação documentada: os insumos da projeção (candidatos + intent
// metadata) são comparados integralmente.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
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

func parseManifestFile(t *testing.T, path string) *config.Schema {
	t.Helper()
	s, err := config.ParseManifest(path, map[string]string{})
	if err != nil {
		t.Fatalf("ParseManifest(%s): %v", path, err)
	}
	return s
}

type schemaParser func(string, map[string]string) (*config.Schema, error)

func parseExpanded(t *testing.T, compactPath string, parse schemaParser) *config.Schema {
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
	s, err := parse(tmp, map[string]string{})
	if err != nil {
		t.Fatalf("parse expanded(%s): %v", compactPath, err)
	}
	return s
}

// canonicalCandidate flattens a MethodCandidate into comparable JSON,
// including hooks, sources, and secret references (metadados completos).
func canonicalCandidate(m *config.MethodCandidate) map[string]any {
	when := ""
	if m.When != nil {
		when = m.When.String()
	}
	cfg, _ := json.Marshal(m.Config)
	archMap, _ := json.Marshal(m.ArchMap)
	osMap, _ := json.Marshal(m.OSMap)
	pre, _ := json.Marshal(m.PreInstall)
	post, _ := json.Marshal(m.PostInstall)
	sources, _ := json.Marshal(m.Sources)
	secret, _ := json.Marshal(m.SecretRef)
	checksumSecret, _ := json.Marshal(m.ChecksumSecretRef)
	signatureSecret, _ := json.Marshal(m.SignatureSecretRef)
	return map[string]any{
		"kind":            m.Kind,
		"label":           m.Label,
		"when":            when,
		"config":          string(cfg),
		"inferred":        m.Inferred,
		"archMap":         string(archMap),
		"osMap":           string(osMap),
		"requires":        m.Requires,
		"sources":         string(sources),
		"preInstall":      string(pre),
		"postInstall":     string(post),
		"secretRef":       string(secret),
		"checksumSecret":  string(checksumSecret),
		"signatureSecret": string(signatureSecret),
		"err":             m.Err != nil,
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

func assertSameOrder(t *testing.T, tool string, a, c []*config.MethodCandidate) {
	t.Helper()
	var orderA, orderC []string
	for _, m := range a {
		orderA = append(orderA, m.Kind+"\x00"+m.Label)
	}
	for _, m := range c {
		orderC = append(orderC, m.Kind+"\x00"+m.Label)
	}
	if !reflect.DeepEqual(orderA, orderC) {
		t.Fatalf("%s: ordem diverge A=%v C=%v", tool, orderA, orderC)
	}
}

func TestBaselineParses(t *testing.T) {
	// [tools]-style (br) e [packages]-style real (br/omp/wt).
	s := parseSchemaFile(t, testdata(t, "a_br.toml"))
	if len(s.Tools["br"].Methods) != 4 {
		t.Fatalf("br: %d candidatos, want 4", len(s.Tools["br"].Methods))
	}
	m := parseManifestFile(t, testdata(t, "a_manifest.toml"))
	if len(m.Tools["br"].Methods) != 4 {
		t.Fatalf("packages.br: %d candidatos, want 4", len(m.Tools["br"].Methods))
	}
	omp := m.Tools["omp"]
	if omp == nil {
		t.Fatal("packages.omp ausente")
	}
	if len(omp.Methods) != 6 { // aur, native, 3 github, nix
		t.Fatalf("packages.omp: %d candidatos, want 6", len(omp.Methods))
	}
	if len(omp.MethodOnly) != 4 {
		t.Fatalf("packages.omp: method_only=%v, want 4 entradas", omp.MethodOnly)
	}
	wt := m.Tools["wt"]
	if wt == nil {
		t.Fatal("packages.wt ausente")
	}
	if len(wt.Methods) != 6 { // native, aur, cargo, conda, 2 github
		t.Fatalf("packages.wt: %d candidatos, want 6", len(wt.Methods))
	}
	if len(wt.PostInstall) != 2 {
		t.Fatalf("packages.wt: %d post_install, want 2", len(wt.PostInstall))
	}
	for _, tool := range []*config.Tool{omp, wt} {
		for _, cand := range tool.Methods {
			if cand.When == nil && cand.Kind == "github" {
				t.Errorf("github sem when: kind=%s label=%s", cand.Kind, cand.Label)
			}
		}
	}
}

func TestPreservesNonCases(t *testing.T) {
	// simple = [...] gera uma tool por entrada; todas sobrevivem.
	s := parseSchemaFile(t, testdata(t, "schema_real.toml"))
	for _, name := range []string{"zsh", "bat"} {
		if s.Tools[name] == nil || len(s.Tools[name].Methods) == 0 {
			t.Fatalf("simple list não preservada: tool %q ausente", name)
		}
	}
	// P3: simple passa EFETIVAMENTE pelo expansor junto com cases.
	c := parseExpanded(t, testdata(t, "c_simple.toml"), config.ParseProjectSchema)
	for _, name := range []string{"zsh", "bat"} {
		if c.Tools[name] == nil || len(c.Tools[name].Methods) == 0 {
			t.Fatalf("expansor perdeu simple: tool %q ausente", name)
		}
	}
	if len(c.Tools["wt"].Methods) != 2 {
		t.Fatalf("wt expandido: %d candidatos, want 2", len(c.Tools["wt"].Methods))
	}
	if c.Tools["br"] == nil {
		t.Fatal("tool vazia br perdida na expansão")
	}
	raw, err := os.ReadFile(testdata(t, "c_manifest.toml")) // #nosec G304 -- fixture-controlled path.
	if err != nil {
		t.Fatal(err)
	}
	expanded, _, err := ExpandCompact(raw)
	if err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(t.TempDir(), "expanded.toml")
	if err := os.WriteFile(tmp, expanded, 0o600); err != nil {
		t.Fatal(err)
	}
	cm := parseManifestFile(t, tmp)
	// aur/native/nix/cargo/conda (label vazio: a seção já é o kind) intactos.
	for _, tc := range []struct {
		tool, kind, label string
	}{
		{"omp", "aur", ""},
		{"omp", "native", ""},
		{"omp", "nix", ""},
		{"wt", "aur", ""},
		{"wt", "cargo", ""},
	} {
		if _, err := config.FindMethodCandidate(cm.Tools[tc.tool], tc.kind, tc.label); err != nil {
			t.Errorf("candidato não-github perdido: %s/%s/%s: %v", tc.tool, tc.kind, tc.label, err)
		}
	}
}

func TestExpansionEquivalence(t *testing.T) {
	// [tools]: br. [packages]: br/omp/wt reais.
	aTools := parseSchemaFile(t, testdata(t, "a_br.toml"))
	cTools := parseExpanded(t, testdata(t, "c_br.toml"), config.ParseProjectSchema)
	assertSameCandidates(t, "br[tools]", aTools.Tools["br"].Methods, cTools.Tools["br"].Methods)
	assertSameOrder(t, "br[tools]", aTools.Tools["br"].Methods, cTools.Tools["br"].Methods)

	a := parseManifestFile(t, testdata(t, "a_manifest.toml"))
	c := parseExpanded(t, testdata(t, "c_manifest.toml"), config.ParseManifest)
	for _, tool := range []string{"br", "omp", "wt"} {
		assertSameCandidates(t, tool, a.Tools[tool].Methods, c.Tools[tool].Methods)
		assertSameOrder(t, tool, a.Tools[tool].Methods, c.Tools[tool].Methods)
	}
	// Chaves de nível de tool preservadas (method_only, post_install).
	for _, tool := range []string{"omp", "wt"} {
		if !reflect.DeepEqual(a.Tools[tool].MethodOnly, c.Tools[tool].MethodOnly) {
			t.Fatalf("%s: method_only diverge A=%v C=%v", tool, a.Tools[tool].MethodOnly, c.Tools[tool].MethodOnly)
		}
		if len(a.Tools[tool].PostInstall) != len(c.Tools[tool].PostInstall) {
			t.Fatalf("%s: post_install diverge", tool)
		}
	}
}

func TestMergeEquivalence(t *testing.T) {
	// Caminho real: schema vazio + manifesto ([packages]), depois override.
	schema := parseSchemaFile(t, testdata(t, "schema_real.toml"))
	a := parseManifestFile(t, testdata(t, "a_manifest.toml"))
	c := parseExpanded(t, testdata(t, "c_manifest.toml"), config.ParseManifest)
	override := parseManifestFile(t, testdata(t, "manifest_override.toml"))

	ma := config.MergeLayers(schema, a, override)
	mc := config.MergeLayers(schema, c, override)
	for _, tool := range []string{"br", "omp", "wt"} {
		assertSameCandidates(t, tool+"-merged", ma.Tools[tool].Methods, mc.Tools[tool].Methods)
		// P1: ordem e hashes também depois do merge (o snapshot ordenado
		// acima não detectaria divergência de ordenação).
		assertSameOrder(t, tool+"-merged", ma.Tools[tool].Methods, mc.Tools[tool].Methods)
	}
	hma, sma, err := lock.SnapshotIntentMetadata(ma)
	if err != nil {
		t.Fatal(err)
	}
	hmc, smc, err := lock.SnapshotIntentMetadata(mc)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"br", "omp", "wt"} {
		if hma[tool] != hmc[tool] {
			t.Fatalf("%s merged: MethodsHash diverge A=%q C=%q", tool, hma[tool], hmc[tool])
		}
	}
	if !reflect.DeepEqual(sma, smc) {
		t.Fatalf("merged SourceHash diverge A=%v C=%v", sma, smc)
	}
	// br: 4 originais + 1 novo = 5 (override do github-linux é in place).
	if len(ma.Tools["br"].Methods) != 5 {
		t.Fatalf("br merged: %d candidatos, want 5", len(ma.Tools["br"].Methods))
	}
	for _, s := range []*config.Schema{ma, mc} {
		m, err := config.FindMethodCandidate(s.Tools["br"], "github", "github-linux")
		if err != nil {
			t.Fatal(err)
		}
		if m.Config["checksum"] != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
			t.Fatalf("merge não aplicou override de checksum: %v", m.Config["checksum"])
		}
		m, err = config.FindMethodCandidate(s.Tools["omp"], "github", "github-musl")
		if err != nil {
			t.Fatal(err)
		}
		if m.Config["scope"] != "system" {
			t.Fatalf("merge não aplicou override omp: %v", m.Config["scope"])
		}
	}
}

func TestIntentMetadataEquivalence(t *testing.T) {
	// MethodsHash + SourceHash: insumos offline da projeção/lock.
	a := parseManifestFile(t, testdata(t, "a_manifest.toml"))
	c := parseExpanded(t, testdata(t, "c_manifest.toml"), config.ParseManifest)
	ha, sa, err := lock.SnapshotIntentMetadata(a)
	if err != nil {
		t.Fatal(err)
	}
	hc, sc, err := lock.SnapshotIntentMetadata(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"br", "omp", "wt"} {
		if ha[tool] != hc[tool] {
			t.Fatalf("%s: MethodsHash diverge A=%q C=%q", tool, ha[tool], hc[tool])
		}
	}
	if !reflect.DeepEqual(sa, sc) {
		t.Fatalf("SourceHash diverge A=%v C=%v", sa, sc)
	}
}

func TestCandidateIdentityEquivalence(t *testing.T) {
	// FindMethodCandidate (usado por upgrade/remove/why) resolve igual em A e C.
	a := parseManifestFile(t, testdata(t, "a_manifest.toml"))
	c := parseExpanded(t, testdata(t, "c_manifest.toml"), config.ParseManifest)
	for _, tc := range []struct{ tool, kind, label string }{
		{"br", "github", "github-windows"},
		{"omp", "github", "github-musl"},
		{"wt", "github", "github-linux"},
		{"wt", "aur", ""},
		{"omp", "nix", ""},
	} {
		ma, err := config.FindMethodCandidate(a.Tools[tc.tool], tc.kind, tc.label)
		if err != nil {
			t.Fatal(err)
		}
		mc, err := config.FindMethodCandidate(c.Tools[tc.tool], tc.kind, tc.label)
		if err != nil {
			t.Fatal(err)
		}
		ja, _ := json.Marshal(canonicalCandidate(ma))
		jc, _ := json.Marshal(canonicalCandidate(mc))
		if string(ja) != string(jc) {
			t.Fatalf("%s/%s: identidade diverge\nA=%s\nC=%s", tc.tool, tc.label, ja, jc)
		}
	}
}

func TestValidateEquivalence(t *testing.T) {
	// Exigência estrita: zero erros nos fundidos A e C (não apenas
	// contagens iguais), com diagnósticos idênticos. Fixture inválida
	// reprova o spike.
	kinds := methodkind.KnownKinds()
	schema := parseSchemaFile(t, testdata(t, "schema_real.toml"))
	a := parseManifestFile(t, testdata(t, "a_manifest.toml"))
	c := parseExpanded(t, testdata(t, "c_manifest.toml"), config.ParseManifest)
	// O caminho operacional real é schema+manifesto fundidos (o manifesto
	// sozinho não carrega [defaults]; ex.: candidato nix exige a entrada
	// correspondente na ordem, como no depends.toml real).
	ma := config.MergeLayers(schema, a)
	mc := config.MergeLayers(schema, c)
	for name, s := range map[string]*config.Schema{
		"merged-A": ma, "merged-C": mc,
	} {
		r := validate.ValidateSchema(s, kinds)
		if r.HasErrors() {
			raw, _ := json.Marshal(r.Errors)
			t.Fatalf("%s: %d erros de validação: %s", name, len(r.Errors), raw)
		}
	}
	ra := validate.ValidateSchema(ma, kinds)
	rc := validate.ValidateSchema(mc, kinds)
	// P2: diagnósticos completos, não apenas contagens.
	ja, _ := json.Marshal(ra)
	jc, _ := json.Marshal(rc)
	if string(ja) != string(jc) {
		t.Fatalf("diagnósticos divergem\nA=%s\nC=%s", ja, jc)
	}
}

func hostMatrix() map[string]*platform.Facts {
	return map[string]*platform.Facts{
		"linux-glibc":         {OS: "linux", TargetArch: "x86_64", Libc: "glibc 2.35", TargetFamily: "unix"},
		"linux-musl":          {OS: "linux", TargetArch: "x86_64", Libc: "musl 1.2.4", TargetFamily: "unix"},
		"mac-arm64":           {OS: "darwin", TargetArch: "arm64", TargetFamily: "unix"},
		"windows-x86_64":      {OS: "windows", TargetArch: "x86_64", TargetFamily: "windows"},
		"android-termux":      {OS: "linux", TargetArch: "aarch64", Libc: "bionic", IsAndroid: true, TargetFamily: "unix"},
		"freebsd-unsupported": {OS: "freebsd", TargetArch: "x86_64", TargetFamily: "unix"},
	}
}

func eligibleLabels(methods []*config.MethodCandidate, facts *platform.Facts) []string {
	var out []string
	for _, m := range methods {
		if m.When == nil || m.When.Match(facts) {
			out = append(out, m.Kind+"/"+m.Label)
		}
	}
	sort.Strings(out)
	return out
}

func TestHostMatrixEquivalence(t *testing.T) {
	a := parseManifestFile(t, testdata(t, "a_manifest.toml"))
	c := parseExpanded(t, testdata(t, "c_manifest.toml"), config.ParseManifest)
	// Elegibilidade github de br/omp coincide com as receitas reais.
	for host, facts := range hostMatrix() {
		for _, tool := range []string{"br", "omp", "wt"} {
			ea := eligibleLabels(a.Tools[tool].Methods, facts)
			ec := eligibleLabels(c.Tools[tool].Methods, facts)
			if !sameStrings(ea, ec) {
				t.Fatalf("host %s tool %s: elegíveis divergem A=%v C=%v", host, tool, ea, ec)
			}
		}
	}
	// Spot-checks contra o real: omp não tem github em windows.
	ompWin := eligibleLabels(a.Tools["omp"].Methods, hostMatrix()["windows-x86_64"])
	for _, e := range ompWin {
		if strings.HasPrefix(e, "github/") {
			t.Fatalf("omp tem github elegível em windows: %v", ompWin)
		}
	}
}

func TestFailClosed(t *testing.T) {
	compact := func(body string) []byte {
		return []byte("schema_version = 1\n[packages.br.github]\nrepo = \"o/r\"\nasset = \"a.tar.gz\"\nchecksum = \"sha256:auto\"\nscope = \"user\"\n" + body + "\n")
	}
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
		{"non-github-base", "[packages.br.cargo]\n  pkg = \"x\"\n  cases = [{label=\"y\", when={os=[\"linux\"]}}]", "github-only"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var doc []byte
			if tc.name == "non-github-base" {
				doc = []byte("schema_version = 1\n[packages.br]\nmethod_only = [\"cargo\"]\n" + tc.fragment + "\n")
			} else {
				doc = compact(tc.fragment)
			}
			_, _, err := ExpandCompact(doc)
			if err == nil {
				t.Fatalf("esperava erro contendo %q, obteve nil", tc.wantErr)
			}
			if !containsFold(err.Error(), tc.wantErr) {
				t.Fatalf("erro %q não contém %q", err.Error(), tc.wantErr)
			}
		})
	}
	// Colisão com método explícito: caso nunca substitui declaração existente.
	t.Run("collision-explicit", func(t *testing.T) {
		doc := []byte(`schema_version = 1
[packages.example.github]
repo = "owner/project"
asset = "base.tar.gz"
checksum = "sha256:auto"
scope = "user"
cases = [{label = "github-linux", when = {os = ["linux"]}}]

[packages.example.github-linux]
kind = "github"
repo = "outro/projeto"
asset = "outro.tar.gz"
checksum = "sha256:auto"
scope = "user"
when = {os = ["linux"]}
`)
		_, _, err := ExpandCompact(doc)
		if err == nil || !containsFold(err.Error(), "collides") {
			t.Fatalf("esperava erro de colisão, obteve %v", err)
		}
	})
}

func TestDeterminism(t *testing.T) {
	raw, err := os.ReadFile(testdata(t, "c_manifest.toml")) // #nosec G304 -- fixture-controlled path.
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
	// Goldens são obrigatórios: ausência é falha (criar só com
	// SPIKE_WRITE_GOLDENS=1), nunca criação silenciosa.
	write := os.Getenv("SPIKE_WRITE_GOLDENS") == "1"
	a := parseManifestFile(t, testdata(t, "a_manifest.toml"))
	c := parseExpanded(t, testdata(t, "c_manifest.toml"), config.ParseManifest)
	ha, _, err := lock.SnapshotIntentMetadata(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"br", "omp", "wt"} {
		golden := map[string]any{
			"tool":        tool,
			"candidates":  candidateSnapshot(a.Tools[tool].Methods),
			"methodsHash": ha[tool],
		}
		raw, _ := json.MarshalIndent(golden, "", "  ")
		path := filepath.Join("testdata", "goldens", tool+".json")
		if write {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { // #nosec G301 -- golden dir versionado; permissões herdadas do checkout.
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil { // #nosec G306 -- golden JSON versionado; sem segredos.
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path) // #nosec G304 -- golden path derivado de chave fixa.
		if os.IsNotExist(err) {
			t.Fatalf("golden ausente: %s (regenere com SPIKE_WRITE_GOLDENS=1)", path)
		}
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(raw)+"\n" {
			t.Fatalf("tool %s: golden diverge; comportamento atual mudou ou expansão quebrou", tool)
		}
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
