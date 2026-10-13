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
	"github.com/Khorea1/depengine/internal/exec"
	"github.com/Khorea1/depengine/internal/integrity"
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
