package ghrelease

// Tests for explicit asset regexes ({ regex = "..." }) and the shared
// in-memory matcher. No network: catalogs are plain string slices.

import (
	"math/rand"
	"testing"
)

func newDeterministicRNG(seed int64) *rand.Rand {
	// #nosec G404 -- deterministic shuffle is the test's subject (order
	// independence); cryptographic randomness would defeat the purpose.
	return rand.New(rand.NewSource(seed))
}

func equalStrings(a, b []string) bool {
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

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		if seen[s] == 0 {
			return false
		}
		seen[s]--
	}
	return true
}

func TestCompileExplicitRegexExpandsPlaceholders(t *testing.T) {
	re, err := CompileExplicitRegex(`atlas-{version}-(?:linux|gnu-linux)-{arch_any}\.tar\.gz`, "v1.2.3", "x86_64", "linux")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"atlas-1.2.3-linux-amd64.tar.gz", "atlas-v1.2.3-gnu-linux-x86_64.tar.gz"} {
		if !re.MatchString(name) {
			t.Errorf("must match %q", name)
		}
	}
	for _, name := range []string{"atlas-1.2.4-linux-amd64.tar.gz", "atlas-1.2.3-darwin-amd64.tar.gz"} {
		if re.MatchString(name) {
			t.Errorf("must not match %q", name)
		}
	}
}

