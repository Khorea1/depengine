package config

import (
	"reflect"
	"testing"

	"github.com/Khorea1/depengine/pkg/engine"
)

func TestExpandNoOp(t *testing.T) {
	m := map[string]string{"arch": "x86_64"}
	if got := Expand("nothing here", m); got != "nothing here" {
		t.Fatalf("expected unchanged, got %q", got)
	}
	if got := Expand("", m); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestExpandSingle(t *testing.T) {
	m := map[string]string{"arch": "x86_64"}
	if got := Expand("https://example.com/{arch}/bin", m); got != "https://example.com/x86_64/bin" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandMultiple(t *testing.T) {
	m := map[string]string{"arch": "arm64", "os": "linux", "libc": "glibc"}
	got := Expand("https://x.com/{os}/{arch}/{libc}/pkg", m)
	want := "https://x.com/linux/arm64/glibc/pkg"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpandAdjacent(t *testing.T) {
	m := map[string]string{"arch": "x86_64", "os": "linux"}
	got := Expand("{os}{arch}", m)
	if got != "linuxx86_64" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandUnknownLeftAsIs(t *testing.T) {
	m := map[string]string{"arch": "x86_64"}
	// a typo must survive so the validator can flag it later, instead of being
	// silently turned into an empty string and producing a broken URL.
	got := Expand("https://x.com/{archh}/bin", m)
	if got != "https://x.com/{archh}/bin" {
		t.Fatalf("expected unknown placeholder preserved, got %q", got)
	}
}

func TestExpandPreservesPkgAndLatest(t *testing.T) {
	// {pkg} and {latest} belong to later stages; Expand must never touch them.
	m := map[string]string{"arch": "x86_64"}
	got := Expand("pacman -S {pkg} --from {arch} {latest}", m)
	if got != "pacman -S {pkg} --from x86_64 {latest}" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandReusesAcrossCalls(t *testing.T) {
	// same map in twice -> stable, no mutation of the table
	m := map[string]string{"arch": "x86_64"}
	_ = Expand("/{arch}/", m)
	if m["arch"] != "x86_64" {
		t.Fatalf("Expand mutated the substitution map: %+v", m)
	}
}

func TestBuildMapCoversAllFactsPlusClan(t *testing.T) {
	f := &engine.Facts{
		TargetArch:      "x86_64",
		DistroID:        "arch",
		DistroName:      "Arch Linux",
		DistroIDLike:    "",
		TargetFamily:    "unix",
		DetectionMethod: "os-release",
		Confidence:      "high",
		IsWSL:           false,
		IsContainer:     true,
		IsAndroid:       false,
		Kernel:          "6.7.0",
		Libc:            "glibc",
		InitSystem:      "systemd",
		OS:              "linux",
	}
	m := BuildMap(f, "arch")

	want := map[string]string{
		"id":             "arch",
		"distro_name":    "Arch Linux",
		"distro_id_like": "",
		"distro_family":  "arch",
		"target_family":  "unix",
		"arch":           "x86_64",
		"detection":      "os-release",
		"confidence":     "high",
		"kernel":         "6.7.0",
		"libc":           "glibc",
		"init_system":    "systemd",
		"os":             "linux",
		"is_wsl":         "false",
		"is_container":   "true",
		"is_android":     "false",
	}
	if !reflect.DeepEqual(m, want) {
		t.Fatalf("BuildMap mismatch:\n got: %v\nwant: %v", m, want)
	}
}

func TestExpandAllWalksNestedMapsAndSlices(t *testing.T) {
	m := map[string]string{"arch": "x86_64", "os": "linux"}
	v := map[string]any{
		"url":   "https://x.com/{os}/{arch}/bin",
		"sub":   map[string]any{"deep": "a-{arch}-b"},
		"list":  []any{"{arch}", "{os}", 7},
		"num":   42,
		"bool":  true,
		"plain": "no-placeholder",
	}
	got := ExpandAll(v, m).(map[string]any)
	if got["url"] != "https://x.com/linux/x86_64/bin" {
		t.Fatalf("url not expanded: %v", got["url"])
	}
	if got["plain"] != "no-placeholder" {
		t.Fatalf("plain changed: %v", got["plain"])
	}
	if got["num"].(int) != 42 {
		t.Fatalf("num changed: %v", got["num"])
	}
	sub := got["sub"].(map[string]any)
	if sub["deep"] != "a-x86_64-b" {
		t.Fatalf("sub.deep not expanded: %v", sub["deep"])
	}
	list := got["list"].([]any)
	if list[0] != "x86_64" || list[1] != "linux" {
		t.Fatalf("list not expanded: %v", list)
	}
	if list[2] != 7 {
		t.Fatalf("list int changed: %v", list[2])
	}
}

// TestExpandAllDoesNotMutateInput ensures ExpandAll does not mutate the input map/slice.
// This is a regression test for a bug that was fixed.
func TestExpandAllMutatesInput(t *testing.T) {
	t.Parallel()
	m := map[string]string{"arch": "x86_64", "os": "linux"}

	input := map[string]any{
		"url": "https://example.com/{arch}/{os}/file.tar.gz",
		"nested": map[string]any{
			"name": "tool-{arch}",
		},
	}

	// Deep copy the original to compare later.
	originalURL := input["url"]
	originalNestedName := input["nested"].(map[string]any)["name"]

	_ = ExpandAll(input, m)

	if input["url"] != originalURL {
		t.Fatal("BUG REPRODUCED: ExpandAll mutated top-level url in place")
	}
	if input["nested"].(map[string]any)["name"] != originalNestedName {
		t.Fatal("BUG REPRODUCED: ExpandAll mutated nested value in place")
	}
}
