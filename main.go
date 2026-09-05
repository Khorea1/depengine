package main

import (
	"os"

	"github.com/Khorea1/depengine/pkg/ecosystem"
	"github.com/Khorea1/depengine/pkg/exec"
)

var version = "dev"

func main() {
	initAdapters()
	root := newRootCmd()
	root.SetArgs(normalizeArgs(os.Args[1:]))
	if err := root.Execute(); err != nil {
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
}
