package main

import (
	_ "embed"
	"fmt"
	"os"
)

//go:embed scripts/depengine-completion.bash
var completionBash string

//go:embed scripts/depengine-completion.zsh
var completionZsh string

//go:embed scripts/depengine-completion.fish
var completionFish string

func runCompletion(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: depengine completion bash|zsh|fish")
		os.Exit(2)
	}
	shell := args[0]

	var script string
	switch shell {
	case "bash":
		script = completionBash
	case "zsh":
		script = completionZsh
	case "fish":
		script = completionFish
	default:
		fmt.Fprintf(os.Stderr, "error: unsupported shell %q (supported: bash, zsh, fish)\n", shell)
		os.Exit(2)
	}

	fmt.Print(script)
}
