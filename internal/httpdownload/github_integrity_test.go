package httpdownload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/ghrelease"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

type githubAPIFixtureTransport struct {
	apiTarget *url.URL
}

func (t githubAPIFixtureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	if strings.EqualFold(clone.URL.Host, "api.github.com") {
		target := *clone.URL
		target.Scheme = t.apiTarget.Scheme
		target.Host = t.apiTarget.Host
		clone.URL = &target
	}
	return http.DefaultTransport.RoundTrip(clone)
}

func TestGitHubRepoAssetIntegritySurvivesResolutionAndBlocksMutation(t *testing.T) {
	skipIfNoGPG(t)
	setupGPGDir(t)
	genGPGKey(t, "repo-asset-integrity@example.test")

	publicKeyCmd := exec.Command("gpg", "--armor", "--export", "repo-asset-integrity@example.test") // #nosec G204 -- test exports its controlled ephemeral signing key.
	publicKey, err := publicKeyCmd.Output()
	if err != nil {
		t.Fatalf("export test key: %v", err)
	}

	payload := []byte("verified release payload\n")
	digest := sha256.Sum256(payload)
	validChecksum := []byte(hex.EncodeToString(digest[:]))
	wrongChecksum := []byte(strings.Repeat("0", sha256.Size*2))
	validSignature := signChecksumFixture(t, validChecksum)
	wrongChecksumSignature := signChecksumFixture(t, wrongChecksum)
	invalidSignature := append([]byte(nil), validSignature...)
	invalidSignature[len(invalidSignature)/2] ^= 0xff

	var checksumSidecar, signatureSidecar []byte
	var sidecarMu sync.RWMutex
	var serverURL atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/example/demo/releases/latest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"tag_name": "v1.0.0",
				"assets":   []map[string]string{{"name": "demo.bin", "browser_download_url": serverURL.Load().(string) + "/download/demo.bin"}},
			})
		case "/download/demo.bin":
			_, _ = w.Write(payload)
		case "/checksum":
			sidecarMu.RLock()
			checksum := append([]byte(nil), checksumSidecar...)
			sidecarMu.RUnlock()
			_, _ = w.Write(checksum)
		case "/checksum.sig":
			sidecarMu.RLock()
			signature := append([]byte(nil), signatureSidecar...)
			sidecarMu.RUnlock()
			_, _ = w.Write(signature)
		case "/key.asc":
			_, _ = w.Write(publicKey)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	serverURL.Store(server.URL)

	apiTarget, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	previousResolver := ghrelease.Default
	resolver := ghrelease.NewResolver()
	resolver.SetHTTPClient(&http.Client{Transport: githubAPIFixtureTransport{apiTarget: apiTarget}})
	ghrelease.Default = resolver
	t.Cleanup(func() { ghrelease.Default = previousResolver })

	for _, test := range []struct {
		name             string
		checksum         []byte
		signature        []byte
		wantInstallError string
	}{
		{name: "valid checksum and detached signature", checksum: validChecksum, signature: validSignature},
		{name: "checksum mismatch", checksum: wrongChecksum, signature: wrongChecksumSignature, wantInstallError: "checksum"},
		{name: "invalid detached signature", checksum: validChecksum, signature: invalidSignature, wantInstallError: "gpg"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			checksumSidecar = test.checksum
			signatureSidecar = test.signature

			tool := &config.Tool{Name: "demo"}
			method := &config.MethodCandidate{Kind: "github", Config: map[string]any{
				"repo": "example/demo", "asset": "demo.bin", "release": "latest",
				"checksum": "sha256:auto", "checksum_url": server.URL + "/checksum", "checksum_file_format": "raw",
				"signature_url": server.URL + "/checksum.sig", "signing_key": server.URL + "/key.asc",
				"extract_to": t.TempDir(), "binary": "demo", "sudo_required": false,
			}}
			intent, err := planner.BuildCandidateIntent(tool, method)
			if err != nil {
				t.Fatalf("BuildCandidateIntent() error: %v", err)
			}
			resolved, err := NewGitHubAdapter().ResolvePlan(context.Background(), run.OSExecRunner{}, tool, method, &intent)
			if err != nil {
				t.Fatalf("ResolvePlan() error: %v", err)
			}
			if len(resolved.Artifacts) != 1 {
				t.Fatalf("resolved artifacts = %+v, want one concrete artifact", resolved.Artifacts)
			}

			err = NewGitHubAdapter().InstallResolved(context.Background(), run.OSExecRunner{}, tool, method, resolved)
			target := filepath.Join(method.Config["extract_to"].(string), "demo")
			if test.wantInstallError != "" {
				if err == nil || !strings.Contains(strings.ToLower(err.Error()), test.wantInstallError) {
					t.Fatalf("InstallResolved() error = %v, want %q verification failure", err, test.wantInstallError)
				}
				if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
					t.Fatalf("failed verification mutated payload target: stat error = %v", statErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("InstallResolved() error: %v", err)
			}
			installed, err := os.ReadFile(target) // #nosec G304 -- target is the test-owned installation directory.
			if err != nil {
				t.Fatalf("installed payload: %v", err)
			}
			if string(installed) != string(payload) {
				t.Fatalf("installed payload = %q, want %q", installed, payload)
			}
		})
	}
}

func signChecksumFixture(t *testing.T, checksum []byte) []byte {
	t.Helper()
	checksumPath := filepath.Join(t.TempDir(), "checksum")
	if err := os.WriteFile(checksumPath, checksum, 0o600); err != nil {
		t.Fatalf("write checksum fixture: %v", err)
	}
	signaturePath := signFile(t, checksumPath)
	signature, err := os.ReadFile(signaturePath) // #nosec G304 -- signaturePath is produced by this test's signer fixture.
	if err != nil {
		t.Fatalf("read signature fixture: %v", err)
	}
	return signature
}
