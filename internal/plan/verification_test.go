package plan

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestReconcileSatisfiedExactIdentity(t *testing.T) {
	desired := ResolvedIdentity{Version: "1.4.3", Source: "registry.example/stable", Scope: "user"}
	got := Reconcile(desired, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "1.4.3", Source: "registry.example/stable", Scope: "user"},
		KnownFields: []IdentityField{FieldScope, FieldVersion, FieldSource},
	})
	if got.State != StateSatisfied {
		t.Fatalf("state = %q, want %q: %#v", got.State, StateSatisfied, got)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	wantFields := []IdentityField{FieldVersion, FieldSource, FieldScope}
	if !reflect.DeepEqual(got.KnownFields, wantFields) {
		t.Fatalf("known fields = %#v, want %#v", got.KnownFields, wantFields)
	}
}

func TestReconcileUsesExactRequestedVersionBeforeConcreteResolution(t *testing.T) {
	desired := ResolvedIdentity{RequestedVersion: &VersionIntent{Mode: VersionExact, Value: "2.0.0"}}

	satisfied := Reconcile(desired, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "2.0.0"},
		KnownFields: []IdentityField{FieldVersion},
	})
	if satisfied.State != StateSatisfied {
		t.Fatalf("matching exact request state = %q, want %q: %#v", satisfied.State, StateSatisfied, satisfied)
	}

	drifted := Reconcile(desired, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "1.9.0"},
		KnownFields: []IdentityField{FieldVersion},
	})
	if drifted.State != StateDrifted {
		t.Fatalf("mismatched exact request state = %q, want %q: %#v", drifted.State, StateDrifted, drifted)
	}
	want := []IdentityDrift{{Field: FieldVersion, Desired: "2.0.0", Observed: "1.9.0"}}
	if !reflect.DeepEqual(drifted.Drift, want) {
		t.Fatalf("drift = %#v, want %#v", drifted.Drift, want)
	}
}

func TestReconcileUsesRequestedDigestBeforeConcreteResolution(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	desired := ResolvedIdentity{RequestedVersion: &VersionIntent{Mode: VersionDigest, Value: digest}}

	got := Reconcile(desired, Observation{
		Presence: PresencePresent,
		Identity: ObservedIdentity{
			Digest: "SHA256:" + strings.Repeat("A", 64),
		},
		KnownFields: []IdentityField{FieldDigest},
	})
	if got.State != StateSatisfied {
		t.Fatalf("equivalent requested digest state = %q, want %q: %#v", got.State, StateSatisfied, got)
	}
}

func TestReconcileVersionDriftIsNotSatisfied(t *testing.T) {
	got := Reconcile(ResolvedIdentity{Version: "2.0.0"}, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "1.9.0"},
		KnownFields: []IdentityField{FieldVersion},
	})
	if got.State != StateDrifted {
		t.Fatalf("state = %q, want %q", got.State, StateDrifted)
	}
	want := []IdentityDrift{{Field: FieldVersion, Desired: "2.0.0", Observed: "1.9.0"}}
	if !reflect.DeepEqual(got.Drift, want) {
		t.Fatalf("drift = %#v, want %#v", got.Drift, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestReconcileUnknownWhenManagerCannotReportDesiredIdentity(t *testing.T) {
	got := Reconcile(ResolvedIdentity{Version: "2.0.0", Source: "stable"}, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "2.0.0"},
		KnownFields: []IdentityField{FieldVersion},
		Detail:      "manager does not report package source",
	})
	if got.State != StateUnknown {
		t.Fatalf("state = %q, want %q", got.State, StateUnknown)
	}
	if !reflect.DeepEqual(got.Unverifiable, []IdentityField{FieldSource}) {
		t.Fatalf("unverifiable = %#v", got.Unverifiable)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestReconcileDriftTakesPrecedenceOverUnknownDimensions(t *testing.T) {
	got := Reconcile(ResolvedIdentity{Version: "2", Source: "stable"}, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "1"},
		KnownFields: []IdentityField{FieldVersion},
	})
	if got.State != StateDrifted {
		t.Fatalf("state = %q, want %q", got.State, StateDrifted)
	}
	if len(got.Drift) != 1 || !reflect.DeepEqual(got.Unverifiable, []IdentityField{FieldSource}) {
		t.Fatalf("result = %#v", got)
	}
}

