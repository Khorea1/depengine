package plan_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func TestLockProjectionGolden(t *testing.T) {
	tests := []struct {
		name string
		plan plan.ResolvedInstallPlan
	}{
		{name: "github_artifact", plan: githubArtifactPlan()},
		{name: "native_package", plan: nativePackagePlan()},
		{name: "git_revision", plan: gitRevisionPlan()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			projection, err := plan.ProjectLock(tt.plan)
			if err != nil {
				t.Fatalf("ProjectLock() error: %v", err)
			}
			if err := projection.RequireImmutable(); err != nil {
				t.Fatalf("fixture lock is not immutable: %v", err)
			}
			got, err := json.MarshalIndent(projection, "", "  ")
			if err != nil {
				t.Fatalf("MarshalIndent() error: %v", err)
			}
			got = append(got, '\n')

			path := filepath.Join("testdata", tt.name+".lock.golden.json")
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
				t.Fatalf("lock projection differs from %s\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
			}
		})
	}
}
