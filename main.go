package main

import (
	"fmt"
	"os"

	"depengine/pkg/exec"
	"depengine/pkg/lang"
)

var version = "dev"

func main() {
	initAdapters()
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}
	switch os.Args[1] {
	case "install":
		runInstall(os.Args[2:])
	case "update":
		runUpdate(os.Args[2:])
	case "validate":
		runValidate(os.Args[2:])
	case "check":
		runCheck(os.Args[2:])
	case "graph":
		runGraph(os.Args[2:])
	case "status":
		runStatus(os.Args[2:])
	case "forget":
		runForget(os.Args[2:])
	case "remove":
		runRemove(os.Args[2:])
	case "why":
		runWhy(os.Args[2:])
	case "undo":
		runUndo(os.Args[2:])
	case "sbom":
		runSBOM(os.Args[2:])
	case "diff":
		runDiff(os.Args[2:])
	case "version", "-v", "--version":
		fmt.Println("depengine " + version)
		fmt.Println("Motor distro-agnostic de instalação de dependências")
	case "help", "-h", "--help":
		printUsage()
	case "completion":
		runCompletion(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func initAdapters() {
	exec.Register(exec.NewNativeAdapter(""))
	exec.RegisterNativeManagerAliases()
	lang.RegisterAll("paru")
}