func TestReconcilePresenceStates(t *testing.T) {
	tests := []struct {
		name        string
		observation Observation
		want        VerificationState
	}{
		{name: "absent", observation: Observation{Presence: PresenceAbsent}, want: StateAbsent},
		{name: "unknown", observation: Observation{Presence: PresenceUnknown, Detail: "query unsupported"}, want: StateUnknown},
		{name: "broken", observation: Observation{Presence: PresenceBroken, Detail: "manager query failed"}, want: StateBroken},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Reconcile(ResolvedIdentity{Version: "1.0.0"}, tc.observation)
			if got.State != tc.want {
				t.Fatalf("state = %q, want %q", got.State, tc.want)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("Validate: %v", err)
			}
		})
	}
}

func TestReconcileInvalidPresenceFailsClosed(t *testing.T) {
	got := Reconcile(ResolvedIdentity{Version: "1"}, Observation{Presence: "maybe"})
	if got.State != StateBroken {
		t.Fatalf("state = %q, want %q", got.State, StateBroken)
	}
	if got.Detail == "" {
		t.Fatal("expected diagnostic detail")
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestVerificationValidateRejectsContradictoryResults(t *testing.T) {
	tests := []VerificationResult{
		{State: StateSatisfied, Drift: []IdentityDrift{{Field: FieldVersion, Desired: "2", Observed: "1"}}},
		{State: StateDrifted},
		{State: StateUnknown},
		{State: StateBroken},
		{State: StateDrifted, Drift: []IdentityDrift{{Field: FieldVersion, Desired: "1", Observed: "1"}}},
		{State: StateSatisfied, KnownFields: []IdentityField{FieldVersion, FieldVersion}},
	}
	for i, result := range tests {
		if err := result.Validate(); err == nil {
			t.Fatalf("case %d: expected validation error for %#v", i, result)
		}
	}
}

func TestReconcileIgnoresRequestedConstraintAfterConcreteResolution(t *testing.T) {
	desired := ResolvedIdentity{
		RequestedVersion: &VersionIntent{Mode: VersionConstraint, Value: ">=1,<2"},
		Version:          "1.8.4",
	}
	got := Reconcile(desired, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Version: "1.8.4"},
		KnownFields: []IdentityField{FieldVersion},
	})
	if got.State != StateSatisfied {
		t.Fatalf("state = %q, want %q", got.State, StateSatisfied)
	}
}

func TestReconcileInvalidKnownFieldFailsClosed(t *testing.T) {
	for _, fields := range [][]IdentityField{{"mystery"}, {FieldVersion, FieldVersion}} {
		got := Reconcile(ResolvedIdentity{Version: "1"}, Observation{
			Presence:    PresencePresent,
			KnownFields: fields,
		})
		if got.State != StateBroken {
			t.Fatalf("fields %#v: state = %q, want %q", fields, got.State, StateBroken)
		}
		if got.Detail == "" {
			t.Fatalf("fields %#v: expected detail", fields)
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("fields %#v: Validate: %v", fields, err)
		}
	}
}

