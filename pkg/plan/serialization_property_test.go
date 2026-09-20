package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
)

func TestMarshalJSONRedactionDoesNotMutatePlan(t *testing.T) {
	secret := "serialization-secret-42"
	p := New("demo", "http", true)
	p.Identity.Source = "https://user:" + secret + "@example.test/source"
	p.Artifacts = []Artifact{{URL: "https://example.test/a?token=" + secret}}
	p.Sources = []SourceReference{{
		Role: SourceSelection,
		URL:  "https://user:" + secret + "@example.test/index",
		Trust: &SourceTrust{
			KeyReference: "https://example.test/key?token=" + secret,
		},
	}}
	before := clonePlanForTest(p)

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(secret)) {
		t.Fatalf("serialized plan leaked secret: %s", data)
	}
	if !reflect.DeepEqual(p, before) {
		t.Fatalf("MarshalJSON mutated plan\nbefore: %#v\nafter:  %#v", before, p)
	}
}

func FuzzResolvedInstallPlanSerializationRedactsSecrets(f *testing.F) {
	for _, seed := range []string{"secret", "tok_123", "A1b2C3d4", "with-dash_9"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		digest := sha256.Sum256([]byte(input))
		secret := "DEPSECRET_" + hex.EncodeToString(digest[:8])

		credentialURL := "https://user:" + secret + "@example.test/path?token=" + secret
		p := New(credentialURL, credentialURL, true)
		p.Identity.Package = credentialURL
		p.Identity.Source = credentialURL
		p.Identity.Registry = credentialURL
		p.Identity.Version = credentialURL
		p.Identity.Revision = credentialURL
		p.Identity.Digest = credentialURL
		p.Artifacts = []Artifact{{URL: credentialURL, SignatureURL: credentialURL}}
		p.Prerequisites = []Prerequisite{{Name: credentialURL, Method: credentialURL}}
		p.Sources = []SourceReference{{
			Role: SourceSelection,
			Name: credentialURL,
			URL:  credentialURL,
			Trust: &SourceTrust{
				KeyReference: credentialURL,
				Fingerprint:  credentialURL,
			},
		}}
		p.OwnedPaths = []string{credentialURL}
		p.Entrypoints = map[string]string{credentialURL: credentialURL}
		p.Operations = []Operation{{
			Kind:        credentialURL,
			Description: credentialURL,
			Effect:      EffectMutation,
			Command:     []string{"tool", "--token", secret, credentialURL},
		}}
		p.Removal.Identity = credentialURL
		p.Removal.OwnedPaths = []string{credentialURL}
		p.Secrets = []SecretReference{{Provider: credentialURL, Name: credentialURL}}

		before := clonePlanForTest(p)
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("serialized plan leaked %q: %s", secret, data)
		}
		if !reflect.DeepEqual(p, before) {
			t.Fatal("MarshalJSON mutated the input plan")
		}
	})
}

func clonePlanForTest(p ResolvedInstallPlan) ResolvedInstallPlan {
	out := p
	out.Artifacts = append([]Artifact(nil), p.Artifacts...)
	out.Prerequisites = append([]Prerequisite(nil), p.Prerequisites...)
	out.Sources = append([]SourceReference(nil), p.Sources...)
	for i := range out.Sources {
		if p.Sources[i].Trust != nil {
			trust := *p.Sources[i].Trust
			out.Sources[i].Trust = &trust
		}
		if p.Sources[i].SecretRef != nil {
			secret := *p.Sources[i].SecretRef
			out.Sources[i].SecretRef = &secret
		}
	}
	out.OwnedPaths = append([]string(nil), p.OwnedPaths...)
	if p.Entrypoints != nil {
		out.Entrypoints = make(map[string]string, len(p.Entrypoints))
		for k, v := range p.Entrypoints {
			out.Entrypoints[k] = v
		}
	}
	out.Operations = cloneOperationsForTest(p.Operations)
	out.Removal.OwnedPaths = append([]string(nil), p.Removal.OwnedPaths...)
	out.Secrets = append([]SecretReference(nil), p.Secrets...)
	return out
}

func cloneOperationsForTest(in []Operation) []Operation {
	out := append([]Operation(nil), in...)
	for i := range out {
		out[i].Command = append([]string(nil), in[i].Command...)
	}
	return out
}
