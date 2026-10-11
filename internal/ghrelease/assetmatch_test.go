package ghrelease

// Tests for explicit asset regexes ({ regex = "..." }) and the shared
// in-memory matcher. No network: catalogs are plain string slices.

import (
	"testing"
)

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
