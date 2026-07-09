package httpdownload

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// VerifyChecksum checks a file's SHA-256 hash against the expected value.
// Supported checksum formats:
//   - "sha256:<hex>" — compare file hash against the given hex string
//   - "sha256:auto"  — not supported here; call VerifyChecksumAuto instead
//   - empty           — skip verification
func VerifyChecksum(filePath, checksum string) error {
	if checksum == "" {
		return nil
	}

	const prefix = "sha256:"
	if !strings.HasPrefix(checksum, prefix) {
		return fmt.Errorf("unsupported checksum format: %q (expected 'sha256:...')", checksum)
	}

	expected := strings.TrimPrefix(checksum, prefix)
	if expected == "auto" {
		return fmt.Errorf("sha256:auto must be resolved before calling VerifyChecksum")
	}

	actual, err := SHA256File(filePath)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}

	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

// SHA256File calculates the SHA-256 hash of a file.
func SHA256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ParseChecksumFile parses a .sha256 / .sha256sum file and returns a
// map of filename → hex hash. Supports both formats:
//   - "<hash>  <filename>" (standard sha256sum)
//   - "<hash> *<filename>" (binary mode)
//   - "<hash>  <filename>" (text mode)
func ParseChecksumFile(r io.Reader) (map[string]string, error) {
	result := map[string]string{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: <hash>  <filename> or <hash> *<filename>
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		hash := parts[0]
		filename := strings.TrimLeft(parts[1], "* ")
		result[filename] = hash
	}
	return result, scanner.Err()
}
