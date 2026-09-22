package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/Khorea1/depengine/internal/app"
)

var version = "dev"

//go:embed docs/depengine.1
var manPage string

func main() {
	app.InitAdapters()
	app.Version = version
	app.ManPage = manPage
	// SIGINT/SIGTERM cancel in-flight work instead of killing the
	// process mid-mutation: adapter subprocesses receive SIGTERM as a
	// group (see internal/run) and the preparation journal stays in a
	// recoverable state for the next run.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	root := app.NewRootCmd()
	root.SetArgs(app.NormalizeArgs(os.Args[1:]))
	if err := root.ExecuteContext(ctx); err != nil {
		var exitErr *app.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
