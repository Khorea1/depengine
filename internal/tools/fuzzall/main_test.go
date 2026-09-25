package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	runpkg "github.com/Khorea1/depengine/internal/run"
)

func TestRuntimeDiscoveryUsesGoSignatureAndIgnoresComments(t *testing.T) {
	root, _, manifest := newFixture(t)
	writeFile(t, manifest, "./pkg FuzzAlpha\n")
	targets, err := validateFixture(context.Background(), root, manifest)
	if err != nil {
		t.Fatalf("validate fixture: %v", err)
	}
	assertTargetsEqual(t, targets, []Target{{Package: "./pkg", Name: "FuzzAlpha"}})
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
	assertTargetsEqual(t, targets, []Target{{Package: ".", Name: "FuzzRoot"}, {Package: "./pkg", Name: "FuzzAlpha"}})
}

func TestMissingDeclaredTargetFails(t *testing.T) {
	root, _, manifest := newFixture(t)
	writeFile(t, manifest, "./pkg FuzzMissing\n")
	_, err := validateFixture(context.Background(), root, manifest)
	assertErrorContains(t, err, "unlisted fuzz targets", "FuzzAlpha", "missing or renamed targets", "FuzzMissing")
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

func TestBuildTaggedTargetIsTrackedWithoutBeingRunnable(t *testing.T) {
	root, source, manifest := newFixture(t)
	writeFile(t, source, `//go:build never

package pkg
import "testing"
func FuzzTagged(seed *testing.F) {}
`)
	writeFile(t, manifest, "./pkg FuzzTagged\n")
	targets, err := validateFixture(context.Background(), root, manifest)
	if err != nil {
		t.Fatalf("validate fixture: %v", err)
	}
	assertTargetsEqual(t, targets, []Target{{Package: "./pkg", Name: "FuzzTagged"}})
	runnable, err := discoverTargets(context.Background(), root, runpkg.OSExecRunner{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := targetSet(runnable)[Target{Package: "./pkg", Name: "FuzzTagged"}]; ok {
		t.Fatal("build-tagged target unexpectedly runnable")
	}
}

func TestBuildTaggedTargetCannotBeOmitted(t *testing.T) {
	root, source, manifest := newFixture(t)
	writeFile(t, source, `//go:build never

package pkg
import "testing"
func FuzzTagged(seed *testing.F) {}
`)
	writeFile(t, manifest, "# omitted\n")
	_, err := validateFixture(context.Background(), root, manifest)
	assertErrorContains(t, err, "unlisted fuzz targets", "FuzzTagged")
}

func TestStaticInventoryUnderstandsTestingAlias(t *testing.T) {
	root, source, _ := newFixture(t)
	writeFile(t, source, `package pkg
import testpkg "testing"
func FuzzAlias(seed *testpkg.F) {}
`)
	declared, err := discoverDeclaredTargets(root)
	if err != nil {
		t.Fatal(err)
	}
	assertTargetsEqual(t, declared, []Target{{Package: "./pkg", Name: "FuzzAlias"}})
}

func TestEmptyManifestAndDiscoveryFail(t *testing.T) {
	root, source, manifest := newFixture(t)
	writeFile(t, source, "package pkg\n")
	writeFile(t, manifest, "# no targets\n")
	_, err := validateFixture(context.Background(), root, manifest)
	assertErrorContains(t, err, "no fuzz targets discovered")
}

func TestDuplicateManifestEntryFails(t *testing.T) {
	root, _, manifest := newFixture(t)
	writeFile(t, manifest, "./pkg FuzzAlpha\n./pkg FuzzAlpha\n")
	_, err := validateFixture(context.Background(), root, manifest)
	assertErrorContains(t, err, "duplicate fuzz target entry", "FuzzAlpha")
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
	declared, err := discoverDeclaredTargets(root)
	if err != nil {
		return nil, err
	}
	runnable, err := discoverTargets(ctx, root, runpkg.OSExecRunner{})
	if err != nil {
		return nil, err
	}
	if err := validateTargets(expected, declared, runnable); err != nil {
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
