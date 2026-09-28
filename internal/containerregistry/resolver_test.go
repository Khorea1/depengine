package containerregistry

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveTagDigestUsesRegistryDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead || r.URL.Path != "/v2/team/tool/manifests/stable" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Docker-Content-Digest", digest)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	source := strings.TrimPrefix(server.URL, "http://") + "/team/tool"
	got, err := ResolveTagDigest(context.Background(), source, "stable", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != digest {
		t.Fatalf("digest = %q, want %q", got, digest)
	}
}

func TestResolveTagDigestBearerChallengeUsesCredentialsOnlyAtTokenEndpoint(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/team/tool/manifests/latest":
			if r.Header.Get("Authorization") == "Bearer registry-token" {
				w.Header().Set("Docker-Content-Digest", digest)
				w.WriteHeader(http.StatusOK)
				return
			}
			if r.Header.Get("Authorization") != "" {
				t.Fatalf("manifest request leaked preemptive credential: %q", r.Header.Get("Authorization"))
			}
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="test-registry",scope="repository:team/tool:pull"`, server.URL))
			w.WriteHeader(http.StatusUnauthorized)
		case "/token":
			user, pass, ok := r.BasicAuth()
			if !ok || user != "ci-user" || pass != "registry-secret" {
				t.Fatalf("token BasicAuth = (%q, %q, %t)", user, pass, ok)
			}
			if r.URL.Query().Get("service") != "test-registry" || r.URL.Query().Get("scope") != "repository:team/tool:pull" {
				t.Fatalf("token query = %v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"token":"registry-token"}`))
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	source := strings.TrimPrefix(server.URL, "http://") + "/team/tool"
	got, err := ResolveTagDigest(context.Background(), source, "", &Credentials{Username: "ci-user", Secret: "registry-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if got != digest {
		t.Fatalf("digest = %q, want %q", got, digest)
	}
}

func TestResolveTagDigestFallsBackToManifestBodyHash(t *testing.T) {
	body := []byte(`{"schemaVersion":2,"mediaType":"application/vnd.oci.image.manifest.v1+json"}`)
	sum := sha256.Sum256(body)
	want := "sha256:" + hex.EncodeToString(sum[:])
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()

	source := strings.TrimPrefix(server.URL, "http://") + "/team/tool"
	got, err := ResolveTagDigest(context.Background(), source, "edge", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("digest = %q, want %q", got, want)
	}
}

func TestSplitRepositoryDockerHubDefaults(t *testing.T) {
	for _, tc := range []struct {
		source, registry, repository string
	}{
		{"redis", "registry-1.docker.io", "library/redis"},
		{"owner/tool", "registry-1.docker.io", "owner/tool"},
		{"ghcr.io/owner/tool", "ghcr.io", "owner/tool"},
		{"docker.io/library/redis", "registry-1.docker.io", "library/redis"},
		{"docker.io/redis", "registry-1.docker.io", "library/redis"},
		{"index.docker.io/library/redis", "registry-1.docker.io", "library/redis"},
		{"index.docker.io/redis", "registry-1.docker.io", "library/redis"},
		{"registry-1.docker.io/library/redis", "registry-1.docker.io", "library/redis"},
		{"registry-1.docker.io/redis", "registry-1.docker.io", "library/redis"},
		{"localhost:5000/tool", "localhost:5000", "tool"},
	} {
		registry, repository := splitRepository(tc.source)
		if registry != tc.registry || repository != tc.repository {
			t.Fatalf("splitRepository(%q) = (%q, %q), want (%q, %q)", tc.source, registry, repository, tc.registry, tc.repository)
		}
	}
}

func TestHTTPClientRejectsHTTPSDowngradeAndStripsCrossHostAuthorization(t *testing.T) {
	client := newHTTPClient()

	previous, err := http.NewRequest(http.MethodGet, "https://registry.example/v2/tool/manifests/latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	previous.Header.Set("Authorization", "Bearer secret")
	downgrade, err := http.NewRequest(http.MethodGet, "http://registry.example/v2/tool/manifests/latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(downgrade, []*http.Request{previous}); err == nil || !strings.Contains(err.Error(), "refusing HTTPS redirect") {
		t.Fatalf("downgrade redirect error = %v", err)
	}

	crossHost, err := http.NewRequest(http.MethodGet, "https://cdn.example/manifests/latest", nil)
	if err != nil {
		t.Fatal(err)
	}
	crossHost.Header.Set("Authorization", "Bearer secret")
	if err := client.CheckRedirect(crossHost, []*http.Request{previous}); err != nil {
		t.Fatal(err)
	}
	if got := crossHost.Header.Get("Authorization"); got != "" {
		t.Fatalf("cross-host redirect kept Authorization = %q", got)
	}
}
