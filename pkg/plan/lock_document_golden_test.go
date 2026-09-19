package plan_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/pkg/plan"
)

func TestLockDocumentGolden(t *testing.T) {
	doc, err := plan.BuildLockDocument([]plan.ResolvedInstallPlan{
		gitRevisionPlan(),
		githubArtifactPlan(),
		nativePackagePlan(),
	})
	if err != nil {
		t.Fatalf("BuildLockDocument() error: %v", err)
	}
	got, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent() error: %v", err)
	}
	got = append(got, '\n')

	path := filepath.Join("testdata", "universal_lock.golden.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("lock document differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