func TestVerificationJSONRedactsSensitiveObservedState(t *testing.T) {
	result := VerificationResult{
		State: StateDrifted,
		Observed: ObservedIdentity{
			Source: "https://user:secret@example.test/pkg?token=topsecret",
		},
		KnownFields: []IdentityField{FieldSource},
		Drift: []IdentityDrift{{
			Field:    FieldSource,
			Desired:  "https://example.test/pkg?api_key=wantedsecret",
			Observed: "https://example.test/pkg?sig=observedsecret",
		}},
		Detail: "probe https://example.test/?access_token=detailsecret failed",
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, secret := range []string{"secret", "topsecret", "wantedsecret", "observedsecret", "detailsecret"} {
		if strings.Contains(text, secret) {
			t.Fatalf("serialized verification leaked %q: %s", secret, text)
		}
	}
}

func TestVerificationMarshalDoesNotMutateInput(t *testing.T) {
	secret := "verification-mutation-secret-42"
	result := VerificationResult{
		State: StateDrifted,
		Drift: []IdentityDrift{{
			Field:    FieldSource,
			Desired:  "https://example.test/want?token=" + secret,
			Observed: "https://user:" + secret + "@example.test/got",
		}},
	}
	wantDesired := result.Drift[0].Desired
	wantObserved := result.Drift[0].Observed

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secret) {
		t.Fatalf("serialized verification leaked secret: %s", data)
	}
	if result.Drift[0].Desired != wantDesired || result.Drift[0].Observed != wantObserved {
		t.Fatal("MarshalJSON mutated verification result")
	}
}

