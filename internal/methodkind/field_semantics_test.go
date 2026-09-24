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

func TestSecretRefsBelongOnlyToHTTPContract(t *testing.T) {
	http, ok := Lookup("http")
	if !ok {
		t.Fatal("http contract missing")
	}
	secretFields := []string{"secret_ref", "checksum_secret_ref", "signature_secret_ref"}
	for _, name := range secretFields {
		field, ok := http.Fields[name]
		if !ok || field.Type != SecretRef || field.Effects != EffectResolve|EffectExecute || field.Semantic != SemanticAuthentication {
			t.Fatalf("http.%s contract = %+v, want typed resolve/execute authentication field", name, field)
		}
	}
	for _, kind := range []string{"github", "appimage", "msi"} {
		contract, ok := Lookup(kind)
		if !ok {
			t.Fatalf("%s contract missing", kind)
		}
		for _, name := range secretFields {
			if _, declared := contract.Fields[name]; declared {
				t.Errorf("%s unexpectedly declares %s", kind, name)
			}
		}
	}
}
