package main

import "testing"

func TestGenerateCorpusRecipesMatchesRealVariantGolden(t *testing.T) {
	if err := generateCorpusRecipes("../testdata/recipe-corpus/dep-ixd-13", false); err != nil {
		t.Fatal(err)
	}
}

func TestRejectDuplicateArchitectureKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{"unique variants", `{"version":"1","architecture":{"64bit":{},"arm64":{}}}`, false},
		{"duplicate variant", `{"version":"1","architecture":{"64bit":{},"64bit":{}}}`, true},
		{"duplicate architecture object", `{"architecture":{},"architecture":{}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := rejectDuplicateArchitectureKeys([]byte(tc.raw))
			if (err != nil) != tc.want {
				t.Fatalf("rejectDuplicateArchitectureKeys() error = %v, want error %v", err, tc.want)
			}
		})
	}
}