func TestVerificationValidateRejectsInconsistentFieldKnowledge(t *testing.T) {
	tests := []struct {
		name string
		in   VerificationResult
	}{
		{
			name: "drift is not known",
			in: VerificationResult{
				State: StateDrifted,
				Drift: []IdentityDrift{{Field: FieldVersion, Desired: "2", Observed: "1"}},
			},
		},
		{
			name: "known and unverifiable",
			in: VerificationResult{
				State:        StateUnknown,
				KnownFields:  []IdentityField{FieldSource},
				Unverifiable: []IdentityField{FieldSource},
			},
		},
		{
			name: "duplicate drift",
			in: VerificationResult{
				State:       StateDrifted,
				KnownFields: []IdentityField{FieldVersion},
				Drift: []IdentityDrift{
					{Field: FieldVersion, Desired: "2", Observed: "1"},
					{Field: FieldVersion, Desired: "3", Observed: "1"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.in.Validate(); err == nil {
				t.Fatalf("Validate() accepted inconsistent result: %#v", tt.in)
			}
		})
	}
}

func TestVerificationValidateBindsDriftToAuthoritativeObservedIdentity(t *testing.T) {
	tests := []struct {
		name string
		in   VerificationResult
	}{
		{
			name: "forged version drift",
			in: VerificationResult{
				State:       StateDrifted,
				Observed:    ObservedIdentity{Version: "2"},
				KnownFields: []IdentityField{FieldVersion},
				Drift:       []IdentityDrift{{Field: FieldVersion, Desired: "3", Observed: "1"}},
			},
		},
		{
			name: "forged source drift",
			in: VerificationResult{
				State:       StateDrifted,
				Observed:    ObservedIdentity{Source: "https://example.test/stable"},
				KnownFields: []IdentityField{FieldSource},
				Drift: []IdentityDrift{{
					Field:    FieldSource,
					Desired:  "https://example.test/edge",
					Observed: "https://example.test/other",
				}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.in.Validate(); err == nil || !strings.Contains(err.Error(), "authoritative observed identity") {
				t.Fatalf("Validate() error = %v, want authoritative observation mismatch", err)
			}
			if _, err := TransitionForVerification(tt.in); err == nil {
				t.Fatal("TransitionForVerification accepted forged drift")
			}
		})
	}
}

func TestVerificationValidateAcceptsCanonicalEquivalentObservedDriftSpelling(t *testing.T) {
	result := VerificationResult{
		State: StateDrifted,
		Observed: ObservedIdentity{
			Source: "HTTPS://EXAMPLE.TEST/index?z=2&a=1",
		},
		KnownFields: []IdentityField{FieldSource},
		Drift: []IdentityDrift{{
			Field:    FieldSource,
			Desired:  "https://example.test/other",
			Observed: "https://example.test/index?a=1&z=2",
		}},
	}
	if err := result.Validate(); err != nil {
		t.Fatalf("Validate() rejected semantically identical observed spelling: %v", err)
	}
}

func TestVerificationValidateRejectsMalformedDesiredDriftIdentity(t *testing.T) {
	tests := []VerificationResult{
		{
			State:       StateDrifted,
			Observed:    ObservedIdentity{Digest: "sha256:" + strings.Repeat("b", 64)},
			KnownFields: []IdentityField{FieldDigest},
			Drift:       []IdentityDrift{{Field: FieldDigest, Desired: "sha256:abcd", Observed: "sha256:" + strings.Repeat("b", 64)}},
		},
		{
			State:       StateDrifted,
			Observed:    ObservedIdentity{Scope: "user"},
			KnownFields: []IdentityField{FieldScope},
			Drift:       []IdentityDrift{{Field: FieldScope, Desired: "planet", Observed: "user"}},
		},
		{
			State:       StateDrifted,
			Observed:    ObservedIdentity{Environment: &EnvironmentTarget{Kind: EnvironmentNamed, Value: "dev"}},
			KnownFields: []IdentityField{FieldEnvironment},
			Drift:       []IdentityDrift{{Field: FieldEnvironment, Desired: "unknown:prod", Observed: "named:dev"}},
		},
	}
	for i, result := range tests {
		if err := result.Validate(); err == nil {
			t.Fatalf("case %d: Validate() accepted malformed desired drift identity: %#v", i, result)
		}
	}
}

func TestReconcileKnownIdentityDimensionsNeverSilentlySatisfyDrift(t *testing.T) {
	tests := []struct {
		name    string
		desired ResolvedIdentity
		seen    ObservedIdentity
		field   IdentityField
	}{
		{"package", ResolvedIdentity{Package: "want"}, ObservedIdentity{Package: "got"}, FieldPackage},
		{"version", ResolvedIdentity{Version: "2"}, ObservedIdentity{Version: "1"}, FieldVersion},
		{"revision", ResolvedIdentity{Revision: "abc"}, ObservedIdentity{Revision: "def"}, FieldRevision},
		{"digest", ResolvedIdentity{Digest: "sha256:" + strings.Repeat("a", 64)}, ObservedIdentity{Digest: "sha256:" + strings.Repeat("b", 64)}, FieldDigest},
		{"source", ResolvedIdentity{Source: "stable"}, ObservedIdentity{Source: "edge"}, FieldSource},
		{"registry", ResolvedIdentity{Registry: "corp"}, ObservedIdentity{Registry: "public"}, FieldRegistry},
		{"scope", ResolvedIdentity{Scope: "user"}, ObservedIdentity{Scope: "system"}, FieldScope},
		{"environment", ResolvedIdentity{Environment: &EnvironmentTarget{Kind: EnvironmentNamed, Value: "a"}}, ObservedIdentity{Environment: &EnvironmentTarget{Kind: EnvironmentNamed, Value: "b"}}, FieldEnvironment},
		{"architecture", ResolvedIdentity{Architecture: "amd64"}, ObservedIdentity{Architecture: "arm64"}, FieldArchitecture},
		{"platform", ResolvedIdentity{Platform: "linux"}, ObservedIdentity{Platform: "darwin"}, FieldPlatform},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Reconcile(tt.desired, Observation{Presence: PresencePresent, Identity: tt.seen, KnownFields: []IdentityField{tt.field}})
			if got.State != StateDrifted {
				t.Fatalf("state = %q, want drifted: %#v", got.State, got)
			}
			if len(got.Drift) != 1 || got.Drift[0].Field != tt.field {
				t.Fatalf("drift = %#v, want field %q", got.Drift, tt.field)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("Validate() error: %v", err)
			}
		})
	}
}

func TestReconcileAlwaysProducesValidUnknownAndBrokenResults(t *testing.T) {
	tests := []struct {
		name        string
		desired     ResolvedIdentity
		observation Observation
		want        VerificationState
	}{
		{
			name:    "unknown presence ignores stale known identity",
			desired: ResolvedIdentity{Version: "2"},
			observation: Observation{
				Presence:    PresenceUnknown,
				Identity:    ObservedIdentity{Version: "1"},
				KnownFields: []IdentityField{FieldVersion},
			},
			want: StateUnknown,
		},
		{
			name:        "unknown empty desired identity gets diagnostic",
			observation: Observation{Presence: PresenceUnknown},
			want:        StateUnknown,
		},
		{
			name:        "broken probe gets diagnostic",
			observation: Observation{Presence: PresenceBroken},
			want:        StateBroken,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Reconcile(tt.desired, tt.observation)
			if got.State != tt.want {
				t.Fatalf("state = %q, want %q", got.State, tt.want)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("Reconcile produced invalid result: %#v: %v", got, err)
			}
			if tt.observation.Presence == PresenceUnknown && len(got.KnownFields) != 0 {
				t.Fatalf("unknown presence retained authoritative fields: %#v", got.KnownFields)
			}
		})
	}
}

