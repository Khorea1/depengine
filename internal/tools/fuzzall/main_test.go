package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeDiscoveryUsesGoSignatureAndIgnoresComments(t *testing.T) {
	root, _, manifest := newFixture(t)
	writeFile(t, manifest, "./pkg FuzzAlpha\n")

	targets, err := validateFixture(context.Background(), root, manifest)
	if err != nil {
		t.Fatalf("validate fixture: %v", err)
	}
	want := []Target{{Package: "./pkg", Name: "FuzzAlpha"}}
	assertTargetsEqual(t, targets, want)
}

func TestRootPackageUsesDotPath(t *testing.T) {
	root, _, manifest := newFixture(t)
	writeFile(t, filepath.Join(root, "fuzz_test.go"), `package fuzzfixture
import "testing"
func FuzzRoot(seed *testing.F) {}
`)
	writeFile(t, manifest, ". FuzzRoot\n./pkg FuzzAlpha\n")

	targets, err := validateFixture(context.Background(), root, manifest)
	if err != nil {
		t.Fatalf("validate fixture: %v", err)
	}
	want := []Target{
		{Package: ".", Name: "FuzzRoot"},
		{Package: "./pkg", Name: "FuzzAlpha"},
	}
	assertTargetsEqual(t, targets, want)
}

func TestMissingRuntimeTargetFails(t *testing.T) {
	root, _, manifest := newFixture(t)
	writeFile(t, manifest, "./pkg FuzzMissing\n")

	_, err := validateFixture(context.Background(), root, manifest)
	assertErrorContains(t, err, "missing or renamed targets", "FuzzMissing")
}

func TestNewRuntimeTargetCannotBeOmitted(t *testing.T) {
	root, source, manifest := newFixture(t)
	writeFile(t, source, `package pkg
import "testing"
func FuzzAlpha(seed *testing.F) {}
func FuzzNew(seed *testing.F) {}
`)
	writeFile(t, manifest, "./pkg FuzzAlpha\n")

	_, err := validateFixture(context.Background(), root, manifest)
	assertErrorContains(t, err, "unlisted fuzz targets", "FuzzNew")
}

func TestBuildTaggedTargetMissingAtRuntimeFails(t *testing.T) {
	root, source, manifest := newFixture(t)
	writeFile(t, source, `//go:build never

package pkg
import "testing"
func FuzzTagged(seed *testing.F) {}
`)
	writeFile(t, manifest, "./pkg FuzzTagged\n")

	_, err := validateFixture(context.Background(), root, manifest)
	assertErrorContains(t, err, "missing or renamed targets", "FuzzTagged")
}

func TestEmptyManifestAndDiscoveryFail(t *testing.T) {
	root, source, manifest := newFixture(t)
	writeFile(t, source, "package pkg\n")
	writeFile(t, manifest, "# no targets\n")

	_, err := validateFixture(context.Background(), root, manifest)
	assertErrorContains(t, err, "no runnable fuzz targets")
}

func TestDuplicateManifestEntryFails(t *testing.T) {
	root, _, manifest := newFixture(t)
	writeFile(t, manifest, "./pkg FuzzAlpha\n./pkg FuzzAlpha\n")

	_, err := validateFixture(context.Background(), root, manifest)
	assertErrorContains(t, err, "duplicate fuzz target entry")
}

func TestInvalidManifestEntryFails(t *testing.T) {
	_, _, manifest := newFixture(t)
	writeFile(t, manifest, "./pkg Fuzz-Invalid\n")

	_, err := readManifest(manifest)
	assertErrorContains(t, err, "expected '<package> <FuzzTarget>'")
}

func newFixture(t *testing.T) (root, source, manifest string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/fuzzfixture\n\ngo 1.23\n")
	if err := os.Mkdir(filepath.Join(root, "pkg"), 0o750); err != nil {
		t.Fatal(err)
	}
	source = filepath.Join(root, "pkg", "fuzz_test.go")
	writeFile(t, source, `package pkg
import "testing"
// func FuzzCommentedFake(f *testing.F) {}
func FuzzAlpha(seed *testing.F) {}
`)
	manifest = filepath.Join(root, "targets.txt")
	return root, source, manifest
}

func validateFixture(ctx context.Context, root, manifest string) ([]Target, error) {
	expected, err := readManifest(manifest)
	if err != nil {
		return nil, err
	}
	discovered, err := discoverTargets(ctx, root)
	if err != nil {
		return nil, err
	}
	if err := validateTargets(expected, discovered); err != nil {
		return nil, err
	}
	return expected, nil
}

func writeFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertTargetsEqual(t *testing.T, got, want []Target) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("targets length = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("target[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func assertErrorContains(t *testing.T, err error, fragments ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, fragment := range fragments {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("error %q does not contain %q", err, fragment)
		}
	}
}
