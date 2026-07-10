package schema

import (
	"os"
	"testing"

	"github.com/pelletier/go-toml/v2"
)



var testdataPrefix = "../../testdata/benchmarks/"

// knownKinds returns the list of registered adapter kinds for validation.
// Hardcoded to match the default method_order in schema.go.
func knownKinds() []string {
	return []string{"native", "cargo", "go", "pip", "pipx", "uv", "aur", "git", "http"}
}

// BenchmarkParseSchema measures the time to parse each schema size.
func BenchmarkParseSchema(b *testing.B) {
	benchmarks := []struct {
		name string
		path string
	} {
		{"Minimal", testdataPrefix + "schema_minimal.toml"},
		{"Medium",  testdataPrefix + "schema_medium.toml"},
		{"Large",   testdataPrefix + "schema_large.toml"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			m := fixedMap()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, err := ParseSchema(bm.path, m)
				if err != nil {
					b.Fatalf("ParseSchema: %v", err)
				}
			}
		})
	}
}

// BenchmarkPlaceholderExpansion measures the time to expand placeholders.
func BenchmarkPlaceholderExpansion(b *testing.B) {
	benchmarks := []struct {
		name string
		path string
	} {
		{"Minimal", testdataPrefix + "schema_minimal.toml"},
		{"Medium",  testdataPrefix + "schema_medium.toml"},
		{"Large",   testdataPrefix + "schema_large.toml"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			rawBytes, err := os.ReadFile(bm.path)
			if err != nil {
				b.Fatalf("ReadFile: %v", err)
			}

			var raw map[string]any
			if err := toml.Unmarshal(rawBytes, &raw); err != nil {
				b.Fatalf("Unmarshal: %v", err)
			}

			m := fixedMap()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ExpandAll(raw, m)
			}
		})
	}
}

// BenchmarkValidation measures the time to validate each parsed schema.
func BenchmarkValidation(b *testing.B) {
	benchmarks := []struct {
		name string
		path string
	} {
		{"Minimal", testdataPrefix + "schema_minimal.toml"},
		{"Medium",  testdataPrefix + "schema_medium.toml"},
		{"Large",   testdataPrefix + "schema_large.toml"},
	}

	// Parse schemas once
	parsed := make(map[string]*Schema, len(benchmarks))
	for _, bm := range benchmarks {
		s, err := ParseSchema(bm.path, fixedMap())
		if err != nil {
			b.Fatalf("ParseSchema(%s): %v", bm.name, err)
		}
		parsed[bm.name] = s
	}

	kinds := knownKinds()
	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			s := parsed[bm.name]
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = Validate(s, kinds)
			}
		})
	}
}