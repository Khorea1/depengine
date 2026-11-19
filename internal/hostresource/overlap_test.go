package hostresource

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Khorea1/depengine/internal/plan"
)

func fileSlot(path string) plan.ProspectiveTarget {
	return plan.ProspectiveTarget{Scope: "host-filesystem", Kind: "file-slot", Identity: path}
}

func TestCompareHostFileSlotsRecognizesProvenAliases(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(real, "tool")
	if err := os.WriteFile(original, []byte("tool"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	symErr := os.Symlink(real, alias)
	link := filepath.Join(real, "hardlink")
	linkErr := os.Link(original, link)

	tests := []struct {
		name                        string
		a, b                        plan.ProspectiveTarget
		want                        plan.ResourceOverlap
		needsSymlink, needsHardlink bool
	}{
		{"same declared target", fileSlot(original), fileSlot(original), plan.OverlapSame, false, false},
		{"symlinked parent", fileSlot(original), fileSlot(filepath.Join(alias, "tool")), plan.OverlapSame, true, false},
		{"hardlinks share existing inode", fileSlot(original), fileSlot(link), plan.OverlapSame, false, true},
		{"same absent slot through parent alias", fileSlot(filepath.Join(real, "missing")), fileSlot(filepath.Join(alias, "missing")), plan.OverlapSame, true, false},
		{"distinct existing files not proven disjoint", fileSlot(original), fileSlot(filepath.Join(real, "other")), plan.OverlapUnknown, false, false},
		{"distinct absent slots not proven disjoint", fileSlot(filepath.Join(real, "absent-one")), fileSlot(filepath.Join(real, "absent-two")), plan.OverlapUnknown, false, false},
	}
	if err := os.WriteFile(filepath.Join(real, "other"), []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.needsSymlink && symErr != nil {
				t.Skipf("directory symlinks unsupported: %v", symErr)
			}
			if tc.needsHardlink && linkErr != nil {
				t.Skipf("hardlinks unsupported: %v", linkErr)
			}
			if got := CompareHostFileSlots(tc.a, tc.b); got != tc.want {
				t.Fatalf("forward: got %q, want %q", got, tc.want)
			}
			if got := CompareHostFileSlots(tc.b, tc.a); got != tc.want {
				t.Fatalf("reverse: got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCompareHostFileSlotsFailsClosedOnUnverifiableNamespaces(t *testing.T) {
	root := t.TempDir()
	existing := filepath.Join(root, "file")
	if err := os.WriteFile(existing, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(root, "broken")
	brokenErr := os.Symlink(filepath.Join(root, "nonexistent"), broken)
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	base := fileSlot(existing)
	cases := []struct {
		name string
		a, b plan.ProspectiveTarget
	}{
		{"different scope", base, plan.ProspectiveTarget{Scope: "container:abc", Kind: "file-slot", Identity: existing}},
		{"different resource kind", base, plan.ProspectiveTarget{Scope: "host-filesystem", Kind: "native-package", Identity: existing}},
		{"relative path", base, fileSlot("relative/path")},
		{"final symlink", base, fileSlot(broken)},
		{"broken parent", base, fileSlot(filepath.Join(broken, "file"))},
		{"directory occupying file slot", base, fileSlot(directory)},
		{"missing parent", base, fileSlot(filepath.Join(root, "missing", "file"))},
		{"empty identity", base, fileSlot("")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if brokenErr != nil && (tc.name == "final symlink" || tc.name == "broken parent") {
				t.Skipf("symlinks unsupported: %v", brokenErr)
			}
			if got := CompareHostFileSlots(tc.a, tc.b); got != plan.OverlapUnknown {
				t.Fatalf("forward: got %q, want unknown", got)
			}
			if got := CompareHostFileSlots(tc.b, tc.a); got != plan.OverlapUnknown {
				t.Fatalf("reverse: got %q, want unknown", got)
			}
		})
	}
}
