package main

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
)

//go:embed docs/depengine.1
var manPageContent string

func printManPage() {
	// Try to pipe through man -l - for proper rendering.
	cmd := exec.Command("man", "-l", "-")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	pipe, err := cmd.StdinPipe()
	if err == nil {
		if err := cmd.Start(); err == nil {
			_, _ = pipe.Write([]byte(manPageContent))
			_ = pipe.Close()
			_ = cmd.Wait()
			return
		}
	}

	// Fallback: print raw if man is unavailable.
	fmt.Fprintln(os.Stderr, "warning: 'man' not found, printing raw man page")
	fmt.Print(manPageContent)
}
