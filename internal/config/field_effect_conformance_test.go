package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/methodkind"
)

// Every EffectValidate field must have a valid baseline and a value that the
// public schema parser rejects for that field. A newly declared field cannot
// silently acquire validation semantics without a concrete parser probe.
func TestValidateEffectFieldsHaveBehaviorProbes(t *testing.T) {
	bases := map[string]string{
		"http":      `url = "https://example.test/demo.tar.gz"`,
		"github":    "repo = \"example/demo\"\nasset = \"demo.tar.gz\"",
		"appimage":  `url = "https://example.test/demo.AppImage"`,
		"android":   `url = "https://example.test/demo.apk"`,
		"msi":       "url = \"https://example.test/demo.msi\"\nproduct_name = \"Demo\"",
		"container": "manager = \"docker\"\nsource = \"example/demo\"",
		"git":       `url = "https://example.test/demo.git"`,
		"local":     `local_path = "vendor/demo.tar.gz"`,
	}
	probes := map[string]string{
		"checksum":             `checksum = ""`,
		"checksum_url":         `checksum_url = "https://example.test/SHA256SUMS"`,
		"checksum_file_format": "checksum = \"sha256:auto\"\nchecksum_file_format = \"unknown\"",
		"signature_url":        `signature_url = ""`,
		"signing_key":          "checksum = \"sha256:auto\"\nsigning_key = \"ABCD1234\"",
		"strip_components":     `strip_components = -1`,
		"container.platform":   `platform = ""`,
		"git.depth":            `depth = "shallow"`,
		"git.managed_paths":    `managed_paths = [42]`,
		"local.checksum":       `checksum = 42`,
	}
	used := make(map[string]bool, len(probes))
	for _, contract := range methodkind.Contracts {
		for name, field := range contract.Fields {
			if field.Effects&methodkind.EffectValidate == 0 {
				continue
			}
			key := contract.Kind + "." + name
			base, ok := bases[contract.Kind]
			if !ok {
				t.Errorf("%s declares EffectValidate without a valid schema baseline", key)
				continue
			}
			probeKey := key
			probe, ok := probes[probeKey]
			if !ok {
				probeKey = name
				probe, ok = probes[probeKey]
			}
			if !ok {
				t.Errorf("%s declares EffectValidate without a rejection probe", key)
				continue
			}
			used[probeKey] = true
			t.Run(key, func(t *testing.T) {
				parse := func(fields string) error {
					path := filepath.Join(t.TempDir(), "schema.toml")
					data := "schema_version = 1\n[tools.demo." + contract.Kind + "]\n" + fields + "\n"
					if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
						t.Fatal(err)
					}
					_, err := ParseProjectSchema(path, nil)
					return err
				}
				if err := parse(base); err != nil {
					t.Fatalf("baseline rejected: %v", err)
				}
				if err := parse(base + "\n" + probe); err == nil || !strings.Contains(err.Error(), name) {
					t.Fatalf("invalid %s accepted or rejected for a different reason: %v", key, err)
				}
			})
		}
	}
	for key := range probes {
		if !used[key] {
			t.Errorf("validation probe %q has no declared EffectValidate field", key)
		}
	}
}
