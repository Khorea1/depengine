package plan

import "testing"

func TestParseScopePortableVocabulary(t *testing.T) {
	for _, raw := range []string{"user", "system"} {
		t.Run(raw, func(t *testing.T) {
			got, err := ParseScope(raw)
			if err != nil {
				t.Fatalf("ParseScope(%q): %v", raw, err)
			}
			if string(got) != raw {
				t.Fatalf("ParseScope(%q) = %q", raw, got)
			}
		})
	}
}

func TestParseScopeRejectsAliasesAndAmbiguousValues(t *testing.T) {
	for _, raw := range []string{"", "global", "machine", "default", " user", "system "} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ParseScope(raw); err == nil {
				t.Fatalf("ParseScope(%q) unexpectedly succeeded", raw)
			}
		})
	}
}
