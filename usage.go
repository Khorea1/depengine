package main

import (
	"fmt"
	"os"

	"github.com/Khorea1/depengine/pkg/i18n"
)

// usageGroup is a named, ordered group of commands in the top-level help.
type usageGroup struct {
	Title string
	Cmds  []usageCmd
}

// usageCmd is one row of the top-level help: command name + one-line docs.
type usageCmd struct {
	Name string
	Desc string
}

func printUsage() {
	if i18n.GetLocale() == "en" {
		printUsageEN()
	} else {
		printUsagePT()
	}
}

// renderUsage prints a grouped, aligned command listing. Column widths are
// computed from the data so a long command name never breaks alignment, and
// group titles separate "manage tools" verbs from "inspect" verbs — a fresh
// reader scans the group headers, not the raw list.
func renderUsage(c *cliStyle, groups []usageGroup, flags [][2]string) {
	w := c.w
	width := 0
	for _, g := range groups {
		for _, cmd := range g.Cmds {
			if len(cmd.Name) > width {
				width = len(cmd.Name)
			}
		}
	}

	for i, g := range groups {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, c.dim(g.Title))
		for _, cmd := range g.Cmds {
			fmt.Fprintf(w, "  %s  %s\n", c.cyan(padRight(cmd.Name, width)), cmd.Desc)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, c.dim(ifPT("Flags globais", "Global flags")))
	fwidth := 0
	for _, f := range flags {
		if len(f[0]) > fwidth {
			fwidth = len(f[0])
		}
	}
	for _, f := range flags {
		fmt.Fprintf(w, "  %s  %s\n", padRight(f[0], fwidth), f[1])
	}
}

// ifPT picks the PT-BR or EN variant of a short UI label.
func ifPT(pt, en string) string {
	if i18n.GetLocale() == "en" {
		return en
	}
	return pt
}

func printUsagePT() {
	c := newCLIStyle(os.Stdout)
	fmt.Fprintf(c.w, "%s\n\n", c.bold("depengine — motor distro-agnóstico de instalação de dependências"))
	fmt.Fprintf(c.w, "Uso: %s\n\n", c.dim("depengine <comando> [flags]"))
	renderUsage(c, []usageGroup{
		{"Gerenciar ferramentas", []usageCmd{
			{"init", "Inicializar schema.toml para um projeto"},
			{"install", "Instalar ferramentas do schema.toml"},
			{"upgrade", "Atualizar ferramentas para as versões do depengine.lock"},
			{"remove", "Remover ferramentas do sistema"},
			{"forget", "Esquecer ferramentas do estado (sem desinstalar)"},
			{"undo", "Reverter instalação via snapshot"},
		}},
		{"Inspecionar", []usageCmd{
			{"status", "Mostrar estado das ferramentas em relação ao schema"},
			{"check", "Verificar se uma ferramenta está de fato instalada"},
			{"validate", "Validar schema.toml e ambiente"},
			{"why", "Explicar por que um método foi escolhido"},
			{"graph", "Mostrar grafo de dependências"},
			{"diff", "Comparar estado entre máquinas"},
		}},
		{"Exportar e integrar", []usageCmd{
			{"update", "Resolver {latest} e fixar versões no depengine.lock"},
			{"sbom", "Exportar SBOM (CycloneDX ou SPDX)"},
			{"completion", "Gerar script de autocomplete"},
		}},
	}, [][2]string{
		{"--version, -v", "Mostrar versão"},
		{"--help, -h", "Mostrar ajuda"},
	})
	fmt.Fprintf(c.w, "\n%s\n", c.dim("Use \"depengine <comando> --help\" para flags específicas."))
}

func printUsageEN() {
	c := newCLIStyle(os.Stdout)
	fmt.Fprintf(c.w, "%s\n\n", c.bold("depengine — distro-agnostic dependency installer"))
	fmt.Fprintf(c.w, "Usage: %s\n\n", c.dim("depengine <command> [flags]"))
	renderUsage(c, []usageGroup{
		{"Manage tools", []usageCmd{
			{"init", "Initialize a schema.toml for a new project"},
			{"install", "Install tools from schema.toml"},
			{"upgrade", "Upgrade installed tools to pinned versions"},
			{"remove", "Remove tools from the system"},
			{"forget", "Forget tools from state (without uninstalling)"},
			{"undo", "Revert installation via snapshot"},
		}},
		{"Inspect", []usageCmd{
			{"status", "Show tool status vs schema"},
			{"check", "Check whether a tool is actually installed"},
			{"validate", "Validate schema.toml and environment"},
			{"why", "Explain why a method was selected"},
			{"graph", "Show dependency graph"},
			{"diff", "Compare state between machines"},
		}},
		{"Export & integrate", []usageCmd{
			{"update", "Resolve {latest} and pin versions in depengine.lock"},
			{"sbom", "Export SBOM (CycloneDX or SPDX)"},
			{"completion", "Generate autocomplete script"},
		}},
	}, [][2]string{
		{"--version, -v", "Show version"},
		{"--help, -h", "Show help"},
	})
	fmt.Fprintf(c.w, "\n%s\n", c.dim("Use \"depengine <command> --help\" for command-specific flags."))
}
