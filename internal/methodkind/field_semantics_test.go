package methodkind

import "testing"

func TestEveryContractFieldHasExplicitSemanticClassification(t *testing.T) {
	seen := make(map[string]bool)
	for _, contract := range Contracts {
		for name, field := range contract.Fields {
			seen[name] = true
			if field.Semantic == SemanticUnknown {
				t.Errorf("%s.%s has no semantic classification", contract.Kind, name)
			}
			if got := semanticForField(name); got != field.Semantic {
				t.Errorf("%s.%s semantic = %d, registry = %d", contract.Kind, name, field.Semantic, got)
			}
		}
	}
	for name := range fieldSemantics {
		if !seen[name] {
			t.Errorf("semantic registry contains stale field %q", name)
		}
	}
}

func TestSemanticRegistryCoversEveryPublicFieldName(t *testing.T) {
	for _, contract := range Contracts {
		for name := range contract.Fields {
			if semanticForField(name) == SemanticUnknown {
				t.Fatalf("new contract field %s.%s must be classified in fieldSemantics", contract.Kind, name)
			}
		}
	}
}

func TestSecretRefBelongsOnlyToHTTPContract(t *testing.T) {
	http, ok := Lookup("http")
	if !ok {
		t.Fatal("http contract missing")
	}
	field, ok := http.Fields["secret_ref"]
	if !ok || field.Type != SecretRef || field.Effects != EffectResolve|EffectExecute || field.Semantic != SemanticAuthentication {
		t.Fatalf("http.secret_ref contract = %+v, want typed resolve/execute authentication field", field)
	}
	for _, kind := range []string{"github", "appimage", "msi"} {
		contract, ok := Lookup(kind)
		if !ok {
			t.Fatalf("%s contract missing", kind)
		}
		if _, declared := contract.Fields["secret_ref"]; declared {
			t.Errorf("%s unexpectedly declares secret_ref", kind)
		}
	}
}
