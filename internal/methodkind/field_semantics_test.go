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

func TestSecretRefsBelongOnlyToSupportedContracts(t *testing.T) {
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
	github, ok := Lookup("github")
	if !ok {
		t.Fatal("github contract missing")
	}
	field, ok := github.Fields["secret_ref"]
	if !ok || field.Type != SecretRef || field.Effects != EffectResolve|EffectExecute || field.Semantic != SemanticAuthentication {
		t.Fatalf("github.secret_ref contract = %+v, want typed resolve/execute authentication field", field)
	}
	git, ok := Lookup("git")
	if !ok {
		t.Fatal("git contract missing")
	}
	field, ok = git.Fields["secret_ref"]
	if !ok || field.Type != SecretRef || field.Effects != EffectResolve|EffectExecute || field.Semantic != SemanticAuthentication {
		t.Fatalf("git.secret_ref contract = %+v, want typed resolve/execute authentication field", field)
	}
	if git.Capabilities&CapabilityAuth == 0 {
		t.Fatalf("git capabilities = %v, want authentication capability", git.Capabilities)
	}
	cargo, ok := Lookup("cargo")
	if !ok {
		t.Fatal("cargo contract missing")
	}
	field, ok = cargo.Fields["secret_ref"]
	if !ok || field.Type != SecretRef || field.Effects != EffectResolve|EffectExecute || field.Semantic != SemanticAuthentication {
		t.Fatalf("cargo.secret_ref contract = %+v, want typed resolve/execute authentication field", field)
	}
	if cargo.Capabilities&CapabilityAuth == 0 {
		t.Fatalf("cargo capabilities = %v, want authentication capability", cargo.Capabilities)
	}
	if got := cargo.Requires["secret_ref"]; len(got) != 1 || got[0] != "git" {
		t.Fatalf("cargo.secret_ref requirements = %v, want git source", got)
	}
	for _, name := range []string{"checksum_secret_ref", "signature_secret_ref"} {
		if _, declared := github.Fields[name]; declared {
			t.Errorf("github unexpectedly declares %s", name)
		}
		if _, declared := git.Fields[name]; declared {
			t.Errorf("git unexpectedly declares %s", name)
		}
	}
	for _, kind := range []string{"appimage", "msi", "native"} {
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
