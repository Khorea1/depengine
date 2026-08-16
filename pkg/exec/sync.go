package exec

import (
	"context"

	"github.com/Khorea1/depengine/pkg/native"
	"github.com/Khorea1/depengine/pkg/run"
)

// SyncManager handles native package manager index synchronization.
// It ensures NeedsSync runs at most once per execution session.
type SyncManager struct {
	rn     run.Runner
	clan   string
	synced bool
}

// NewSyncManager creates a SyncManager for the given clan.
func NewSyncManager(rn run.Runner, clan string) *SyncManager {
	return &SyncManager{rn: rn, clan: clan}
}

// NeedsSync returns true if the native manager requires index sync.
func (sm *SyncManager) NeedsSync() bool {
	cmd := native.BuildSyncCmd(sm.clan)
	return cmd != nil
}

// Sync runs the sync command if needed and not yet synced. Returns nil if
// sync is not needed, already done, or succeeds. Logs warning on failure
// but does not abort (install may still work from local cache).
func (sm *SyncManager) Sync(ctx context.Context) error {
	if sm.synced {
		return nil
	}
	cmd := native.BuildSyncCmd(sm.clan)
	if cmd == nil {
		return nil
	}

	res := sm.rn.Run(ctx, cmd[0], cmd[1:]...)
	if err := run.CheckResult(res, "sync"); err != nil {
		return err
	}

	sm.synced = true
	return nil
}
