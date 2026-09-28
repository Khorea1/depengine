package localartifactadapter_test

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
	localartifactadapter "github.com/Khorea1/depengine/internal/localartifactadapter"
	"github.com/Khorea1/depengine/internal/planner"
	"github.com/Khorea1/depengine/internal/run"
)

const signatureTestFingerprint = "0123456789ABCDEF0123456789ABCDEF01234567"

// fakeGPGRunner replays the exact gpg dialogue the isolated identity-check
// path performs for a file:// signing key: --show-key reports the canned
// fingerprint, --import succeeds, and --verify emits VALIDSIG for it.
// verifyFails flips only the --verify verdict to simulate a bad signature;
// gpgPresent false simulates gpg missing from PATH.
type fakeGPGRunner struct {
	calls       []run.FakeCall
	fpr         string
	gpgPresent  bool
	verifyFails bool
}

func (r *fakeGPGRunner) LookPath(_ context.Context, name string) bool {
	return name == "gpg" && r.gpgPresent
}

func (r *fakeGPGRunner) Run(_ context.Context, name string, args ...string) run.Result {
	r.calls = append(r.calls, run.FakeCall{Name: name, Args: slices.Clone(args)})
	if name == "which" {
		if r.gpgPresent {
			return run.Result{}
		}
		return run.Result{ExitCode: 1}
	}
	if name != "gpg" {
		return run.Result{}
	}
	if slices.Contains(args, "--show-key") {
		return run.Result{Stdout: []byte("fpr:::::::::" + r.fpr + ":\n")}
	}
	if slices.Contains(args, "--status-fd=1") {
		if r.verifyFails {
			return run.Result{ExitCode: 1, Stderr: []byte("BAD signature")}
		}
		return run.Result{Stdout: []byte("[GNUPG:] VALIDSIG " + r.fpr + " 0 1 2 3 4 5 6 " + r.fpr + "\n")}
	}
	return run.Result{}
}

func canonicalExistingPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

func (r *fakeGPGRunner) sawGPGVerify(source, sig string) bool {
	source = canonicalExistingPath(source)
	sig = canonicalExistingPath(sig)
	for _, call := range r.calls {
		if call.Name != "gpg" || !slices.Contains(call.Args, "--status-fd=1") {
			continue
		}
		seenSource, seenSig := false, false
		for _, arg := range call.Args {
			canonical := canonicalExistingPath(arg)
			seenSource = seenSource || canonical == source
			seenSig = seenSig || canonical == sig
		}
		if seenSource && seenSig {
			return true
		}
	}
	return false
}

