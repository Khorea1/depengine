package httpdownload

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	"github.com/Khorea1/depengine/internal/downloadcache"
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/integrity"
	"github.com/Khorea1/depengine/internal/plan"
	"github.com/Khorea1/depengine/internal/platform"
	"github.com/Khorea1/depengine/internal/run"
)

func TestCachedChecksumRefetchSignatureFailureIsTerminal(t *testing.T) {
	skipIfNoGPG(t)
	setupGPGDir(t)
	const keyID = "refetch-gpg@example.com"
	genGPGKey(t, keyID)
	keyFile := exportGPGKeyFile(t, keyID)
	key, err := os.ReadFile(keyFile) // #nosec G304 -- test-owned temporary key.
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte("fresh payload")
	digest := sha256.Sum256(payload)
	checksumPath := filepath.Join(t.TempDir(), "checksums.txt")
	checksumListing := []byte(fmt.Sprintf("%x  demo.tar.gz\n", digest))
	if err := os.WriteFile(checksumPath, checksumListing, 0o600); err != nil {
		t.Fatal(err)
	}
	validSignaturePath := signFile(t, checksumPath)
	validSignature, err := os.ReadFile(validSignaturePath) // #nosec G304 -- test-owned temporary signature.
	if err != nil {
		t.Fatal(err)
	}
	badSignature := append([]byte(nil), validSignature...)
	badSignature[len(badSignature)/2] ^= 0x01

	var payloadRequests, checksumRequests int
	forceBadSignature := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/demo.tar.gz":
			payloadRequests++
			_, _ = w.Write(payload)
		case "/checksums":
			checksumRequests++
			_, _ = w.Write(checksumListing)
		case "/checksums.sig":
			if checksumRequests == 1 && !forceBadSignature {
				_, _ = w.Write(validSignature)
			} else {
				_, _ = w.Write(badSignature)
			}
		case "/key":
			_, _ = w.Write(key)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	method := &config.MethodCandidate{Kind: "http", Config: map[string]any{
		"checksum":             "sha256:auto",
		"checksum_url":         server.URL + "/checksums",
		"checksum_file_format": "sha256sum",
		"signature_url":        server.URL + "/checksums.sig",
		"signing_key":          server.URL + "/key",
	}}

	stalePath := filepath.Join(t.TempDir(), "cached-payload")
	if err := os.WriteFile(stalePath, []byte("corrupt cached payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	err = NewHTTPAdapter().verifyDownloadedArtifact(context.Background(), run.OSExecRunner{}, &config.Tool{Name: "demo"}, method, server.URL+"/demo.tar.gz", stalePath, true, "", false)
	if err == nil {
		t.Fatal("verification succeeded after the refetched checksum sidecar had an invalid signature")
	}
	if payloadRequests != 1 || checksumRequests != 2 {
		t.Fatalf("payload requests = %d, checksum requests = %d; want one refetch and two sidecar verifications", payloadRequests, checksumRequests)
	}
	var terminalErr *exec.IntegrityError
	if !errors.As(err, &terminalErr) || !exec.IsTerminalMethodError(err) {
		t.Fatalf("refetch signature failure = %v, want terminal IntegrityError", err)
	}
	if !errors.Is(err, integrity.ErrSignatureVerification) {
		t.Fatalf("refetch signature failure = %v, want preserved ErrSignatureVerification", err)
	}

	forceBadSignature = true
	for _, fromCache := range []bool{false, true} {
		name := "uncached"
		if fromCache {
			name = "cached-first-verification"
		}
		t.Run(name, func(t *testing.T) {
			verifiedPath := filepath.Join(t.TempDir(), "verified-payload")
			if err := os.WriteFile(verifiedPath, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			previousPayloadRequests, previousChecksumRequests := payloadRequests, checksumRequests
			err := NewHTTPAdapter().verifyDownloadedArtifact(context.Background(), run.OSExecRunner{}, &config.Tool{Name: "demo"}, method, server.URL+"/demo.tar.gz", verifiedPath, fromCache, "", false)
			if err == nil {
				t.Fatal("verification succeeded with an invalid initial checksum signature")
			}
			var terminalErr *exec.IntegrityError
			if !errors.As(err, &terminalErr) || !exec.IsTerminalMethodError(err) {
				t.Fatalf("initial signature failure = %v, want terminal IntegrityError", err)
			}
			if !errors.Is(err, integrity.ErrSignatureVerification) {
				t.Fatalf("initial signature failure = %v, want preserved ErrSignatureVerification", err)
			}
			if payloadRequests != previousPayloadRequests || checksumRequests != previousChecksumRequests+1 {
				t.Fatalf("payload requests = %d, checksum requests = %d; want no payload refetch and exactly one checksum verification", payloadRequests, checksumRequests)
			}
		})
	}
}

type fallbackInstallRecorder struct {
	installs int
}

func (a *fallbackInstallRecorder) Kind() string                               { return "fallback" }
func (a *fallbackInstallRecorder) Available(context.Context, run.Runner) bool { return true }
func (a *fallbackInstallRecorder) ResolvePlan(_ context.Context, _ run.Runner, _ *config.Tool, _ *config.MethodCandidate, intent *plan.ResolvedInstallPlan) (*plan.ResolvedInstallPlan, error) {
	resolved := intent.Clone()
	return &resolved, nil
}
func (a *fallbackInstallRecorder) Observe(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) (plan.Observation, error) {
	return plan.Observation{}, nil
}
func (a *fallbackInstallRecorder) InstallResolved(context.Context, run.Runner, *config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan) error {
	a.installs++
	return nil
}
func (*fallbackInstallRecorder) Remove(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) error {
	return nil
}
func (*fallbackInstallRecorder) CanRemove() bool { return false }
func (*fallbackInstallRecorder) CheckAvailable(context.Context, run.Runner, *config.Tool, *config.MethodCandidate) bool {
	return true
}
func (*fallbackInstallRecorder) CheckHostCompatibility(*config.Tool, *config.MethodCandidate, *plan.ResolvedInstallPlan, *platform.Facts, string) error {
	return nil
}

var _ exec.AdapterV2 = (*fallbackInstallRecorder)(nil)

func TestExecutorDoesNotFallbackAfterCachedGPGRefetchFailure(t *testing.T) {
	skipIfNoGPG(t)
	setupGPGDir(t)
	const keyID = "executor-refetch-gpg@example.com"
	genGPGKey(t, keyID)
	keyFile := exportGPGKeyFile(t, keyID)
	key, err := os.ReadFile(keyFile) // #nosec G304 -- test-owned temporary key.
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte("fresh payload")
	digest := sha256.Sum256(payload)
	checksumListing := []byte(fmt.Sprintf("%x  demo.tar.gz\n", digest))
	checksumPath := filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(checksumPath, checksumListing, 0o600); err != nil {
		t.Fatal(err)
	}
	validSignaturePath := signFile(t, checksumPath)
	validSignature, err := os.ReadFile(validSignaturePath) // #nosec G304 -- test-owned temporary signature.
	if err != nil {
		t.Fatal(err)
	}
	badSignature := append([]byte(nil), validSignature...)
	badSignature[len(badSignature)/2] ^= 0x01

	var payloadRequests, checksumRequests, signatureRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/demo.tar.gz":
			payloadRequests++
			_, _ = w.Write(payload)
		case "/checksums":
			checksumRequests++
			_, _ = w.Write(checksumListing)
		case "/checksums.sig":
			signatureRequests++
			if checksumRequests == 1 {
				_, _ = w.Write(validSignature)
			} else {
				_, _ = w.Write(badSignature)
			}
		case "/key":
			_, _ = w.Write(key)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	artifactURL := server.URL + "/demo.tar.gz"
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	stalePath := filepath.Join(t.TempDir(), "stale-cached-payload")
	if err := os.WriteFile(stalePath, []byte("corrupt cached payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := downloadcache.Store(artifactURL, stalePath); err != nil {
		t.Fatalf("seed cached payload: %v", err)
	}

	fallback := &fallbackInstallRecorder{}
	executor := exec.New()
	exec.WithRunner(run.OSExecRunner{})(executor)
	exec.WithAdapters(NewHTTPAdapter(), fallback)(executor)
	exec.WithFacts(&platform.Facts{OS: "linux", TargetArch: "x86_64", TargetFamily: "unix"})(executor)
	schema := &config.Schema{
		Defaults: config.Defaults{MethodOrder: []string{"http", "fallback"}},
		Tools: map[string]*config.Tool{
			"demo": {
				Name: "demo",
				Methods: []*config.MethodCandidate{
					{Kind: "http", Config: map[string]any{
						"url": artifactURL, "checksum": "sha256:auto",
						"checksum_url": server.URL + "/checksums", "checksum_file_format": "sha256sum",
						"signature_url": server.URL + "/checksums.sig", "signing_key": server.URL + "/key",
						"binary": "depengine-gpg-refetch-test", "extract_to": t.TempDir(), "sudo_required": false,
					}},
					{Kind: "fallback", Config: map[string]any{"pkg": "fallback-demo"}},
				},
			},
		},
	}

	report, err := executor.Execute(context.Background(), schema, "linux")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(report.Tools) != 1 || report.Tools[0].Status != exec.StatusFailed {
		t.Fatalf("tool result = %+v, want terminal failure", report.Tools)
	}
	if len(report.Tools[0].Methods) != 1 {
		t.Fatalf("attempted methods = %+v, want no fallback method", report.Tools[0].Methods)
	}
	if fallback.installs != 0 {
		t.Fatalf("fallback installs = %d, want 0 after cached GPG refetch failure", fallback.installs)
	}
	if payloadRequests != 1 || checksumRequests != 2 || signatureRequests != 2 {
		t.Fatalf("payload requests = %d, checksum requests = %d, signature requests = %d; want one artifact refetch and two checksum/signature verifications", payloadRequests, checksumRequests, signatureRequests)
	}
}
