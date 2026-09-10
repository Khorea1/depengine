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

// Sync runs the sync command if needed and not yet synced. It always
// returns nil: sync is a best-effort optimization, never a precondition
// for attempting an install.
//
// A failed sync — e.g. `apt-get update` exiting 100 because one unrelated
// third-party repo (a stale PPA, a Docker repo, nodesource, ...) is broken
// or unsigned, even though every index entry the requested packages
// actually need is fine — is logged at WARN by the underlying runner
// (cmd/exit/stderr) and then swallowed here rather than propagated:
//   - native installs can often still succeed against the locally cached
//     index, which may simply be a little stale rather than missing the
//     packages that were actually requested;
//   - tools whose method has nothing to do with the native manager
//     (http, git, cargo, go, ...) never depended on this sync succeeding
//     in the first place.
//
// Treating a sync failure as fatal here would veto every tool in the
// schema over a single unrelated repo problem, contradicting the "tries
// every available installation method until one succeeds" promise. See
// findings.md, Achado 1.
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
		// Do not set sm.synced: a failed attempt is not "already done",
		// so a future call within the same session (none exists today,
		// but nothing should assume that stays true) is free to retry
		// instead of being stuck reporting success it never had.
		return nil
	}

	sm.synced = true
	return nil
}