// writeSignatureProject vendors a payload, a detached signature file, and a
// key file, returning the project root. File contents are arbitrary: the fake
// gpg dialogue above stands in for real cryptography.
func writeSignatureProject(t *testing.T, payload []byte) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "vendor"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "tool"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "tool.sig"), []byte("detached signature"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "vendor", "key.asc"), []byte("public key"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func fileURL(path string) string {
	slashPath := filepath.ToSlash(path)
	if !strings.HasPrefix(slashPath, "/") {
		slashPath = "/" + slashPath
	}
	return (&url.URL{Scheme: "file", Path: slashPath}).String()
}

func signatureMethod(root, dest string) (*config.Tool, *config.MethodCandidate) {
	tool := &config.Tool{Name: "tool"}
	mc := &config.MethodCandidate{Kind: "local", ProjectRoot: root, Config: map[string]any{
		"local_path":     "vendor/tool",
		"signature_path": "vendor/tool.sig",
		"signing_key":    fileURL(filepath.Join(root, "vendor", "key.asc")),
		"install_dir":    dest,
	}}
	return tool, mc
}

func TestLocalInstallVerifiesDetachedSignature(t *testing.T) {
	root := writeSignatureProject(t, []byte("signed payload"))
	dest := t.TempDir()
	tool, mc := signatureMethod(root, dest)
	runner := &fakeGPGRunner{fpr: signatureTestFingerprint, gpgPresent: true}
	adapter := localartifactadapter.NewAdapter()
	if err := adapter.Install(context.Background(), runner, tool, mc); err != nil {
		t.Fatalf("Install() error: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "tool")) // #nosec G304 -- fixture-controlled path under t.TempDir.
	if err != nil || string(got) != "signed payload" {
		t.Fatalf("installed payload = %q, err=%v", got, err)
	}
	source := filepath.Join(root, "vendor", "tool")
	sig := filepath.Join(root, "vendor", "tool.sig")
	if !runner.sawGPGVerify(source, sig) {
		t.Fatalf("gpg never verified %q against %q: %#v", source, sig, runner.calls)
	}
}

func TestLocalInstallResolvedVerifiesDetachedSignature(t *testing.T) {
	root := writeSignatureProject(t, []byte("signed payload"))
	dest := t.TempDir()
	tool, mc := signatureMethod(root, dest)
	adapter := localartifactadapter.NewAdapter()
	intent, err := planner.BuildCandidateIntent(tool, mc)
	if err != nil {
		t.Fatalf("BuildCandidateIntent() error: %v", err)
	}
	resolved, err := adapter.ResolvePlan(context.Background(), &run.FakeRunner{}, tool, mc, &intent)
	if err != nil {
		t.Fatalf("ResolvePlan() error: %v", err)
	}
	if len(resolved.Artifacts) != 1 || resolved.Artifacts[0].SignaturePath != "vendor/tool.sig" || resolved.Artifacts[0].SigningKey == "" {
		t.Fatalf("resolved signature policy = %+v", resolved.Artifacts)
	}

	// Execution must use the resolved plan, not mutable method configuration.
	mc.Config["signature_path"] = "../changed-after-resolution.sig"
	mc.Config["signing_key"] = "changed-after-resolution"

	runner := &fakeGPGRunner{fpr: signatureTestFingerprint, gpgPresent: true}
	if err := adapter.InstallResolved(context.Background(), runner, tool, mc, resolved); err != nil {
		t.Fatalf("InstallResolved() error: %v", err)
	}
	if !runner.sawGPGVerify(filepath.Join(root, "vendor", "tool"), filepath.Join(root, "vendor", "tool.sig")) {
		t.Fatalf("gpg verification missing: %#v", runner.calls)
	}
}
func TestLocalInstallRejectsBadDetachedSignature(t *testing.T) {
	root := writeSignatureProject(t, []byte("signed payload"))
	dest := t.TempDir()
	tool, mc := signatureMethod(root, dest)
	runner := &fakeGPGRunner{fpr: signatureTestFingerprint, gpgPresent: true, verifyFails: true}
	if err := localartifactadapter.NewAdapter().Install(context.Background(), runner, tool, mc); err == nil {
		t.Fatal("expected bad signature to fail closed, got nil")
	} else if !strings.Contains(err.Error(), "signature") {
		t.Fatalf("error = %q, want signature failure", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "tool")); !os.IsNotExist(err) {
		t.Fatal("payload was materialized despite signature failure")
	}
}

func TestLocalInstallRejectsMissingGPG(t *testing.T) {
	root := writeSignatureProject(t, []byte("signed payload"))
	dest := t.TempDir()
	tool, mc := signatureMethod(root, dest)
	runner := &fakeGPGRunner{fpr: signatureTestFingerprint}
	if err := localartifactadapter.NewAdapter().Install(context.Background(), runner, tool, mc); err == nil {
		t.Fatal("expected missing gpg to fail closed, got nil")
	} else if !strings.Contains(err.Error(), "gpg") {
		t.Fatalf("error = %q, want gpg failure", err)
	}
}

func TestLocalInstallRejectsMissingSignatureFile(t *testing.T) {
	root := writeSignatureProject(t, []byte("signed payload"))
	if err := os.Remove(filepath.Join(root, "vendor", "tool.sig")); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	tool, mc := signatureMethod(root, dest)
	runner := &fakeGPGRunner{fpr: signatureTestFingerprint, gpgPresent: true}
	if err := localartifactadapter.NewAdapter().Install(context.Background(), runner, tool, mc); err == nil {
		t.Fatal("expected missing signature file to fail closed, got nil")
	} else if !strings.Contains(err.Error(), "signature_path") {
		t.Fatalf("error = %q, want signature_path failure", err)
	}
}

func TestLocalInstallRejectsEscapingSignaturePath(t *testing.T) {
	root := writeSignatureProject(t, []byte("signed payload"))
	dest := t.TempDir()
	tool, mc := signatureMethod(root, dest)
	mc.Config["signature_path"] = "../evil.sig"
	runner := &fakeGPGRunner{fpr: signatureTestFingerprint, gpgPresent: true}
	if err := localartifactadapter.NewAdapter().Install(context.Background(), runner, tool, mc); err == nil {
		t.Fatal("expected escaping signature path to fail closed, got nil")
	}
	for _, call := range runner.calls {
		if call.Name == "gpg" {
			t.Fatalf("gpg must not run for an escaping signature path: %#v", runner.calls)
		}
	}
}

func TestLocalInstallRejectsHalfConfiguredSignature(t *testing.T) {
	cases := map[string]map[string]any{
		"signature without key": {"signature_path": "vendor/tool.sig"},
		"key without signature": {"signing_key": "file:///vendor/key.asc"},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			root := writeSignatureProject(t, []byte("signed payload"))
			dest := t.TempDir()
			tool := &config.Tool{Name: "tool"}
			cfg := map[string]any{"local_path": "vendor/tool", "install_dir": dest}
			for key, value := range extra {
				cfg[key] = value
			}
			mc := &config.MethodCandidate{Kind: "local", ProjectRoot: root, Config: cfg}
			runner := &fakeGPGRunner{fpr: signatureTestFingerprint, gpgPresent: true}
			if err := localartifactadapter.NewAdapter().Install(context.Background(), runner, tool, mc); err == nil {
				t.Fatal("expected half-configured signature to fail closed, got nil")
			}
		})
	}
}
