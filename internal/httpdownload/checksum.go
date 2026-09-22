package httpdownload

import (
	"bufio"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"regexp"
	"strings"
)

// Checksum prefix constants for known algorithms.
const (
	PrefixSHA256 = "sha256:"
	PrefixMD5    = "md5:"
	PrefixSHA1   = "sha1:"
	PrefixSHA512 = "sha512:"
)

// HashFile hashes a file with the given algorithm name ("sha256", "md5", "sha1", "sha512").
func HashFile(algorithm, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open: %w", err)
	}
	defer f.Close()

	var h hash.Hash
	switch algorithm {
	case "sha256":
		h = sha256.New()
	case "md5":
		h = md5.New()
	case "sha1":
		h = sha1.New()
	case "sha512":
		h = sha512.New()
	default:
		return "", fmt.Errorf("unsupported hash algorithm: %q", algorithm)
	}

	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// SHA256File calculates the SHA-256 hash of a file.
func SHA256File(path string) (string, error) {
	return HashFile("sha256", path)
}

// MD5File calculates the MD5 hash of a file.
func MD5File(path string) (string, error) {
	return HashFile("md5", path)
}

// SHA1File calculates the SHA-1 hash of a file.
func SHA1File(path string) (string, error) {
	return HashFile("sha1", path)
}

// SHA512File calculates the SHA-512 hash of a file.
func SHA512File(path string) (string, error) {
	return HashFile("sha512", path)
}

// VerifyChecksum checks a file's hash against the expected value.
// Supported checksum formats:
//   - "sha256:<hex>" — compare file SHA-256 hash against the given hex string
//   - "md5:<hex>"    — compare file MD5 hash against the given hex string
//   - "sha1:<hex>"   — compare file SHA-1 hash against the given hex string
//   - "sha512:<hex>" — compare file SHA-512 hash against the given hex string
//   - "*:auto"       — not supported here; call VerifyChecksumAuto instead
//   - empty           — skip verification
func VerifyChecksum(filePath, checksum string) error {
	if checksum == "" {
		return nil
	}

	prefix, algorithm, err := parseChecksumPrefix(checksum)
	if err != nil {
		return err
	}

	expected := strings.TrimPrefix(checksum, prefix)
	if expected == "auto" {
		return fmt.Errorf("%s:auto must be resolved before calling VerifyChecksum", prefix)
	}

	actual, err := HashFile(algorithm, filePath)
	if err != nil {
		return fmt.Errorf("checksum: %w", err)
	}

	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expected, actual)
	}
	return nil
}

// parseChecksumPrefix extracts the prefix and algorithm name from a checksum string.
func parseChecksumPrefix(checksum string) (prefix, algorithm string, err error) {
	for _, p := range []struct {
		prefix    string
		algorithm string
	}{
		{PrefixSHA256, "sha256"},
		{PrefixMD5, "md5"},
		{PrefixSHA1, "sha1"},
		{PrefixSHA512, "sha512"},
	} {
		if strings.HasPrefix(checksum, p.prefix) {
			return p.prefix, p.algorithm, nil
		}
	}
	return "", "", fmt.Errorf("unsupported checksum format: %q (expected 'sha256:...', 'md5:...', 'sha1:...', or 'sha512:...')", checksum)
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

// bsdLineRegex matches BSD-style checksum lines like:
//
//	SHA256 (filename) = hash
//	MD5 (filename) = hash
var bsdLineRegex = regexp.MustCompile(`^(\S+)\s+\(([^)]+)\)\s*=\s*(\S+)$`)

// ParseChecksumFileBSDExtended parses BSD-style checksum output. Format:
//
//	SHA256 (<filename>) = <hash>
//	MD5 (<filename>) = <hash>
//
// Returns the same map as ParseChecksumFile: filename → hash.
func ParseChecksumFileBSDExtended(r io.Reader) (map[string]string, error) {
	result := map[string]string{}
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := bsdLineRegex.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// m[1] = algorithm (SHA256, MD5, etc.)
		// m[2] = filename
		// m[3] = hash
		result[m[2]] = strings.ToLower(m[3])
	}
	return result, scanner.Err()
}

// ParseChecksumFileAuto tries sha256sum format first, then BSD format.
func ParseChecksumFileAuto(r io.Reader) (map[string]string, error) {
	// Read all data so we can rewind if needed.
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	// Sniff the format by looking at the first non-comment line.
	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// BSD format lines have parentheses and an equals sign:
		//   ALGORITHM (filename) = hash
		if strings.Contains(line, "(") && strings.Contains(line, ") =") {
			return ParseChecksumFileBSDExtended(strings.NewReader(string(b)))
		}
		break
	}

	// Default to sha256sum format.
	return ParseChecksumFile(strings.NewReader(string(b)))
}
