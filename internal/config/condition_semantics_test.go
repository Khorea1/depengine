package config

import (
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/platform"
)

func TestConditionKernelRange(t *testing.T) {
	facts := &platform.Facts{Kernel: "6.7.0-arch1-1"}
	cases := []struct {
		name string
		cond *Condition
		want bool
	}{
		{"exact match", &Condition{Kernel: []string{"6.7.0-arch1-1"}}, true},
		{"exact is not a prefix", &Condition{Kernel: []string{"6.7"}}, false},
		{"minimum inclusive", &Condition{KernelMin: "6.7.0"}, true},
		{"minimum above host", &Condition{KernelMin: "6.8"}, false},
		{"maximum inclusive", &Condition{KernelMax: "6.7.0-arch1-1"}, true},
		{"maximum below host", &Condition{KernelMax: "6.7"}, false},
		{"explicit range", &Condition{KernelMin: "6.6", KernelMax: "6.8"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cond.Match(facts); got != tc.want {
				t.Fatalf("Match() = %v, want %v for %+v", got, tc.want, tc.cond)
			}
		})
	}

	if (&Condition{KernelMin: "6.7"}).Match(&platform.Facts{}) {
		t.Fatal("kernel_min matched a host with no detected kernel")
	}
	if got := (&Condition{KernelMin: "6.8"}).Mismatch(facts); !strings.Contains(got, "below the inclusive minimum") {
		t.Fatalf("Mismatch() = %q, want kernel minimum explanation", got)
	}
}

func TestConditionLibcRule(t *testing.T) {
	cases := []struct {
		name   string
		cond   *Condition
		actual string
		want   bool
	}{
		{"family exact", &Condition{Libc: []string{"glibc"}}, "glibc", true},
		{"case insensitive", &Condition{Libc: []string{"GLIBC"}}, "glibc", true},
		{"prefix accepts a versioned fact", &Condition{Libc: []string{"glibc"}}, "glibc 2.35", true},
		{"musl does not match glibc", &Condition{Libc: []string{"musl"}}, "glibc", false},
		{"uClibc normalizes case", &Condition{Libc: []string{"uclibc"}}, "uClibc", true},
		{"versioned condition cannot match family token", &Condition{Libc: []string{"glibc 2.35"}}, "glibc", false},
		{"empty fact never matches", &Condition{Libc: []string{"glibc"}}, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			facts := &platform.Facts{Libc: tc.actual}
			if got := tc.cond.Match(facts); got != tc.want {
				t.Fatalf("Match() = %v, want %v (libc %q)", got, tc.want, tc.actual)
			}
		})
	}
}

func TestConditionTraitThreeState(t *testing.T) {
	cases := []struct {
		name     string
		cond     *Condition
		facts    *platform.Facts
		want     bool
		mismatch string
	}{
		{"wsl false known", &Condition{IsWSL: boolPtr(false)}, &platform.Facts{}, true, ""},
		{"wsl false unknown", &Condition{IsWSL: boolPtr(false)}, &platform.Facts{IsWSLUnknown: true}, false, "inconclusive"},
		{"wsl true detected", &Condition{IsWSL: boolPtr(true)}, &platform.Facts{IsWSL: true}, true, ""},
		{"wsl true unknown", &Condition{IsWSL: boolPtr(true)}, &platform.Facts{IsWSLUnknown: true}, false, "want true"},
		{"container false known", &Condition{IsContainer: boolPtr(false)}, &platform.Facts{}, true, ""},
		{"container false unknown", &Condition{IsContainer: boolPtr(false)}, &platform.Facts{IsContainerUnknown: true}, false, "inconclusive"},
		{"container true detected", &Condition{IsContainer: boolPtr(true)}, &platform.Facts{IsContainer: true}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cond.Match(tc.facts); got != tc.want {
				t.Fatalf("Match() = %v, want %v", got, tc.want)
			}
			if tc.mismatch != "" {
				if got := tc.cond.Mismatch(tc.facts); !strings.Contains(got, tc.mismatch) {
					t.Fatalf("Mismatch() = %q, want substring %q", got, tc.mismatch)
				}
			}
		})
	}
}

func TestConditionDimensionIndependence(t *testing.T) {
	// A method that only depends on arch must not require libc, kernel,
	// init system, or either environment trait to be declared or known.
	facts := &platform.Facts{TargetArch: "x86_64"}
	cond := &Condition{Arch: []string{"x86_64"}}
	if !cond.Match(facts) {
		t.Fatal("arch-only condition rejected a host with no other facts")
	}
}

func TestConditionAndroidMismatch(t *testing.T) {
	// Termux reports os = linux, so is_android is the only dimension that can
	// reject an Android host; the diagnostic must name it.
	cond := &Condition{IsAndroid: boolPtr(true)}
	facts := &platform.Facts{OS: "linux"}
	if cond.Match(facts) {
		t.Fatal("is_android = true matched a non-Android host")
	}
	if got := cond.Mismatch(facts); !strings.Contains(got, "is_android is false, want true") {
		t.Fatalf("Mismatch() = %q, want is_android explanation", got)
	}
}

func TestConditionKernelRangeString(t *testing.T) {
	cond := &Condition{KernelMin: "6.6", KernelMax: "6.8"}
	got := cond.String()
	if !strings.Contains(got, "kernel >= \"6.6\"") || !strings.Contains(got, "kernel <= \"6.8\"") {
		t.Fatalf("String() = %q, want kernel range", got)
	}
}
