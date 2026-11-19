package hostresource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func managerStateRoot(path string) plan.ProspectiveTarget {
	return plan.ProspectiveTarget{Scope: "host-filesystem", Kind: "manager-state-root", Identity: path}
}

func TestCompareHostManagerStateRootsRecognizesPhysicalAliases(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "database")
	if err := os.Mkdir(db, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(db, "status")
	if err := os.WriteFile(file, []byte("package status"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "database-alias")
	symlinkErr := os.Symlink(db, alias)
	hardlink := filepath.Join(root, "status-hardlink")
	hardlinkErr := os.Link(file, hardlink)

	cases := []struct {
		name    string
		a, b    plan.ProspectiveTarget
		require error
	}{
		{"identical directory root", managerStateRoot(db), managerStateRoot(db), nil},
		{"directory symlink alias", managerStateRoot(db), managerStateRoot(alias), symlinkErr},
		{"regular-file manager state", managerStateRoot(file), managerStateRoot(file), nil},
		{"file hardlink alias", managerStateRoot(file), managerStateRoot(hardlink), hardlinkErr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.require != nil {
				t.Skipf("filesystem aliases unsupported: %v", tc.require)
			}
			if got := CompareHostManagerStateRoots(tc.a, tc.b); got != plan.OverlapSame {
				t.Fatalf("forward: got %q, want same", got)
			}
			if got := CompareHostManagerStateRoots(tc.b, tc.a); got != plan.OverlapSame {
				t.Fatalf("reverse: got %q, want same", got)
			}
		})
	}
}

func TestCompareHostManagerStateRootsRejectsUnsupportedEvidence(t *testing.T) {
	root := t.TempDir()
	one := filepath.Join(root, "manager-one")
	two := filepath.Join(root, "manager-two")
	if err := os.Mkdir(one, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(two, 0700); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "missing")
	broken := filepath.Join(root, "dangling")
	symlinkErr := os.Symlink(missing, broken)
	base := managerStateRoot(one)
	cases := []struct {
		name string
		a, b plan.ProspectiveTarget
	}{
		{"different manager root objects", base, managerStateRoot(two)},
		{"missing manager root", base, managerStateRoot(missing)},
		{"relative manager root", base, managerStateRoot("../manager-one")},
		{"empty manager root", base, managerStateRoot("")},
		{"unproven container namespace", base, plan.ProspectiveTarget{Scope: "container:abc", Kind: "manager-state-root", Identity: one}},
		{"file-slot is not manager state", base, plan.ProspectiveTarget{Scope: "host-filesystem", Kind: "file-slot", Identity: one}},
		{"directory inside other directory is not equal root", managerStateRoot(root), base},
		{"dangling symlink", base, managerStateRoot(broken)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "dangling symlink" && symlinkErr != nil {
				t.Skipf("symlink unsupported: %v", symlinkErr)
			}
			if got := CompareHostManagerStateRoots(tc.a, tc.b); got != plan.OverlapUnknown {
				t.Fatalf("forward: got %q, want unknown", got)
			}
			if got := CompareHostManagerStateRoots(tc.b, tc.a); got != plan.OverlapUnknown {
				t.Fatalf("reverse: got %q, want unknown", got)
			}
		})
	}
}
