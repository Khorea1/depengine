package httpdownload

import (
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
func GPGVerify(ctx context.Context, rn run.Runner, checksumFile, signatureFile, signingKey string) error {
	// Check if gpg is available on the system.
	if !run.LookPath(ctx, rn, "gpg") {
		return fmt.Errorf("gpg: not found in PATH, cannot verify signature")
	}

	// Import signing key if provided as a URL or fingerprint.
	if signingKey != "" {
		if strings.Contains(signingKey, "://") {
			tmpDir, err := os.MkdirTemp("", "depengine-gpg-key-*")
			if err != nil {
				return fmt.Errorf("gpg: temp dir for key import: %w", err)
			}
			defer os.RemoveAll(tmpDir)
			keyFile := filepath.Join(tmpDir, "pubkey.asc")

			dl := NewGoDownloader()
			if err := dl.Download(ctx, signingKey, keyFile); err != nil {
				return fmt.Errorf("gpg: downloading signing key: %w", err)
			}

			res := rn.Run(ctx, "gpg", "--import", "--batch", keyFile)
			if res.Err != nil || res.ExitCode != 0 {
				return fmt.Errorf("gpg: importing key: %s", strings.TrimSpace(string(res.Stderr)))
			}
		} else {
			// Treat as a key fingerprint — import from keyserver.
			res := rn.Run(ctx, "gpg", "--batch", "--keyserver", DefaultKeyServer, "--recv-keys", signingKey)
			if res.Err != nil || res.ExitCode != 0 {
				return fmt.Errorf("gpg: failed to import key %s: %v\n%s", signingKey, res.Err, strings.TrimSpace(string(res.Stderr)))
			}
		}
	}

	// Verify detached signature: gpg --verify <signature> <signed-file>
	res := rn.Run(ctx, "gpg", "--verify", "--batch", signatureFile, checksumFile)
	if res.Err != nil || res.ExitCode != 0 {
		return fmt.Errorf("gpg: signature verification failed: %s", strings.TrimSpace(string(res.Stderr)))
	}

	return nil
}
