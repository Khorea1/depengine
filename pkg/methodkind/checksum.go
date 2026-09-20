package methodkind

import (
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

// ChecksumContract defines the digest algorithms a method can verify and
// whether it can resolve an :auto digest from an external checksum source.
type ChecksumContract struct {
	Algorithms []string
	AllowAuto  bool
}

var checksumHexLengths = map[string]int{
	"sha256": 64,
	"sha512": 128,
	"sha1":   40,
	"md5":    32,
}

// ValidateChecksum validates a configured checksum using this method's
// declared checksum contract. Empty checksums are valid because checksum is
// optional for the methods that currently expose it.
func (c Contract) ValidateChecksum(value string) error {
	if value == "" {
		return nil
	}
	if c.Checksum == nil {
		return fmt.Errorf("method %s does not declare checksum semantics", c.Kind)
	}
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("checksum must use algorithm:value syntax")
	}
	algorithm := strings.ToLower(parts[0])
	if !slices.Contains(c.Checksum.Algorithms, algorithm) {
		return fmt.Errorf("checksum algorithm %q is not supported by %s", parts[0], c.Kind)
	}
	if parts[1] == "auto" {
		if !c.Checksum.AllowAuto {
			return fmt.Errorf("checksum :auto is not supported by %s", c.Kind)
		}
		return nil
	}
	length, ok := checksumHexLengths[algorithm]
	if !ok {
		return fmt.Errorf("checksum algorithm %q has no validation contract", algorithm)
	}
	if len(parts[1]) != length {
		return fmt.Errorf("%s checksum requires %d hex characters", algorithm, length)
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return fmt.Errorf("%s checksum must be hexadecimal", algorithm)
	}
	return nil
}
