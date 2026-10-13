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

func TestHTTPAdapterGPGSignatureFailureIsTerminalIntegrityError(t *testing.T) {
	skipIfNoGPG(t)
	setupGPGDir(t)
	const keyID = "terminal-gpg@example.com"
	genGPGKey(t, keyID)
	keyFile := exportGPGKeyFile(t, keyID)

	payload := []byte("payload bytes")
	digest := sha256.Sum256(payload)
	checksumFile := filepath.Join(t.TempDir(), "checksums.txt")
	if err := os.WriteFile(checksumFile, []byte(fmt.Sprintf("%x  demo.tar.gz\n", digest)), 0o600); err != nil {
		t.Fatal(err)
	}
	signatureFile := signFile(t, checksumFile)
	signature, err := os.ReadFile(signatureFile) // #nosec G304 -- signatureFile is created in a test-owned temporary directory.
	if err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(keyFile) // #nosec G304 -- keyFile is created in a test-owned temporary directory.
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/checksums":
			_, _ = w.Write([]byte("tampered checksum listing\n"))
		case "/checksums.sig":
			_, _ = w.Write(signature)
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
	artifactPath := filepath.Join(t.TempDir(), "artifact")
	if err := os.WriteFile(artifactPath, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	err = NewHTTPAdapter().verifyDownloadedArtifact(
		context.Background(), &run.OSExecRunner{}, &config.Tool{Name: "demo"}, method,
		server.URL+"/demo.tar.gz", artifactPath, false, "", false,
	)
	var terminalErr *exec.IntegrityError
	if !errors.As(err, &terminalErr) || !errors.Is(err, exec.ErrIntegrityMismatch) {
		t.Fatalf("signature failure = %v, want terminal exec.IntegrityError", err)
	}
	if !errors.Is(err, integrity.ErrSignatureVerification) {
		t.Fatalf("signature failure = %v, want underlying GPG verification classification", err)
	}
}

func TestGPGAvailabilityFailureIsNotTerminalSignatureMismatch(t *testing.T) {
	failedRunner := &run.FakeRunner{Err: fmt.Errorf("gpg executable unavailable")}
	err := GPGVerify(context.Background(), failedRunner, "/checksum", "/signature", "0123456789ABCDEF")
	if err == nil {
		t.Fatal("expected GPG availability failure")
	}
	if errors.Is(err, integrity.ErrSignatureVerification) {
		t.Fatalf("GPG availability failure was misclassified as a signature mismatch: %v", err)
	}
}
