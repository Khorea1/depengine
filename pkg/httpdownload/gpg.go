package httpdownload

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Khorea1/depengine/pkg/run"
)

// DefaultKeyServer is the GPG keyserver used to receive signing keys by fingerprint.
// Override this for air-gapped environments or custom keyservers.
var DefaultKeyServer = "keys.openpgp.org"

// GPGVerify verifies a GPG detached signature on a checksum file.
// Returns nil if verification succeeds. Returns error if gpg is not
// available or signature verification fails.
//
// When signingKey is non-empty, the function isolates verification to a
// dedicated temporary homedir and enforces that the signer's fingerprint
// matches the expected signing key (identity check).
//
// When signingKey is empty, verification uses the default keyring with no
// identity check (backward-compatible path).
func GPGVerify(ctx context.Context, rn run.Runner, checksumFile, signatureFile, signingKey string) error {
	// Check if gpg is available on the system.
	if !run.LookPath(ctx, rn, "gpg") {
		return fmt.Errorf("gpg: not found in PATH, cannot verify signature")
	}

	// If a signing key is provided, use the isolated verification path with
	// identity checking to prevent signer-confusion attacks.
	if signingKey != "" {
		return gpgVerifyWithIdentityCheck(ctx, rn, checksumFile, signatureFile, signingKey)
	}

	// Backward-compatible path: verify using the shared default keyring.
	res := rn.Run(ctx, "gpg", "--verify", "--batch", signatureFile, checksumFile)
	if res.Err != nil || res.ExitCode != 0 {
		return fmt.Errorf("gpg: signature verification failed: %s", strings.TrimSpace(string(res.Stderr)))
	}
	return nil
}

// gpgVerifyWithIdentityCheck performs GPG signature verification in an
// isolated temporary keyring and enforces that the signer's fingerprint
// matches the provided signingKey.
func gpgVerifyWithIdentityCheck(ctx context.Context, rn run.Runner, checksumFile, signatureFile, signingKey string) error {
	// Create isolated temporary homedir.
	homedir, err := os.MkdirTemp("", "depengine-gpg-verify-*")
	if err != nil {
		return fmt.Errorf("gpg: temp homedir: %w", err)
	}
	defer os.RemoveAll(homedir)
	if err := os.Chmod(homedir, 0o700); err != nil {
		return fmt.Errorf("gpg: chmod homedir: %w", err)
	}

	// Import the expected signing key and extract its fingerprint.
	expectedFPR, err := importSigningKey(ctx, rn, homedir, signingKey)
	if err != nil {
		return err
	}

	// Run verification with status output for fingerprint extraction.
	res := rn.Run(ctx, "gpg", "--homedir", homedir, "--verify", "--batch", "--status-fd=1", signatureFile, checksumFile)
	if err := run.CheckResult(res, "gpg: signature verification failed"); err != nil {
		return err
	}

	// Extract the actual signer's primary fingerprint from the status output.
	actualFPR, ok := parseVALIDSIGPrimaryFingerprint(res.Stdout)
	if !ok {
		return fmt.Errorf("gpg: unable to determine signer fingerprint from status output")
	}

	// Enforce identity match.
	if normalizeFingerprint(actualFPR) != normalizeFingerprint(expectedFPR) {
		return fmt.Errorf("gpg: signature was made by %s, expected %s", actualFPR, expectedFPR)
	}

	return nil
}

// importSigningKey imports the signing key into the isolated homedir and
// returns the expected primary key fingerprint.
//
// Two modes:
//   - URL (contains "://"): downloads the key file, extracts the fingerprint
//     from it, then imports into the isolated homedir.
//   - Fingerprint (no "://"): imports the key from a keyserver by fingerprint,
//     then extracts the fingerprint from the imported keyring as a sanity check.
func importSigningKey(ctx context.Context, rn run.Runner, homedir, signingKey string) (string, error) {
	if strings.Contains(signingKey, "://") {
		return importSigningKeyFromURL(ctx, rn, homedir, signingKey)
	}
	return importSigningKeyByFingerprint(ctx, rn, homedir, signingKey)
}