func TestCompileExplicitRegexIsAnchoredCaseInsensitive(t *testing.T) {
	re, err := CompileExplicitRegex(`tool-{os_any}\.zip`, "v1.0.0", "x86_64", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("TOOL-WINDOWS.ZIP") {
		t.Error("must match case-insensitively")
	}
	if re.MatchString("prefix-tool-windows.zip") || re.MatchString("tool-windows.zip.suffix") {
		t.Error("must be anchored")
	}
	// Hand-written alternations only match whole anchored names; prefer
	// {os_any}, which enumerates the exact known spellings per host.
	narrow, err := CompileExplicitRegex(`tool-(win|windows)\.zip`, "v1.0.0", "x86_64", "windows")
	if err != nil {
		t.Fatal(err)
	}
	if !narrow.MatchString("tool-win.zip") || narrow.MatchString("tool-darwin.zip") {
		t.Error("anchored alternation must match win, never darwin")
	}
}

func TestCompileExplicitRegexRejectsInvalid(t *testing.T) {
	if _, err := CompileExplicitRegex(`tool-([.zip`, "v1.0.0", "x86_64", "linux"); err == nil {
		t.Fatal("expected invalid regex error")
	}
}

func TestCompileExplicitRegexUnknownTokensPassThrough(t *testing.T) {
	// {2} is a regex quantifier, not a placeholder: it must survive.
	re, err := CompileExplicitRegex(`ab{2}\.tar\.gz`, "v1.0.0", "x86_64", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("abb.tar.gz") || re.MatchString("ab.tar.gz") {
		t.Error("quantifier must work")
	}
}

func TestMatchAssetNames(t *testing.T) {
	re, err := CompileExplicitRegex(`tool-{os_any}-{arch_any}`, "v1.0.0", "x86_64", "linux")
	if err != nil {
		t.Fatal(err)
	}
	got := MatchAssetNames(re, []string{"tool-linux-amd64", "tool-darwin-amd64", "checksums.txt"})
	if len(got) != 1 || got[0] != "tool-linux-amd64" {
		t.Fatalf("matched=%v", got)
	}
	if got := MatchAssetNames(re, nil); len(got) != 0 {
		t.Fatalf("empty catalog must match nothing, got %v", got)
	}
}

func TestMatchAssetNamesOrderIndependent(t *testing.T) {
	// A ordem do catálogo nunca decide: embaralhar não muda o resultado.
	catalog := []string{"checksums.txt", "tool-linux-amd64.tar.gz", "tool-darwin-arm64.tar.gz", "tool-linux-arm64.tar.gz"}
	re, err := CompileLiteralPattern("tool-{os_any}-{arch_any}.tar.gz", "v1.0.0", "x86_64", "linux")
	if err != nil {
		t.Fatal(err)
	}
	want := MatchAssetNames(re, catalog)
	if len(want) != 1 || want[0] != "tool-linux-amd64.tar.gz" {
		t.Fatalf("matched=%v", want)
	}
	for seed := int64(0); seed < 5; seed++ {
		shuffled := append([]string(nil), catalog...)
		rng := newDeterministicRNG(seed)
		for i := len(shuffled) - 1; i > 0; i-- {
			j := rng.Intn(i + 1)
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}
		if got := MatchAssetNames(re, shuffled); !equalStrings(got, want) {
			t.Fatalf("seed %d: matched=%v, want %v", seed, got, want)
		}
	}
	// Ambiguidade também é estável sob permutação: mesmo conjunto.
	amb, err := CompileExplicitRegex(`tool-{os_any}-{arch_any}`, "v1.0.0", "x86_64", "linux")
	if err != nil {
		t.Fatal(err)
	}
	multi := []string{"tool-linux-amd64", "tool-linux-x86_64", "tool-darwin-amd64"}
	first := MatchAssetNames(amb, multi)
	if len(first) != 2 {
		t.Fatalf("matched=%v, want 2", first)
	}
	for seed := int64(0); seed < 5; seed++ {
		shuffled := append([]string(nil), multi...)
		rng := newDeterministicRNG(seed)
		for i := len(shuffled) - 1; i > 0; i-- {
			j := rng.Intn(i + 1)
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		}
		if got := MatchAssetNames(amb, shuffled); !sameSet(got, first) {
			t.Fatalf("seed %d: matched=%v, want same set %v", seed, got, first)
		}
	}
}

func TestSynonymCoverage(t *testing.T) {
	// Toda grafia conhecida das tabelas resolve pelo placeholder.
	for arch, syns := range archSynonyms {
		re, err := CompileLiteralPattern("tool-{arch_any}.tar.gz", "v1.0.0", arch, "linux")
		if err != nil {
			t.Fatal(err)
		}
		for _, syn := range syns {
			if got := MatchAssetNames(re, []string{"tool-" + syn + ".tar.gz"}); len(got) != 1 {
				t.Fatalf("arch %q spelling %q unmatched", arch, syn)
			}
		}
	}
	for os, syns := range osSynonyms {
		re, err := CompileLiteralPattern("tool-{os_any}.tar.gz", "v1.0.0", "x86_64", os)
		if err != nil {
			t.Fatal(err)
		}
		for _, syn := range syns {
			if got := MatchAssetNames(re, []string{"tool-" + syn + ".tar.gz"}); len(got) != 1 {
				t.Fatalf("os %q spelling %q unmatched", os, syn)
			}
		}
	}
	// Host desconhecido ainda casa literalmente (fallback, sem falhar).
	re, err := CompileLiteralPattern("tool-{arch_any}.tar.gz", "v1.0.0", "riscv64", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if got := MatchAssetNames(re, []string{"tool-riscv64.tar.gz"}); len(got) != 1 {
		t.Fatalf("unknown arch must match literally, got %v", got)
	}
}

func TestLiteralPatternUnchanged(t *testing.T) {
	// The literal template path keeps QuoteMeta behavior: dots are literal.
	re, err := assetPatternRegexp("tool-{os_any}-{arch_any}.tar.gz", "v1.0.0", "x86_64", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("tool-linux-amd64.tar.gz") || re.MatchString("tool-linux-amd64Xtar.gz") {
		t.Error("literal dots must match literally")
	}
	if matched := MatchAssetNames(re, []string{"tool-linux-amd64.tar.gz"}); len(matched) != 1 {
		t.Fatalf("matched=%v", matched)
	}
}

func TestCompileExplicitRegexTopLevelAlternationStaysAnchored(t *testing.T) {
	// A top-level `a|b` must compile as ^(?:a|b)$: without the group the
	// branches escape the anchors (^a|b$ matches "xa" and "bx").
	re, err := CompileExplicitRegex(`tool-win\.zip|tool-windows\.zip`, "v1.0.0", "x86_64", "windows")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tool-win.zip", "tool-windows.zip"} {
		if !re.MatchString(name) {
			t.Errorf("must match exact name %q", name)
		}
	}
	for _, name := range []string{"tool-win.zip.evil", "evil-tool-windows.zip", "xxtool-win.zip", "tool-windows.zipxx"} {
		if re.MatchString(name) {
			t.Errorf("must not partially match %q", name)
		}
	}
}

func TestLiteralPatternPipeStaysLiteral(t *testing.T) {
	// The literal template path escapes everything first, so `|` never
	// becomes an alternation there; the anchoring fix must not change that.
	re, err := CompileLiteralPattern("tool-a|b.tar.gz", "v1.0.0", "x86_64", "linux")
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("tool-a|b.tar.gz") {
		t.Error("literal pipe must match literally")
	}
	if re.MatchString("tool-a") || re.MatchString("b.tar.gz") {
		t.Error("literal pipe must not act as alternation")
	}
}
