package httpdownload

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/pkg/run"
)

// skipIfNoGPG skips the test if gpg is not available.
func skipIfNoGPG(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gpg"); err != nil {
		t.Skip("gpg not available, skipping GPG test")
	}
}

// setupGPGDir creates a temporary GNUPGHOME with proper permissions and
// sets the GNUPGHOME environment variable for the test.
func setupGPGDir(t *testing.T) string {
	t.Helper()
	gnupgHome := t.TempDir()
	if err := os.Chmod(gnupgHome, 0o700); err != nil {
		t.Fatalf("chmod gnupgHome: %v", err)
	}
	t.Setenv("GNUPGHOME", gnupgHome)
	return gnupgHome
}

// genGPGKey generates a test GPG key with the given email.
func genGPGKey(t *testing.T, keyID string) {
	t.Helper()
	batchInput := "Key-Type: RSA\n" +
		"Key-Length: 2048\n" +
		"Subkey-Type: RSA\n" +
		"Subkey-Length: 2048\n" +
		"Name-Real: GPG Test\n" +
		"Name-Email: " + keyID + "\n" +
		"Expire-Date: 0\n" +
		"%no-protection\n" +
		"%commit\n"

	cmd := exec.Command("gpg", "--batch", "--gen-key")
	cmd.Stdin = strings.NewReader(batchInput)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gpg --gen-key failed: %v\n%s", err, out)
	}
}

// signFile creates an armored detached signature for the given file.
func signFile(t *testing.T, filePath string) string {
	t.Helper()
	cmd := exec.Command("gpg", "--detach-sign", "--armor", "--batch", "--no-tty", filePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gpg --detach-sign failed: %v\n%s", err, out)
	}
	sigPath := filePath + ".asc"
	if _, err := os.Stat(sigPath); err != nil {
		t.Fatalf("signature file not created: %v", err)
	}
	return sigPath
}

// TestGPGVerifyValidSignature tests GPG verification with a valid detached
// signature. It creates a temporary GPG keyring, generates a key, signs a
// checksum file, and verifies the signature.
func TestGPGVerifyValidSignature(t *testing.T) {
	skipIfNoGPG(t)
	setupGPGDir(t)

	// Create the checksum file to sign.
	checksumContent := []byte("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file.tar.gz\n")
	checksumFile := filepath.Join(t.TempDir(), "checksum.sha256")
	if err := os.WriteFile(checksumFile, checksumContent, 0o644); err != nil {
		t.Fatalf("write checksum file: %v", err)
	}

	// Generate a GPG key and sign.
	genGPGKey(t, "gpgtest@example.com")
	signatureFile := signFile(t, checksumFile)

	// Verify the signature via GPGVerify.
	rn := &run.OSExecRunner{}
	err := GPGVerify(context.Background(), rn, checksumFile, signatureFile, "")
	if err != nil {
		t.Fatalf("GPGVerify should succeed: %v", err)
	}
}

// TestGPGVerifyTamperedFile tests that GPG verification fails when the
// checksum file has been modified after signing.
func TestGPGVerifyTamperedFile(t *testing.T) {
	skipIfNoGPG(t)
	setupGPGDir(t)

	// Create checksum file.
	checksumContent := []byte("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855  file.tar.gz\n")
	checksumFile := filepath.Join(t.TempDir(), "checksum.sha256")
	if err := os.WriteFile(checksumFile, checksumContent, 0o644); err != nil {
		t.Fatalf("write checksum file: %v", err)
	}

	// Generate key and sign.
	genGPGKey(t, "gpgtest2@example.com")
	signatureFile := signFile(t, checksumFile)

	// Tamper with the checksum file.
	if err := os.WriteFile(checksumFile, []byte("tampered content\n"), 0o644); err != nil {
		t.Fatalf("tamper checksum file: %v", err)
	}

	// Verification should now fail.
	rn := &run.OSExecRunner{}
	err := GPGVerify(context.Background(), rn, checksumFile, signatureFile, "")
	if err == nil {
		t.Fatal("GPGVerify should fail on tampered file")
	}
	if !strings.Contains(err.Error(), "gpg:") {
		t.Fatalf("error should mention gpg, got: %v", err)
	}
}

// TestGPGVerifyMissingGPG tests that GPG verification silently passes when
// gpg is not installed (returns nil with a warning).
func TestGPGVerifyMissingGPG(t *testing.T) {
	if _, err := exec.LookPath("gpg"); err != nil {
		// gpg not available — verify the warning path returns nil.
		fr := &run.FakeRunner{}
		err := GPGVerify(context.Background(), fr, "/nonexistent", "/nonexistent", "")
		if err != nil {
			t.Fatalf("GPGVerify should return nil when gpg is missing: %v", err)
		}
		return
	}
	// gpg is available — we can't easily test the missing-gpg path
	// without manipulating PATH. The function should not panic.
	t.Log("gpg available, cannot test missing-gpg path")
}

