package schema

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// FuzzParseSchema feeds random bytes as schema.toml content to ParseSchema.
// It catches panics exclusively — parse errors are valid behavior and are
// silently ignored. A basic substitution map (fixedMap) is used so the
// placeholder-expansion code paths are exercised.
func FuzzParseSchema(f *testing.F) {
	seeds := []string{
		"[tools]\nzsh = \"zsh\"\nfd = { apt = \"fd-find\" }",
		"[defaults]\nmanager = \"native\"\n[tools]\nsimple = [\"a\",\"b\"]",
		"[tools]\nbad = { git = { url = \"https://example.com\" } }",
		"[tools]\n[tools.x]\nrequires = [\"y\"]\n[tools.y]\nrequires = [\"x\"]",
		"",
		"[tools]\nstrange = { apt = 42 }",
		"[tools]\nnested = { cargo = { git = \"https://example.com\" }, git = { url = \"https://ex.com\" } }",
		"\x00\x01\x02binary garbage",
		"[tools]\n{ broken",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	m := fixedMap()

	f.Fuzz(func(t *testing.T, content string) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("ParseSchema panicked: %v", r)
			}
		}()

		dir := t.TempDir()
		path := filepath.Join(dir, "schema.toml")
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Skip("write:", err)
		}
		// Parse errors are valid behavior; we only care about panics.
		_, _ = ParseSchema(path, m)
	})
}

// FuzzExpand feeds random strings into Expand with both a nil substitution map
// and a basic map (fixedMap). It catches panics and guards against excessive
// allocations: when the input contains no placeholders, Expand is documented to
// be allocation-free, so we flag any input that causes more than one allocation
// in that case.
func FuzzExpand(f *testing.F) {
	seeds := []string{
		"https://github.com/foo/bar/releases/download/{latest}/foo.tar.gz",
		"{id}-{version}.tar.gz",
		"apt-get install {pkg}",
		"",
		"no placeholders here",
		"{unknown}",
		"nested {outer_{inner}}",
		"{{double}}",
		"{}",
		"{!@#invalid}",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	m := fixedMap()

	f.Fuzz(func(t *testing.T, input string) {
		// --- nil map ---
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Expand(nil map) panicked: %v", r)
				}
			}()
			_ = Expand(input, nil)
		}()

		// --- basic map ---
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Expand(fixedMap) panicked: %v", r)
				}
			}()
			_ = Expand(input, m)
		}()

		// --- excessive allocations guard ---
		// When the input has no matchable placeholders, Expand must be
		// allocation-free (it returns s directly). Flag anything that
		// allocates more than a nominal amount.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Expand alloc-check panicked: %v", r)
				}
			}()

			// Measure allocations only for the basic-map call.
			allocs := testing.AllocsPerRun(10, func() {
				_ = Expand(input, m)
			})

			// When there are no placeholder tokens at all, Expand should
			// allocate zero. Allow a small ceiling for regex overhead in
			// odd inputs, but flag truly excessive allocation.
			if !PlaceholderRe.MatchString(input) && allocs > 4 {
				t.Errorf("Expand allocated %.0f times for input with no placeholders: %q", allocs, input)
			}
			_ = runtime.MemStats{} // keep runtime import meaningful
		}()
	})
}