func TestReconcileAbsentAndBrokenDiscardStaleKnownIdentity(t *testing.T) {
	for _, presence := range []PresenceState{PresenceAbsent, PresenceBroken} {
		t.Run(string(presence), func(t *testing.T) {
			got := Reconcile(ResolvedIdentity{Version: "2"}, Observation{
				Presence:    presence,
				Identity:    ObservedIdentity{Version: "1"},
				KnownFields: []IdentityField{FieldVersion},
			})
			if len(got.KnownFields) != 0 {
				t.Fatalf("%s retained stale authoritative fields: %#v", presence, got.KnownFields)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("Reconcile produced invalid %s result: %v", presence, err)
			}
		})
	}
}

func TestVerificationValidateRejectsStateIdentityContradictions(t *testing.T) {
	tests := []VerificationResult{
		{State: StateUnknown, KnownFields: []IdentityField{FieldVersion}, Drift: []IdentityDrift{{Field: FieldVersion, Desired: "2", Observed: "1"}}, Detail: "partial probe"},
		{State: StateAbsent, KnownFields: []IdentityField{FieldVersion}},
		{State: StateAbsent, Unverifiable: []IdentityField{FieldVersion}},
		{State: StateBroken, KnownFields: []IdentityField{FieldVersion}, Detail: "probe failed"},
		{State: StateBroken, Unverifiable: []IdentityField{FieldVersion}, Detail: "probe failed"},
	}
	for i, result := range tests {
		if err := result.Validate(); err == nil {
			t.Fatalf("case %d: Validate() accepted contradictory result %#v", i, result)
		}
	}
}

func TestReconcileReturnsIndependentObservedEnvironment(t *testing.T) {
	environment := &EnvironmentTarget{Kind: EnvironmentNamed, Value: "dev"}
	observation := Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Environment: environment},
		KnownFields: []IdentityField{FieldEnvironment},
	}
	desired := ResolvedIdentity{Environment: &EnvironmentTarget{Kind: EnvironmentNamed, Value: "dev"}}
	result := Reconcile(desired, observation)
	if result.State != StateSatisfied {
		t.Fatalf("Reconcile() state = %q", result.State)
	}
	result.Observed.Environment.Value = "mutated"
	if observation.Identity.Environment.Value != "dev" {
		t.Fatal("Reconcile result aliases observation environment")
	}
}

