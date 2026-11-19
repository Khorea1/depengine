// Package hostresource contains opt-in host probes for resource-overlap
// diagnostics. The pure planning model in internal/plan remains host-neutral.
package hostresource

import (
	"os"
	"path/filepath"

	"github.com/Khorea1/depengine/internal/plan"
)

// CompareHostFileSlots looks for positive evidence that two prospective raw
// file destinations could affect the same physical resource. Directory aliases
// are compared using the existing parent directories' filesystem identities;
// existing regular files are compared using os.SameFile (including hardlinks).
//
// Different paths, inodes, mounts, user scopes or package-manager namespaces
// are NOT proof of independence. Unknown is the only negative outcome: this
// probe can never return Disjoint. This is an optional, read-only diagnostic,
// not a claim, lock, authorization, or stable write-set. Any positive evidence
// must be revalidated under a future host transaction before a mutation.
func CompareHostFileSlots(a, b plan.ProspectiveTarget) plan.ResourceOverlap {
	if a.Scope != "host-filesystem" || b.Scope != "host-filesystem" ||
		a.Kind != "file-slot" || b.Kind != "file-slot" {
		return plan.OverlapUnknown
	}
	left, leftOK := inspectFileSlot(a.Identity)
	right, rightOK := inspectFileSlot(b.Identity)
	if !leftOK || !rightOK {
		return plan.OverlapUnknown
	}
	if left.name == right.name && os.SameFile(left.parent, right.parent) {
		return plan.OverlapSame
	}
	if left.file != nil && right.file != nil && os.SameFile(left.file, right.file) {
		return plan.OverlapSame
	}
	return plan.OverlapUnknown
}

type fileSlotEvidence struct {
	name   string
	parent os.FileInfo
	file   os.FileInfo // nil only if the slot is verified absent
}

func inspectFileSlot(path string) (fileSlotEvidence, bool) {
	if path == "" || !filepath.IsAbs(path) {
		return fileSlotEvidence{}, false
	}
	name := filepath.Base(path)
	if name == "." || name == string(filepath.Separator) {
		return fileSlotEvidence{}, false
	}
	parentPath, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return fileSlotEvidence{}, false
	}
	parent, err := os.Stat(parentPath)
	if err != nil || !parent.IsDir() {
		return fileSlotEvidence{}, false
	}
	// Deliberately use Lstat: following a symlink at the final target would
	// confuse a declared file slot with the file currently linked from it.
	file, err := os.Lstat(filepath.Join(parentPath, name))
	if err != nil {
		if !os.IsNotExist(err) {
			return fileSlotEvidence{}, false
		}
		return fileSlotEvidence{name: name, parent: parent}, true
	}
	if !file.Mode().IsRegular() {
		return fileSlotEvidence{}, false
	}
	return fileSlotEvidence{name: name, parent: parent, file: file}, true
}
