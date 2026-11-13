package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// auditFixture writes a schema with one static-checksum HTTP tool so the
// lockfile checks stay offline and deterministic.
func auditFixture(t *testing.T, checksum string) (schemaPath string, env func()) {
	t.Helper()
	dir := t.TempDir()
	stateHome := t.TempDir()
	schemaPath = filepath.Join(dir, "schema.toml")
	body := "schema_version = 1\n\n[tools.demo.http]\nurl = \"https://example.com/demo\"\nchecksum = \"sha256:" + checksum + "\"\n"
	if err := os.WriteFile(schemaPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_STATE_HOME", stateHome)
	t.Setenv("HOME", t.TempDir())
	return schemaPath, func() {}
}

func runAuditForTest(t *testing.T, schemaPath string, format string, strict bool) (auditReport, error) {
	t.Helper()
	manifest, noManifest := "", true
	var report auditReport
	var runErr error
	out := captureStdout(t, func() {
		runErr = runAudit(context.Background(), &schemaPath, &manifest, &noManifest, &format, &strict, nil)
	})
	if out == "" {
		return report, runErr
	}
	envelope := decodeEnvelope(t, out)
	if envelope.Command != "audit" {
		t.Fatalf("envelope command = %q, want audit", envelope.Command)
	}
	if err := json.Unmarshal(envelope.Data, &report); err != nil {
		t.Fatalf("decode audit data: %v\n%s", err, out)
	}
	return report, runErr
}

func findAuditFinding(report auditReport, code string) *auditFinding {
	for i := range report.Findings {
		if report.Findings[i].Code == code {
			return &report.Findings[i]
		}
	}
	return nil
}

func TestAuditReportsMissingLockAndToolWithoutFailing(t *testing.T) {
	schemaPath, _ := auditFixture(t, strings.Repeat("a", 64))

	report, err := runAuditForTest(t, schemaPath, "json", false)
	if err != nil {
		t.Fatalf("audit without --strict should exit 0, got %v", err)
	}
	if findAuditFinding(report, "W_LOCK_MISSING") == nil {
		t.Fatalf("findings missing W_LOCK_MISSING: %+v", report.Findings)
	}
	missing := findAuditFinding(report, "W_TOOL_MISSING")
	if missing == nil || missing.Tool != "demo" {
		t.Fatalf("findings missing W_TOOL_MISSING for demo: %+v", report.Findings)
	}
	if !strings.Contains(missing.Remediation, "install --only demo") {
		t.Errorf("remediation = %q, want an install command", missing.Remediation)
	}
	if report.Summary.Errors != 0 || report.Summary.Warnings != 2 {
		t.Errorf("summary = %+v, want 0 errors and 2 warnings", report.Summary)
	}
}

func TestAuditStrictFailsOnWarnings(t *testing.T) {
	schemaPath, _ := auditFixture(t, strings.Repeat("a", 64))

	_, err := runAuditForTest(t, schemaPath, "json", true)
	if code := requireExitCode(t, err); code != 1 {
		t.Fatalf("audit --strict exit = %d, want 1", code)
	}
}

func TestAuditInvalidSchemaExitsTwo(t *testing.T) {
	schemaPath, _ := auditFixture(t, strings.Repeat("a", 64))
	if err := os.WriteFile(schemaPath, []byte("invalid = [toml\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := runAuditForTest(t, schemaPath, "json", false)
	if code := requireExitCode(t, err); code != 2 {
		t.Fatalf("audit invalid schema exit = %d, want 2", code)
	}
	finding := findAuditFinding(report, "E_SCHEMA_INVALID")
	if finding == nil || finding.Severity != "error" {
		t.Fatalf("findings missing E_SCHEMA_INVALID error: %+v", report.Findings)
	}
}

func TestAuditFlagsStaleLockAfterMethodChange(t *testing.T) {
	schemaPath, _ := auditFixture(t, strings.Repeat("a", 64))
	runTestUpdate(t, schemaPath, "")

	clean, err := runAuditForTest(t, schemaPath, "json", false)
	if err != nil {
		t.Fatalf("audit after update: %v", err)
	}
	if clean.Summary.Errors != 0 {
		t.Fatalf("fresh lock should have no errors, got %+v", clean.Findings)
	}
	if clean.LockVersion == 0 {
		t.Fatal("audit did not report a lock version")
	}

	// Adding a method changes the tool's method identity, so the frozen lock
	// must be reported as stale until update rewrites it.
	body := "schema_version = 1\n\n[tools.demo.http]\nurl = \"https://example.com/demo\"\nchecksum = \"sha256:" + strings.Repeat("a", 64) + "\"\n\n[tools.demo.native]\npkg = \"demo\"\n"
	if err := os.WriteFile(schemaPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	stale, err := runAuditForTest(t, schemaPath, "json", false)
	if code := requireExitCode(t, err); code != 1 {
		t.Fatalf("audit stale lock exit = %d, want 1", code)
	}
	if findAuditFinding(stale, "E_LOCK_STALE") == nil {
		t.Fatalf("findings missing E_LOCK_STALE: %+v", stale.Findings)
	}
}

func TestAuditUnreadableLockIsAnError(t *testing.T) {
	schemaPath, _ := auditFixture(t, strings.Repeat("a", 64))
	if err := os.WriteFile(filepath.Join(filepath.Dir(schemaPath), "depengine.lock"), []byte("not = [toml\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := runAuditForTest(t, schemaPath, "json", false)
	if code := requireExitCode(t, err); code != 1 {
		t.Fatalf("audit unreadable lock exit = %d, want 1", code)
	}
	if findAuditFinding(report, "E_LOCK_UNREADABLE") == nil {
		t.Fatalf("findings missing E_LOCK_UNREADABLE: %+v", report.Findings)
	}
}