// importSigningKeyFromURL downloads a key file, extracts its primary
// fingerprint, and imports it into the isolated homedir.
func importSigningKeyFromURL(ctx context.Context, rn run.Runner, homedir, signingKey string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "depengine-gpg-key-*")
	if err != nil {
		return "", fmt.Errorf("gpg: temp dir for key download: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	keyFile := filepath.Join(tmpDir, "pubkey.asc")

	// Download the key file.
	if strings.HasPrefix(signingKey, "file://") {
		// Handle file:// URLs directly (os.ReadFile).
		localPath := strings.TrimPrefix(signingKey, "file://")
		data, err := os.ReadFile(localPath)
		if err != nil {
			return "", fmt.Errorf("gpg: reading key file: %w", err)
		}
		if err := os.WriteFile(keyFile, data, 0o644); err != nil {
			return "", fmt.Errorf("gpg: writing key file: %w", err)
		}
	} else {
		dl := NewGoDownloader(rn)
		if err := dl.Download(ctx, signingKey, keyFile); err != nil {
			return "", fmt.Errorf("gpg: downloading signing key: %w", err)
		}
	}

	// Extract the primary fingerprint from the key file (before import).
	fpr, err := extractFingerprintFromKeyFile(ctx, rn, keyFile)
	if err != nil {
		return "", err
	}

	// Import into the isolated homedir.
	res := rn.Run(ctx, "gpg", "--homedir", homedir, "--import", "--batch", keyFile)
	if res.Err != nil || res.ExitCode != 0 {
		return "", fmt.Errorf("gpg: importing key: %s", strings.TrimSpace(string(res.Stderr)))
	}

	return fpr, nil
}

// importSigningKeyByFingerprint imports a key from a keyserver by fingerprint
// and extracts the fingerprint from the imported keyring as a sanity check.
func importSigningKeyByFingerprint(ctx context.Context, rn run.Runner, homedir, signingKey string) (string, error) {
	res := rn.Run(ctx, "gpg", "--homedir", homedir, "--batch", "--keyserver", DefaultKeyServer, "--recv-keys", signingKey)
	if res.Err != nil || res.ExitCode != 0 {
		return "", fmt.Errorf("gpg: failed to import key %s: %v\n%s", signingKey, res.Err, strings.TrimSpace(string(res.Stderr)))
	}

	// Extract fingerprint from keyring as sanity check.
	fpr, err := extractFingerprintFromKeyring(ctx, rn, homedir, signingKey)
	if err != nil {
		return "", err
	}

	return fpr, nil
}

// extractFingerprintFromKeyFile reads the primary key fingerprint from a key
// file using gpg --show-key --with-colons.
func extractFingerprintFromKeyFile(ctx context.Context, rn run.Runner, keyFile string) (string, error) {
	res := rn.Run(ctx, "gpg", "--show-key", "--with-colons", keyFile)
	if res.Err != nil || res.ExitCode != 0 {
		return "", fmt.Errorf("gpg: reading key file: %s", strings.TrimSpace(string(res.Stderr)))
	}
	fpr, ok := parseFPRLine(res.Stdout)
	if !ok {
		return "", fmt.Errorf("gpg: unable to extract fingerprint from key file")
	}
	return fpr, nil
}

// extractFingerprintFromKeyring reads the primary key fingerprint from the
// isolated keyring using gpg --list-public-keys --with-colons --fingerprint.
func extractFingerprintFromKeyring(ctx context.Context, rn run.Runner, homedir, keyID string) (string, error) {
	res := rn.Run(ctx, "gpg", "--homedir", homedir, "--with-colons", "--list-public-keys", "--fingerprint", keyID)
	if res.Err != nil || res.ExitCode != 0 {
		return "", fmt.Errorf("gpg: listing keys: %s", strings.TrimSpace(string(res.Stderr)))
	}
	fpr, ok := parseFPRLine(res.Stdout)
	if !ok {
		return "", fmt.Errorf("gpg: unable to extract fingerprint from keyring")
	}
	return fpr, nil
}

// parseFPRLine extracts the first fingerprint (fpr:) field from a
// --with-colons formatted GPG output. Field index 9 (10th colon-separated
// value) is the fingerprint value.
func parseFPRLine(output []byte) (string, bool) {
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "fpr:") {
			parts := strings.Split(line, ":")
			if len(parts) > 9 {
				return parts[9], true
			}
		}
	}
	return "", false
}

// parseVALIDSIGPrimaryFingerprint parses the [GNUPG:] VALIDSIG status line
// from gpg --status-fd=1 output and returns the primary key fingerprint of
// the signer.
//
// VALIDSIG format (GPG 2.2.7+):
//
//	[GNUPG:] VALIDSIG <sig-fpr> <date> <timestamp> <expire> <vers> <algo> <hash> <class> <primary-fpr> [...]
//
// The last field is the primary key fingerprint. For older GPG versions that
// lack the primary-fpr field, the first field (signing key fingerprint) is
// used as fallback.
func parseVALIDSIGPrimaryFingerprint(output []byte) (string, bool) {
	prefix := []byte("[GNUPG:] VALIDSIG ")
	lines := bytes.Split(output, []byte("\n"))
	for _, line := range lines {
		if !bytes.HasPrefix(line, prefix) {
			continue
		}
		rest := bytes.TrimSpace(line[len(prefix):])
		parts := strings.Fields(string(rest))
		if len(parts) >= 10 {
			return parts[len(parts)-1], true
		}
		if len(parts) >= 1 {
			return parts[0], true
		}
	}
	return "", false
}

// normalizeFingerprint normalizes a fingerprint for comparison by uppercasing
// and removing spaces.
func normalizeFingerprint(fpr string) string {
	return strings.ToUpper(strings.ReplaceAll(fpr, " ", ""))
}