func TestReconcileRejectsMalformedAuthoritativeObservedIdentity(t *testing.T) {
	tests := []struct {
		name   string
		field  IdentityField
		mutate func(*ObservedIdentity)
	}{
		{name: "version whitespace", field: FieldVersion, mutate: func(i *ObservedIdentity) { i.Version = " 1.2.3" }},
		{name: "source credentials", field: FieldSource, mutate: func(i *ObservedIdentity) { i.Source = "https://token@example.test/index" }},
		{name: "scope invalid", field: FieldScope, mutate: func(i *ObservedIdentity) { i.Scope = "planet" }},
		{name: "environment invalid", field: FieldEnvironment, mutate: func(i *ObservedIdentity) { i.Environment = &EnvironmentTarget{Kind: EnvironmentNamed, Value: " bad"} }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var identity ObservedIdentity
			tc.mutate(&identity)
			got := Reconcile(ResolvedIdentity{}, Observation{
				Presence:    PresencePresent,
				Identity:    identity,
				KnownFields: []IdentityField{tc.field},
			})
			if got.State != StateBroken {
				t.Fatalf("Reconcile() state = %q, want broken: %#v", got.State, got)
			}
			if got.Detail == "" {
				t.Fatal("broken reconciliation missing detail")
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("broken result should validate: %v", err)
			}
		})
	}
}

func TestVerificationValidateRejectsMalformedKnownObservedIdentity(t *testing.T) {
	result := VerificationResult{
		State:       StateSatisfied,
		Observed:    ObservedIdentity{Source: "https://token@example.test/index"},
		KnownFields: []IdentityField{FieldSource},
	}
	if err := result.Validate(); err == nil {
		t.Fatal("credential-bearing authoritative observed source unexpectedly accepted")
	}
}

func FuzzReconcileProducesValidResult(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3}, "1.2.3", "https://example.test/index", "amd64")
	f.Add([]byte{4, 9, 255}, " bad\x00", "https://user:secret@example.test", " amd64")

	desired := ResolvedIdentity{
		Package:      "demo",
		Version:      "1.2.3",
		Source:       "https://example.test/index",
		Scope:        "user",
		Architecture: "amd64",
		Platform:     "linux",
		Environment:  &EnvironmentTarget{Kind: EnvironmentNamed, Value: "tools"},
	}
	fields := []IdentityField{
		FieldPackage,
		FieldVersion,
		FieldRevision,
		FieldDigest,
		FieldSource,
		FieldRegistry,
		FieldScope,
		FieldEnvironment,
		FieldArchitecture,
		FieldPlatform,
		IdentityField("unknown"),
	}
	presences := []PresenceState{
		PresencePresent,
		PresenceAbsent,
		PresenceUnknown,
		PresenceBroken,
		PresenceState("invalid"),
	}

	f.Fuzz(func(t *testing.T, selector []byte, version, source, architecture string) {
		known := make([]IdentityField, 0, len(selector))
		for _, b := range selector {
			known = append(known, fields[int(b)%len(fields)])
		}
		presence := PresencePresent
		if len(selector) > 0 {
			presence = presences[int(selector[0])%len(presences)]
		}
		observation := Observation{
			Presence: presence,
			Identity: ObservedIdentity{
				Package:      "demo",
				Version:      version,
				Source:       source,
				Scope:        "user",
				Architecture: architecture,
				Platform:     "linux",
				Environment:  &EnvironmentTarget{Kind: EnvironmentNamed, Value: "tools"},
			},
			KnownFields: known,
			Detail:      "probe detail",
		}
		got := Reconcile(desired, observation)
		if err := got.Validate(); err != nil {
			t.Fatalf("Reconcile() produced invalid result: %v\nobservation=%#v\nresult=%#v", err, observation, got)
		}
	})
}

func TestReconcileCanonicalizesEquivalentSourceAndDigestIdentity(t *testing.T) {
	digestLower := "sha256:" + strings.Repeat("ab", 32)
	digestUpper := "SHA256:" + strings.Repeat("AB", 32)
	desired := ResolvedIdentity{
		Source:   "HTTPS://EXAMPLE.TEST/index?z=2&a=1",
		Registry: "HTTPS://REGISTRY.EXAMPLE.TEST/v1?b=2&a=1",
		Digest:   digestLower,
	}
	got := Reconcile(desired, Observation{
		Presence: PresencePresent,
		Identity: ObservedIdentity{
			Source:   "https://example.test/index?a=1&z=2",
			Registry: "https://registry.example.test/v1?a=1&b=2",
			Digest:   digestUpper,
		},
		KnownFields: []IdentityField{FieldSource, FieldRegistry, FieldDigest},
	})
	if got.State != StateSatisfied {
		t.Fatalf("state = %q, want satisfied: %#v", got.State, got)
	}
	if len(got.Drift) != 0 {
		t.Fatalf("equivalent canonical identity reported drift: %#v", got.Drift)
	}
}

