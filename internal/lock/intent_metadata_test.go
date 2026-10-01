package lock

import (
	"context"
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/internal/config"
)

func TestSnapshotIntentMetadataMatchesLegacyResolver(t *testing.T) {
	schema := &config.Schema{Tools: map[string]*config.Tool{
		"demo": {
			Name: "demo",
			Methods: []*config.MethodCandidate{
				{
					Kind:  "http",
					Label: "mirror-a",
					Sources: []config.Source{{
						Kind: "brew", Name: "core", URL: "https://example.test/core.git", Revision: "abc123",
					}},
				},
				{
					Kind:  "http",
					Label: "mirror-b",
					Sources: []config.Source{{
						Kind: "brew", Name: "extra", URL: "https://example.test/extra.git",
					}},
				},
			},
		},
	}}

	methods, sources, err := SnapshotIntentMetadata(schema)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := ResolveLegacyV1(context.Background(), schema, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(methods, legacy.MethodsHash) {
		t.Fatalf("method metadata = %#v, legacy resolver produced %#v", methods, legacy.MethodsHash)
	}
	if !reflect.DeepEqual(sources, legacy.SourceHash) {
		t.Fatalf("source metadata = %#v, legacy resolver produced %#v", sources, legacy.SourceHash)
	}
	if len(sources) != 2 || sources["demo/http/0"] == sources["demo/http/1"] {
		t.Fatalf("same-kind source keys are not distinct: %#v", sources)
	}

	repeatedMethods, repeatedSources, err := SnapshotIntentMetadata(schema)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(methods, repeatedMethods) || !reflect.DeepEqual(sources, repeatedSources) {
		t.Fatalf("metadata changed between calls: methods %#v/%#v sources %#v/%#v", methods, repeatedMethods, sources, repeatedSources)
	}
}
