package state

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/Khorea1/depengine/internal/privatepath"
)

// mutationLocks holds the per-user host lock until after the legacy state-root
// lock is released. All callers must acquire them in this order.
type mutationLocks struct {
	host  io.Closer
	state io.Closer
}

func (m *mutationLocks) Close() error {
	return errors.Join(m.state.Close(), m.host.Close())
}

// LockMutationsContext serializes depengine CLI mutations across processes
// sharing the same per-user host namespace, even when XDG_STATE_HOME differs.
// It also holds the state-root lock for interoperability with older clients.
// These are serialization guards, not resource ownership or crash recovery.
// Acquire before any state/lockfile locks, and hold through final persistence.
func LockMutationsContext(ctx context.Context) (io.Closer, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// TempRoot is a stable, owner-validated per-user location independent of
	// the state root. Unix uses a UID-named private directory; Windows uses
	// the user's OS cache/profile directory.
	root, err := privatepath.TempRoot()
	if err != nil {
		return nil, fmt.Errorf("resolve per-user mutation lock location: %w", err)
	}
	host, err := lockFilePathContext(ctx, filepath.Join(root, "host.mutations.lock"), exclusiveLockMode, "acquire per-user host mutation lock")
	if err != nil {
		return nil, fmt.Errorf("per-user host mutation lock: %w", err)
	}

	path, err := defaultPath()
	if err != nil {
		_ = host.Close()
		return nil, fmt.Errorf("resolve state mutation lock location: %w", err)
	}
	local, err := lockFilePathContext(ctx, path+".mutations.lock", exclusiveLockMode, "acquire state mutation lock")
	if err != nil {
		_ = host.Close()
		return nil, fmt.Errorf("state mutation lock: %w", err)
	}
	return &mutationLocks{host: host, state: local}, nil
}