func TestVerificationValidateRejectsCanonicalEquivalentDrift(t *testing.T) {
	result := VerificationResult{
		State:       StateDrifted,
		KnownFields: []IdentityField{FieldSource},
		Drift: []IdentityDrift{{
			Field:    FieldSource,
			Desired:  "HTTPS://EXAMPLE.TEST/index?z=2&a=1",
			Observed: "https://example.test/index?a=1&z=2",
		}},
	}
	if err := result.Validate(); err == nil || !strings.Contains(err.Error(), "equivalent") {
		t.Fatalf("Validate() error = %v, want equivalent drift rejection", err)
	}
}

func TestReconcileTreatsMalformedAuthoritativeDigestAndURLsAsBroken(t *testing.T) {
	desired := ResolvedIdentity{Digest: "sha256:" + strings.Repeat("a", 64)}
	for name, observation := range map[string]Observation{
		"digest": {
			Presence:    PresencePresent,
			Identity:    ObservedIdentity{Digest: "sha256:abcd"},
			KnownFields: []IdentityField{FieldDigest},
		},
		"source URL": {
			Presence:    PresencePresent,
			Identity:    ObservedIdentity{Source: "https://"},
			KnownFields: []IdentityField{FieldSource},
		},
		"registry URL": {
			Presence:    PresencePresent,
			Identity:    ObservedIdentity{Registry: "https:///registry"},
			KnownFields: []IdentityField{FieldRegistry},
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := Reconcile(desired, observation)
			if got.State != StateBroken {
				t.Fatalf("state = %q, want broken (%+v)", got.State, got)
			}
			if err := got.Validate(); err != nil {
				t.Fatalf("broken result is internally invalid: %v", err)
			}
		})
	}
}

func TestReconcileCanonicalizesSCPStyleRemoteHostOnly(t *testing.T) {
	desired := ResolvedIdentity{Source: "Deploy@GIT.EXAMPLE.TEST:Org/Repo.git"}
	got := Reconcile(desired, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Source: "Deploy@git.example.test:Org/Repo.git"},
		KnownFields: []IdentityField{FieldSource},
	})
	if got.State != StateSatisfied {
		t.Fatalf("host-case-only scp remote drifted: %+v", got)
	}

	got = Reconcile(desired, Observation{
		Presence:    PresencePresent,
		Identity:    ObservedIdentity{Source: "Deploy@git.example.test:org/repo.git"},
		KnownFields: []IdentityField{FieldSource},
	})
	if got.State != StateDrifted {
		t.Fatalf("path-case change not detected: %+v", got)
	}
}

func TestReconcileRejectsInvalidDesiredIdentity(t *testing.T) {
	for _, desired := range []ResolvedIdentity{
		{Digest: "sha256:abcd"},
		{Source: "https://"},
		{Scope: "elsewhere"},
		{RequestedVersion: &VersionIntent{Mode: VersionDigest, Value: "sha256:" + strings.Repeat("a", 64)}, Digest: "sha256:" + strings.Repeat("b", 64)},
	} {
		got := Reconcile(desired, Observation{Presence: PresencePresent})
		if got.State != StateBroken || !strings.Contains(got.Detail, "invalid desired identity") {
			t.Fatalf("Reconcile(%#v) = %#v, want broken invalid desired identity", desired, got)
		}
		if err := got.Validate(); err != nil {
			t.Fatalf("broken result should validate: %v", err)
		}
	}
}
