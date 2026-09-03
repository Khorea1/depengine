package main

import (
	"fmt"
	"os"

	"github.com/Khorea1/depengine/pkg/ecosystem"
	"github.com/Khorea1/depengine/pkg/exec"
	"github.com/Khorea1/depengine/pkg/i18n"
)

var version = "dev"

func main() {
	initAdapters()
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	// `depengine <cmd> --help` renders the same aligned help as
	// `depengine help <cmd>` — without this, the flag package's ExitOnError
	// would take over and dump its default two-line-per-flag usage.
	if wantsHelp(args) {
		switch cmd {
		case "install", "update", "upgrade", "validate", "check", "graph",
			"status", "forget", "remove", "why", "undo", "sbom", "init",
			"diff":
			printCommandHelp(cmd)
			os.Exit(0)
		}
	}
	switch cmd {
	case "install":
		runInstall(args)
	case "update":
		runUpdate(args)
	case "upgrade":
		runUpgrade(args)
	case "validate":
		runValidate(args)
	case "check":
		runCheck(args)
	case "graph":
		runGraph(args)
	case "status":
		runStatus(args)
	case "forget":
		runForget(args)
	case "remove":
		runRemove(args)
	case "why":
		runWhy(args)
	case "undo":
		runUndo(args)
	case "sbom":
		runSBOM(args)
	case "init":
		runInit(args)
	case "diff":
		runDiff(args)
	case "version", "-v", "--version":
		fmt.Println("depengine " + version)
		if i18n.GetLocale() == "pt" {
			fmt.Println("Motor distro-agnóstico de instalação de dependências")
		} else {
			fmt.Println("Distro-agnostic dependency installer")
		}
	case "help", "-h", "--help":
		if len(os.Args) == 2 || (len(os.Args) > 2 && os.Args[2] == "--man") {
			if len(os.Args) > 2 && os.Args[2] == "--man" {
				printManPage()
			} else {
				printUsage()
			}
		} else {
			printCommandHelp(os.Args[2])
		}
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
	ecosystem.RegisterAll("paru")
}
