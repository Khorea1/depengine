package app

import (
	"context"
	"fmt"
	"os"

	"github.com/Khorea1/depengine/internal/run"
)

var ManPage string

func printManPage(ctx context.Context) {
	file, err := os.CreateTemp("", "depengine-man-*.1")
	if err == nil {
		path := file.Name()
		defer os.Remove(path)
		if _, err = file.WriteString(ManPage); err == nil {
			err = file.Close()
		} else {
			_ = file.Close()
		}
		if err == nil {
			result := (run.OSExecRunner{Stream: os.Stdout}).Run(ctx, "man", "-l", path)
			if result.Err == nil && result.ExitCode == 0 {
				return
			}
		}
	}

	fmt.Fprintln(os.Stderr, "warning: 'man' not found, printing raw man page")
	fmt.Print(ManPage)
}
