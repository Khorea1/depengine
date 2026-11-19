package plan

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"
)

func TestCompareProspectiveTargetsIsConservative(t *testing.T) {
	slot := ProspectiveTarget{Scope: "host-filesystem", Kind: "file-slot", Identity: "/usr/local/bin/rustc"}
	cases := []struct {
		name string
		a, b ProspectiveTarget
		want ResourceOverlap
	}{
		{"same file slot", slot, slot, OverlapSame},
		{"different paths may alias through hardlinks", slot, ProspectiveTarget{slot.Scope, slot.Kind, "/opt/bin/rustc"}, OverlapUnknown},
		{"different resource kind may overlap", slot, ProspectiveTarget{slot.Scope, "native-package", slot.Identity}, OverlapUnknown},
		{"different scope is not necessarily isolated", slot, ProspectiveTarget{"container:abc", slot.Kind, slot.Identity}, OverlapUnknown},
		{"incomplete target cannot prove identity", slot, ProspectiveTarget{slot.Scope, slot.Kind, ""}, OverlapUnknown},
		{"two empty targets are not same", ProspectiveTarget{}, ProspectiveTarget{}, OverlapUnknown},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompareProspectiveTargets(tt.a, tt.b); got != tt.want {
				t.Fatalf("forward = %q, want %q", got, tt.want)
			}
			if got := CompareProspectiveTargets(tt.b, tt.a); got != tt.want {
				t.Fatalf("reverse = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCompareProspectiveEffectEnvelopesFindsSharedTargetWithoutAssumingCompleteness(t *testing.T) {
	slot := ProspectiveTarget{Scope: "host-filesystem", Kind: "file-slot", Identity: "/usr/local/bin/rustc"}
	other := ProspectiveTarget{Scope: "host-filesystem", Kind: "file-slot", Identity: "/opt/bin/rustc"}
	a := ProspectiveEffectEnvelope{Tool: "rust", Completeness: EffectUnknown, Steps: []ProspectiveEffectStep{
		{Phase: "primary_install", Completeness: EffectUnknown, Targets: []ProspectiveTarget{slot}},
		{Phase: "hook_before", Completeness: EffectUnknown},
	}}
	b := ProspectiveEffectEnvelope{Tool: "rustup", Completeness: EffectUnknown, Steps: []ProspectiveEffectStep{
		{Phase: "primary_install", Completeness: EffectUnknown, Targets: []ProspectiveTarget{other, slot}},
	}}
	beforeA, beforeB := a, b
	if got := CompareProspectiveEffectEnvelopes(a, b); got != OverlapSame {
		t.Fatalf("shared slot = %q, want same", got)
	}
	if got := CompareProspectiveEffectEnvelopes(b, a); got != OverlapSame {
		t.Fatalf("shared slot reverse = %q, want same", got)
	}
	if !reflect.DeepEqual(a, beforeA) || !reflect.DeepEqual(b, beforeB) {
		t.Fatal("overlap comparison mutated its inputs")
	}
}

func TestCompareProspectiveEffectEnvelopesNeverInventsDisjointness(t *testing.T) {
	slot := ProspectiveTarget{Scope: "host-filesystem", Kind: "file-slot", Identity: "/usr/local/bin/rustc"}
	other := ProspectiveTarget{Scope: "host-filesystem", Kind: "file-slot", Identity: "/opt/bin/rustc"}
	a := ProspectiveEffectEnvelope{Completeness: EffectExact, Steps: []ProspectiveEffectStep{{Completeness: EffectExact, Targets: []ProspectiveTarget{slot}}}}
	b := ProspectiveEffectEnvelope{Completeness: EffectExact, Steps: []ProspectiveEffectStep{{Completeness: EffectExact, Targets: []ProspectiveTarget{other}}}}
	if got := CompareProspectiveEffectEnvelopes(a, b); got != OverlapUnknown {
		t.Fatalf("distinct paths without non-aliasing proof = %q, want unknown", got)
	}
	b.Steps[0].Targets = nil
	if got := CompareProspectiveEffectEnvelopes(a, b); got != OverlapUnknown {
		t.Fatalf("missing targets = %q, want unknown", got)
	}
	b.Steps[0].Targets = []ProspectiveTarget{{}}
	if got := CompareProspectiveEffectEnvelopes(a, b); got != OverlapUnknown {
		t.Fatalf("invalid targets = %q, want unknown", got)
	}
	if got := CompareProspectiveEffectEnvelopes(ProspectiveEffectEnvelope{}, ProspectiveEffectEnvelope{}); got != OverlapUnknown {
		t.Fatalf("empty envelopes = %q, want unknown", got)
	}
}

func BenchmarkCompareProspectiveEffectEnvelopes(b *testing.B) {
	for _, count := range []int{1, 10, 100} {
		a := ProspectiveEffectEnvelope{Steps: []ProspectiveEffectStep{{}}}
		other := ProspectiveEffectEnvelope{Steps: []ProspectiveEffectStep{{}}}
		for i := 0; i < count; i++ {
			a.Steps[0].Targets = append(a.Steps[0].Targets, ProspectiveTarget{"host-filesystem", "file-slot", "/tmp/a-" + strconv.Itoa(i)})
			other.Steps[0].Targets = append(other.Steps[0].Targets, ProspectiveTarget{"host-filesystem", "file-slot", "/tmp/b-" + strconv.Itoa(i)})
		}
		b.Run(fmt.Sprintf("targets=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = CompareProspectiveEffectEnvelopes(a, other)
			}
		})
	}
}
