package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func runCompletion(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: depengine completion bash|zsh|fish")
		os.Exit(2)
	}
	shell := args[0]

	// Whitelist: only known shells to prevent path traversal.
	switch shell {
	case "bash", "zsh", "fish":
	default:
		fmt.Fprintf(os.Stderr, "error: unsupported shell %q (supported: bash, zsh, fish)\n", shell)
		os.Exit(2)
	}

	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "scripts", "depengine-completion."+shell)
		if data, err := os.ReadFile(candidate); err == nil {
			fmt.Print(string(data))
			return
		}
	}

	scriptPath := filepath.Join("scripts", "depengine-completion."+shell)
	if data, err := os.ReadFile(scriptPath); err == nil {
		fmt.Print(string(data))
		return
	}

	fmt.Fprintf(os.Stderr, "error: completion script not found for %s\n", shell)
	os.Exit(2)
}