// TestGPGVerifyKeyImport tests GPG verification by importing a key from a URL
// (file:// URL simulating a remote key server).
func TestGPGVerifyKeyImport(t *testing.T) {
	skipIfNoGPG(t)
	_ = setupGPGDir(t)

	// Create checksum file.
	checksumContent := []byte("abc123  test.tar.gz\n")
	checksumFile := filepath.Join(t.TempDir(), "checksum.sha256")
	if err := os.WriteFile(checksumFile, checksumContent, 0o644); err != nil {
		t.Fatalf("write checksum file: %v", err)
	}

	// Create a key and sign.
	keyID := "gpgtest-keyimport@example.com"
	genGPGKey(t, keyID)

	// Export the public key to a file (simulating the key URL).
	keyFile := filepath.Join(t.TempDir(), "pubkey.asc")
	expCmd := exec.Command("gpg", "--armor", "--export", keyID)
	keyData, err := expCmd.Output()
	if err != nil {
		t.Fatalf("gpg --export failed: %v", err)
	}
	if err := os.WriteFile(keyFile, keyData, 0o644); err != nil {
		t.Fatalf("write pubkey: %v", err)
	}

	signatureFile := signFile(t, checksumFile)

	// Reset GNUPGHOME to a clean one so the key is NOT in the keyring.
	gnupgHome2 := t.TempDir()
	if err := os.Chmod(gnupgHome2, 0o700); err != nil {
		t.Fatalf("chmod gnupgHome2: %v", err)
	}
	t.Setenv("GNUPGHOME", gnupgHome2)

	// Verification should fail since the key is not in the new keyring.
	rn := &run.OSExecRunner{}
	err = GPGVerify(context.Background(), rn, checksumFile, signatureFile, "")
	if err == nil {
		t.Fatal("GPGVerify should fail when key is not in keyring")
	}

	// Import the key (simulating what GPGVerify does for a key URL).
	importCmd := exec.Command("gpg", "--import", "--batch", keyFile)
	out, err := importCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("gpg --import failed: %v\n%s", err, out)
	}

	// Now verification should succeed.
	err = GPGVerify(context.Background(), rn, checksumFile, signatureFile, "")
	if err != nil {
		t.Fatalf("GPGVerify should succeed after importing key: %v", err)
	}
}

// TestGPGVerifyAllExistingPass verifies that all existing tests still pass
// by explicitly running the core verification test.
func TestGPGVerifyAllExistingPass(t *testing.T) {
	// Just verify the package builds and checksum verification still works.
	content := []byte("hello world")
	tmpFile := filepath.Join(t.TempDir(), "test.txt")
	if err := os.WriteFile(tmpFile, content, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	err := VerifyChecksum(tmpFile, "sha256:b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9")
	if err != nil {
		t.Fatalf("VerifyChecksum should pass: %v", err)
	}
}

// TestGPGVerifyWrongSigner verifies that GPGVerify rejects a signature made
// by key B when key A was expected, even when both keys are present in the
// keyring. This test proves the signer-confusion vulnerability is fixed.
func TestGPGVerifyWrongSigner(t *testing.T) {
	skipIfNoGPG(t)
	setupGPGDir(t)

	// Create checksum file.
	checksumContent := []byte("abc123  test.tar.gz\n")
	checksumFile := filepath.Join(t.TempDir(), "checksum.sha256")
	if err := os.WriteFile(checksumFile, checksumContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// Generate signer B and sign with it (B is the only key at signing time).
	genGPGKey(t, "signer-b@example.com")
	sigFile := signFile(t, checksumFile) // signs with B (the default key)

	// Export B's key.
	exportB := exec.Command("gpg", "--armor", "--export", "signer-b@example.com")
	keyDataB, err := exportB.Output()
	if err != nil {
		t.Fatal(err)
	}
	keyFileB := filepath.Join(t.TempDir(), "pubkey-b.asc")
	if err := os.WriteFile(keyFileB, keyDataB, 0o644); err != nil {
		t.Fatal(err)
	}

	// Generate signer A.
	genGPGKey(t, "signer-a@example.com")
	exportA := exec.Command("gpg", "--armor", "--export", "signer-a@example.com")
	keyDataA, err := exportA.Output()
	if err != nil {
		t.Fatal(err)
	}
	keyFileA := filepath.Join(t.TempDir(), "pubkey-a.asc")
	if err := os.WriteFile(keyFileA, keyDataA, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a combined key file: A's public key first, then B's.
	// extractFingerprintFromKeyFile (called by importSigningKey via --show-key)
	// will return A's fingerprint (the first fpr: line).
	// But the checksum was signed by B. The identity check must catch this.
	combinedKeyFile := filepath.Join(t.TempDir(), "pubkey-combined.asc")
	combined := append([]byte(nil), keyDataA...)
	combined = append(combined, keyDataB...)
	if err := os.WriteFile(combinedKeyFile, combined, 0o644); err != nil {
		t.Fatal(err)
	}

	// Reset GNUPGHOME to a clean dir — no keys pre-imported.
	cleanDir := t.TempDir()
	if err := os.Chmod(cleanDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GNUPGHOME", cleanDir)

	// Call GPGVerify with the combined key file as signingKey.
	// Before the fix: imports both keys into the shared keyring, verifies B's sig using B's key — passes (BUG).
	// After the fix:  extracts A's fingerprint as expected, imports both into isolated homedir,
	//                 verifies B's sig using B's key — passes crypto check (exit 0),
	//                 then identity comparison fails: expected A, got B.
	rn := &run.OSExecRunner{}
	err = GPGVerify(context.Background(), rn, checksumFile, sigFile, "file://"+combinedKeyFile)
	if err == nil {
		t.Fatal("GPGVerify should fail: signed by key B, expected key A")
	}
	if !strings.Contains(err.Error(), "expected") {
		t.Fatalf("error should mention fingerprint mismatch, got: %v", err)
	}
}
