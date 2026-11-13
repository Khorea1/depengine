package exec

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestExclusiveManagerKey(t *testing.T) {
	tests := []struct {
		kind    string
		manager string
		want    string
		locked  bool
	}{
		{kind: "native", manager: "apt", want: "native:apt", locked: true},
		{kind: "native", manager: "", want: "native", locked: true},
		{kind: "aur", manager: "pacman", want: "aur", locked: true},
		{kind: "flatpak", manager: "", want: "flatpak", locked: true},
		{kind: "scoop", manager: "", want: "scoop", locked: true},
		{kind: "go", manager: "apt", want: "", locked: false},
		{kind: "http", manager: "", want: "", locked: false},
	}
	for _, tc := range tests {
		got, locked := exclusiveManagerKey(tc.kind, tc.manager)
		if got != tc.want || locked != tc.locked {
			t.Errorf("exclusiveManagerKey(%q, %q) = %q, %v; want %q, %v",
				tc.kind, tc.manager, got, locked, tc.want, tc.locked)
		}
	}
}

// TestManagerLockSerializesSameManager checks the contract that closes the
// --jobs contention bug: overlapping installations for one manager run one at a
// time.
func TestManagerLockSerializesSameManager(t *testing.T) {
	ex := New()
	var active, maxActive atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unlock := ex.lockExclusiveManager("native", "apt")
			defer unlock()
			current := active.Add(1)
			for {
				observed := maxActive.Load()
				if current <= observed || maxActive.CompareAndSwap(observed, current) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
			active.Add(-1)
		}()
	}
	wg.Wait()
	if got := maxActive.Load(); got != 1 {
		t.Fatalf("max concurrent installs for one manager = %d, want 1", got)
	}
}

// TestManagerLockKeepsDifferentManagersParallel proves serialization is scoped
// to the manager rather than to the whole executor.
func TestManagerLockKeepsDifferentManagersParallel(t *testing.T) {
	ex := New()
	entered := make(chan string, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	for _, manager := range []string{"apt", "dnf"} {
		wg.Add(1)
		go func(manager string) {
			defer wg.Done()
			unlock := ex.lockExclusiveManager("native", manager)
			defer unlock()
			entered <- manager
			<-release
		}(manager)
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			close(release)
			wg.Wait()
			t.Fatal("installs for different managers did not run in parallel")
		}
	}
	close(release)
	wg.Wait()
}

// TestManagerLockIgnoresNonManagerKinds keeps download/build methods parallel:
// they hold no package-manager lock, so acquiring twice must not deadlock.
func TestManagerLockIgnoresNonManagerKinds(t *testing.T) {
	ex := New()
	done := make(chan struct{})
	go func() {
		defer close(done)
		unlockHTTP := ex.lockExclusiveManager("http", "apt")
		defer unlockHTTP()
		unlockBuild := ex.lockExclusiveManager("github", "apt")
		defer unlockBuild()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("non-manager methods were serialized against each other")
	}
}
