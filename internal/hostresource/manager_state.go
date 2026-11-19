package hostresource

import (
	"os"
	"path/filepath"

	"github.com/Khorea1/depengine/internal/plan"
)

// CompareHostManagerStateRoots probes whether two declared native-manager
// state roots are the same filesystem object. A root may be an existing
// database file or directory. os.Stat follows directory symlinks and os.SameFile
// recognizes filesystem aliases (including hardlinks to database files).
//
// Same proves only a shared state-root object at the time of the probe. It
// does not prove equivalent manager identities, complete manager write-sets,
// ownership, confinement to the root, or safety to mutate. Different roots
// (including different containers, users, bind mounts, or package managers)
// are Unknown, NEVER Disjoint. This is an optional, read-only diagnostic;
// callers must not use its result to authorize installations or removals.
//
// The target kind is reserved for trusted adapter-provided state-root paths,
// not for free-form package names or user-provided namespace labels.
func CompareHostManagerStateRoots(a, b plan.ProspectiveTarget) plan.ResourceOverlap {
	left, leftOK := inspectManagerStateRoot(a)
	right, rightOK := inspectManagerStateRoot(b)
	if leftOK && rightOK && os.SameFile(left, right) {
		return plan.OverlapSame
	}
	return plan.OverlapUnknown
}

func inspectManagerStateRoot(target plan.ProspectiveTarget) (os.FileInfo, bool) {
	if target.Scope != "host-filesystem" || target.Kind != "manager-state-root" ||
		!filepath.IsAbs(target.Identity) {
		return nil, false
	}
	info, err := os.Stat(target.Identity)
	if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
		return nil, false
	}
	return info, true
}
