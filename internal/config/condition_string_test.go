package config

import "testing"

func TestConditionStringDeterministic(t *testing.T) {
	isContainer := false
	condition := &Condition{
		DistroFamily:     []string{"debian", "arch"},
		TargetFamily:     []string{"unix"},
		DistroVersionMin: "12",
		OS:               []string{"linux"},
		IsContainer:      &isContainer,
	}

	want := `distro_family in ["arch","debian"] && target_family in ["unix"] && distro_version >= "12" && os in ["linux"] && is_container == false`
	if got := condition.String(); got != want {
		t.Fatalf("Condition.String() = %q, want %q", got, want)
	}
}

func TestConditionStringNilAndZero(t *testing.T) {
	var nilCondition *Condition
	if got := nilCondition.String(); got != "" {
		t.Fatalf("nil Condition.String() = %q", got)
	}
	if got := (&Condition{}).String(); got != "" {
		t.Fatalf("zero Condition.String() = %q", got)
	}
}
