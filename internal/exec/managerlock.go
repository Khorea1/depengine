package exec

import "sync"

// managerLockedKinds are method kinds whose mutation contends on a single
// package-manager database or lock file. Two installs for the same manager must
// not overlap even when the dependency graph puts them on the same level and
// --jobs permits concurrency: the second invocation fails on the lock or, for
// managers without a lock, corrupts their state.
var managerLockedKinds = map[string]bool{
	"native":   true,
	"aur":      true,
	"flatpak":  true,
	"snap":     true,
	"cask":     true,
	"mas":      true,
	"choco":    true,
	"scoop":    true,
	"winget":   true,
	"conda":    true,
	"nix":      true,
	"sdkman":   true,
	"asdf":     true,
	"steamcmd": true,
}

// exclusiveManagerKey returns the serialization key for a method kind. Native
// installs key on the resolved manager name so hosts that know several managers
// still serialize per actual manager; AUR helpers share one key because they
// all drive pacman.
func exclusiveManagerKey(kind, nativeManagerName string) (string, bool) {
	if !managerLockedKinds[kind] {
		return "", false
	}
	if kind == "native" {
		if nativeManagerName != "" {
			return "native:" + nativeManagerName, true
		}
		return "native", true
	}
	return kind, true
}

// lockExclusiveManager blocks until the manager's install slot is free and
// returns the unlock function. Package managers with no shared lock (downloads,
// builds, language package managers that support concurrent installs) get a
// no-op unlock so they keep running in parallel.
func (ex *Executor) lockExclusiveManager(kind, nativeManagerName string) func() {
	key, ok := exclusiveManagerKey(kind, nativeManagerName)
	if !ok {
		return func() {}
	}
	actual, _ := ex.managerLocks.LoadOrStore(key, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}
