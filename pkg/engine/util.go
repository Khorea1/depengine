package engine

import (
	"context"
	"time"
)

// timeoutUnit is the base unit of time used as multiplier for subprocess
// timeouts (e.g., GatherFacts uses 10× this). We never let a fetcher or
// (later) an install hang the engine indefinitely — this is Go's analog
// of run_cmd_safe() in detect_os.sh.
const timeoutUnit = time.Second

// timeoutCtx is the engine-wide subprocess timeout helper. Adapters
// (cargo, go install, apt-get install...) reuse this rather than each
// building their own context.
func timeoutCtx(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
