package main

import (
	"fmt"
	"os"

	"github.com/Khorea1/depengine/pkg/i18n"
	"github.com/spf13/cobra"
)

// Command groups, in the order they should render in `depengine help`.
// These replace the hand-rolled bilingual tables in the old usage.go: Cobra
// renders one aligned, grouped listing straight from each command's Short
// text, instead of a second copy of every command name/description that had
// to be kept in sync by hand.
const (
	groupManage  = "manage"
	groupInspect = "inspect"
	groupExport  = "export"
)

// ifPT picks the PT-BR or EN variant of a short UI label — used for command
// Short/Long text so `depengine help` reads in the user's language exactly
// as it did before, without a parallel PT/EN table to maintain.
func ifPT(pt, en string) string {
	if i18n.GetLocale() == "en" {
		return en
	}
	return pt
}

// newRootCmd builds the full depengine command tree. Every flag a command
// accepts is declared exactly once, right where the command itself is
// constructed — Cobra derives --help, error messages, and shell completion
// from that single declaration instead of the three hand-maintained copies
// (main.go's switch, help.go's printCommandHelp, and the bash/zsh/fish
// completion scripts) this replaces.
func newRootCmd() *cobra.Command {
	short := ifPT(
		"Motor distro-agnóstico de instalação de dependências",
		"Distro-agnostic dependency installer",
	)

	var showVersion bool
	root := &cobra.Command{
		Use:   "depengine",
		Short: short,
		// Every runXxx function still prints its own errors and calls
		// os.Exit directly (unchanged from before this migration), so
		// RunE never actually returns a real error here — the only
		// errors Cobra itself prints are unknown command / unknown flag /
		// wrong argument count, which we want shown (this is strictly
		// better than the old CLI's plain "unknown command" line: Cobra
		// also suggests the closest match, e.g. "instal" -> "install").
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				printVersion()
				return nil
			}
			return cmd.Help()
		},
	}
	// `-v`/`--version` on the root command, in addition to the `version`
	// subcommand below — both existed before and both still work.
	root.Flags().BoolVarP(&showVersion, "version", "v", false, ifPT("Mostrar versão", "Show version"))

	root.AddGroup(
		&cobra.Group{ID: groupManage, Title: ifPT("Gerenciar ferramentas:", "Manage tools:")},
		&cobra.Group{ID: groupInspect, Title: ifPT("Inspecionar:", "Inspect:")},
		&cobra.Group{ID: groupExport, Title: ifPT("Exportar e integrar:", "Export & integrate:")},
	)

	root.AddCommand(
		newInitCmd(),
		newInstallCmd(),
		newUpgradeCmd(),
		newRemoveCmd(),
		newForgetCmd(),
		newUndoCmd(),
		newStatusCmd(),
		newCheckCmd(),
		newValidateCmd(),
		newWhyCmd(),
		newGraphCmd(),
		newDiffCmd(),
		newUpdateCmd(),
		newSBOMCmd(),
		newVersionCmd(),
	)
	root.SetHelpCommand(newHelpCmd(root))

	return root
}

func printVersion() {
	fmt.Println("depengine " + version)
	fmt.Println(ifPT(
		"Motor distro-agnóstico de instalação de dependências",
		"Distro-agnostic dependency installer",
	))
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "version",
		Short:  ifPT("Mostrar versão", "Show version"),
		Args:   cobra.NoArgs,
		Hidden: true, // discoverable via -v/--version; kept for backward compat
		Run: func(cmd *cobra.Command, args []string) {
			printVersion()
		},
	}
}

// newHelpCmd rebuilds cobra's default help command (find the target command
// by args, print its help) and adds --man, so `depengine help --man` keeps
// working exactly as before instead of moving to a new subcommand.
func newHelpCmd(root *cobra.Command) *cobra.Command {
	var man bool
	cmd := &cobra.Command{
		Use:   "help [command]",
		Short: ifPT("Ajuda sobre qualquer comando", "Help about any command"),
		Run: func(cmd *cobra.Command, args []string) {
			if man {
				printManPage()
				return
			}
			target, _, err := root.Find(args)
			if err != nil || target == nil {
				fmt.Fprintf(os.Stderr, "Unknown help topic %q\n", args)
				_ = root.Usage()
				os.Exit(1)
			}
			target.InitDefaultHelpFlag()
			_ = target.Help()
		},
	}
	cmd.Flags().BoolVar(&man, "man", false, ifPT("Mostrar a man page", "Show the man page"))
	return cmd
}
