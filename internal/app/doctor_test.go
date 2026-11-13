package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Khorea1/depengine/internal/run"
)

func TestDoctorReportsMissingSchema(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("DEPENGINE_MANIFEST", "")

	fake := &run.FakeRunner{LookPaths: map[string]bool{"pacman": true}}
	report := buildDoctorReport(context.Background(), fake, doctorOptions{
		schemaPath:     filepath.Join(dir, "missing.toml"),
		schemaExplicit: true,
		noManifest:     true,
		noCatalog:      true,
	})

	if report.Layers.Schema.Present {
		t.Fatal("missing schema reported as present")
	}
	if !problemContains(report.Problems, "no schema found") {
		t.Fatalf("problems = %v, want a missing-schema problem", report.Problems)
	}
	if report.Lock.Present {
		t.Fatal("lock reported as present without a project")
	}
}

func TestDoctorAcceptsValidSchema(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("DEPENGINE_MANIFEST", "")
	schemaPath := filepath.Join(dir, "schema.toml")
	if err := os.WriteFile(schemaPath, []byte("schema_version = 1\n[tools]\nsimple = [\"zsh\"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	fake := &run.FakeRunner{LookPaths: map[string]bool{"pacman": true}}
	report := buildDoctorReport(context.Background(), fake, doctorOptions{
		schemaPath:     schemaPath,
		schemaExplicit: true,
		noManifest:     true,
		noCatalog:      true,
	})

	if !report.Layers.Schema.Present {
		t.Fatal("valid schema reported as absent")
	}
	if report.Layers.Schema.Path != schemaPath {
		t.Fatalf("schema path = %q, want %q", report.Layers.Schema.Path, schemaPath)
	}
	for _, problem := range report.Problems {
		if strings.HasPrefix(problem, "schema:") {
			t.Fatalf("valid schema reported a problem: %s", problem)
		}
	}
	if report.Lock.Present {
		t.Fatal("lock reported as present for a schema without depengine.lock")
	}
}

func TestDoctorTextRendersSections(t *testing.T) {
	var buf bytes.Buffer
	writeDoctorText(&buf, doctorReport{
		Host:     doctorHost{Target: "arch (arch) · x86_64", DetectionMethod: "os-release", Confidence: "high"},
		Problems: []string{"something is wrong"},
		Lock:     doctorLock{Path: "/tmp/depengine.lock"},
	})
	out := buf.String()
	for _, want := range []string{"Host", "Layers", "Managers", "Elevation", "Lock", "Problems", "something is wrong"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor text missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorCommandIsRegisteredWithExample(t *testing.T) {
	cmd := newDoctorCmd()
	if cmd.Example == "" {
		t.Fatal("doctor command has no Example block")
	}
	root := newRootCmd()
	found, _, err := root.Find([]string{"doctor"})
	if err != nil || found.Name() != "doctor" {
		t.Fatalf("doctor not registered on the root command: %v", err)
	}
}

func problemContains(problems []string, substr string) bool {
	for _, problem := range problems {
		if strings.Contains(problem, substr) {
			return true
		}
	}
	return false
}
