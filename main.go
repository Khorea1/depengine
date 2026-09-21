package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Khorea1/depengine/internal/container"
	"github.com/Khorea1/depengine/internal/ecosystem"
	"github.com/Khorea1/depengine/internal/exec"
	gitadapter "github.com/Khorea1/depengine/internal/git"
	"github.com/Khorea1/depengine/internal/httpdownload"
	"github.com/Khorea1/depengine/internal/localartifactadapter"
	"github.com/Khorea1/depengine/internal/msi"
)

var version = "dev"

func main() {
	initAdapters()
	// SIGINT/SIGTERM cancel in-flight work instead of killing the
	// process mid-mutation: adapter subprocesses receive SIGTERM as a
	// group (see internal/run) and the preparation journal stays in a
	// recoverable state for the next run.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := newRootCmd()
	root.SetArgs(normalizeArgs(os.Args[1:]))
	if err := root.ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

// normalizeArgs rewrites single-dash long flags (e.g. `-schema path`) into
// Cobra's expected double-dash form (`--schema path`). The old stdlib flag
// package treated `-x` and `--x` identically, so existing scripts and
// muscle memory used either interchangeably; pflag (Cobra's flag parser)
// does not, and reads `-schema` as a run of single-letter shorthands. Real
// shorthands (`-v`, `-h`) and already-double-dash flags pass through as-is.
func normalizeArgs(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		if len(a) > 2 && a[0] == '-' && a[1] != '-' {
			out[i] = "-" + a
		} else {
			out[i] = a
		}
	}
	return out
}

func initAdapters() {
	exec.Register(exec.NewNativeAdapter(""))
	exec.RegisterNativeManagerAliases()
	ecosystem.RegisterAll("paru")
	exec.Register(gitadapter.NewGitAdapter())
	exec.Register(localartifactadapter.NewAdapter())
	exec.Register(httpdownload.NewHTTPAdapter())
	exec.Register(httpdownload.NewGitHubAdapter())
	exec.Register(httpdownload.NewAppImageAdapter())
	exec.Register(httpdownload.NewAndroidAdapter())
	exec.Register(msi.NewAdapter())
	exec.Register(container.NewContainerAdapter())
	for _, adapter := range exec.WindowsAdapters() {
		exec.Register(adapter)
	}
}
