package app

import (
	"context"
	"fmt"

	"github.com/Khorea1/depengine/internal/state"
)

// holdHostMutations excludes concurrent mutating depengine CLI operations,
// including state-only forget. Read-only dry runs do not take the lock or
// create its lock file. Acquire before any other state or lockfile lock.
func holdHostMutations(ctx context.Context, readOnly bool) (func(), error) {
	if readOnly {
		return func() {}, nil
	}
	lock, err := state.LockMutationsContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("host mutation lock: %w", err)
	}
	return func() { _ = lock.Close() }, nil
}
