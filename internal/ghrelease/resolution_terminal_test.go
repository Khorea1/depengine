package ghrelease

// Typed classification for terminal asset resolution failures
// (dep-1n0.10, plan section 4): zero/multiple catalog matches and invalid
// selectors carry errors.Is sentinels and an errors.As type, while transport
// and API failures stay unmarked so historic availability fallback applies.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

func terminalCatalogServer(t *testing.T, payload string, status int) *Resolver {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(ts.Close)
	return newTestResolver(ts.URL)
}

func TestResolveAssetURLZeroMatchIsTerminal(t *testing.T) {
	r := terminalCatalogServer(t, `{"tag_name":"v1.0.0","assets":[{"name":"checksums.txt"},{"name":"tool-linux-arm64"}]}`, http.StatusOK)

	_, _, err := r.ResolveAssetURL(context.Background(), "zero-owner/zero-repo", "tool-{os_any}-{arch_any}", "x86_64", "linux", "", run.OSExecRunner{})
	if err == nil {
		t.Fatal("expected zero-match error")
	}
	if !errors.Is(err, ErrNoAssetMatch) {
		t.Fatalf("err = %v, want errors.Is ErrNoAssetMatch", err)
	}
	var typed *AssetResolutionError
	if !errors.As(err, &typed) {
		t.Fatalf("err type = %T, want *AssetResolutionError", err)
	}
	if !strings.Contains(err.Error(), "available assets: checksums.txt, tool-linux-arm64") {
		t.Fatalf("err = %v, want the historic catalog diagnostic", err)
	}
}

func TestResolveAssetURLMultipleMatchesIsTerminal(t *testing.T) {
	r := terminalCatalogServer(t, `{"tag_name":"v1.0.0","assets":[{"name":"tool-linux-x86_64"},{"name":"tool-linux-amd64"},{"name":"tool-linux-arm64"}]}`, http.StatusOK)

	_, _, err := r.ResolveAssetURL(context.Background(), "multi-owner/multi-repo", "tool-{os_any}-{arch_any}", "x86_64", "linux", "", run.OSExecRunner{})
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !errors.Is(err, ErrAmbiguousAssetMatch) {
		t.Fatalf("err = %v, want errors.Is ErrAmbiguousAssetMatch", err)
	}
	var typed *AssetResolutionError
	if !errors.As(err, &typed) {
		t.Fatalf("err type = %T, want *AssetResolutionError", err)
	}
}

func TestResolveRegexAssetURLMatchFailuresAreTerminal(t *testing.T) {
	zero := terminalCatalogServer(t, `{"tag_name":"v1.0.0","assets":[{"name":"checksums.txt"},{"name":"tool-darwin-arm64"}]}`, http.StatusOK)
	_, _, err := zero.ResolveRegexAssetURL(context.Background(), "zero-owner/zero-repo", `tool-{os_any}-{arch_any}`, "x86_64", "linux", "", run.OSExecRunner{})
	if !errors.Is(err, ErrNoAssetMatch) {
		t.Fatalf("regex zero-match err = %v, want ErrNoAssetMatch", err)
	}

	multi := terminalCatalogServer(t, `{"tag_name":"v1.0.0","assets":[{"name":"tool-linux-x86_64","browser_download_url":"https://example.com/a"},{"name":"tool-linux-amd64","browser_download_url":"https://example.com/b"}]}`, http.StatusOK)
	_, _, err = multi.ResolveRegexAssetURL(context.Background(), "amb-owner/amb-repo", `tool-{os_any}-{arch_any}`, "x86_64", "linux", "", run.OSExecRunner{})
	if !errors.Is(err, ErrAmbiguousAssetMatch) {
		t.Fatalf("regex multi-match err = %v, want ErrAmbiguousAssetMatch", err)
	}
	var typed *AssetResolutionError
	if !errors.As(err, &typed) {
		t.Fatalf("err type = %T, want *AssetResolutionError", err)
	}
}

func TestResolveRegexAssetURLInvalidSelectorIsTerminal(t *testing.T) {
	r := newTestResolver("http://127.0.0.1:9")
	_, _, err := r.ResolveRegexAssetURL(context.Background(), "bad-owner/bad-repo", `tool-([`, "x86_64", "linux", "", run.OSExecRunner{})
	if !errors.Is(err, ErrInvalidAssetSelector) {
		t.Fatalf("err = %v, want errors.Is ErrInvalidAssetSelector", err)
	}
	if !strings.Contains(err.Error(), "invalid asset regex") {
		t.Fatalf("err = %v, want the historic compile diagnostic", err)
	}
}

func TestResolveAssetURLFetchFailureStaysRecoverable(t *testing.T) {
	r := terminalCatalogServer(t, `{}`, http.StatusInternalServerError)

	_, _, err := r.ResolveAssetURL(context.Background(), "flaky-owner/flaky-repo", "tool-{os_any}-{arch_any}", "x86_64", "linux", "", run.OSExecRunner{})
	if err == nil {
		t.Fatal("expected the API failure to surface")
	}
	for _, sentinel := range []error{ErrNoAssetMatch, ErrAmbiguousAssetMatch, ErrInvalidAssetSelector} {
		if errors.Is(err, sentinel) {
			t.Fatalf("err = %v must not match terminal sentinel %v: availability failures keep fallback", err, sentinel)
		}
	}
	var typed *AssetResolutionError
	if errors.As(err, &typed) {
		t.Fatalf("err = %v must not classify as terminal: availability failures keep fallback", err)
	}
}

func TestResolveAssetURLSingleMatchUnaffected(t *testing.T) {
	r := terminalCatalogServer(t, `{"tag_name": "v1.0.0", "assets": [{"name": "tool-linux-x86_64", "browser_download_url": "https://example.com/tool-linux-x86_64"}]}`, http.StatusOK)

	url, tag, err := r.ResolveAssetURL(context.Background(), "ok-owner/ok-repo", "tool-linux-{arch_any}", "x86_64", "linux", "", run.OSExecRunner{})
	if err != nil {
		t.Fatalf("ResolveAssetURL: %v", err)
	}
	if url != "https://example.com/tool-linux-x86_64" || tag != "v1.0.0" {
		t.Fatalf("got (%q, %q), want the single match", url, tag)
	}
}
