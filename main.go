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
