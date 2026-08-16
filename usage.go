package main

import (
	"fmt"

	"github.com/Khorea1/depengine/pkg/i18n"
)

func printUsage() {
	locale := i18n.GetLocale()
	switch locale {
	case "en":
		printUsageEN()
	default:
		printUsagePT()
	}
}

func printUsagePT() {
	fmt.Print(`depengine - Motor distro-agnóstico de instalação de dependências

Uso:
  depengine <comando> [flags]

Comandos:
  init      Inicializar schema.toml para um projeto
  install   Instalar ferramentas do schema.toml
  upgrade  Atualizar ferramentas instaladas para as versões travadas no depengine.lock
  validate  Validar schema.toml e ambiente
  check     Verificar se uma ferramenta está de fato instalada (roda o check do adapter)
  status    Mostrar estado das ferramentas em relação ao schema
  remove    Remover ferramentas do sistema
  forget    Esquecer ferramentas do estado (sem desinstalar)
  undo      Reverter instalação via snapshot
  why       Explicar por que um método foi escolhido
  graph     Mostrar grafo de dependências
  sbom      Exportar SBOM (CycloneDX ou SPDX)
  diff      Comparar estado entre máquinas
  completion Gerar script de autocomplete

Flags globais:
  --version, -v   Mostrar versão
  --help, -h      Mostrar ajuda

Use "depengine <comando> --help" para flags específicas.
`)
}

func printUsageEN() {
	fmt.Print(`depengine — tool manager

Usage:
  depengine <command> [flags]

Commands:
  init      Initialize a schema.toml for a new project
  install   Install tools from schema.toml
  upgrade  Upgrade installed tools to pinned versions in depengine.lock
  validate  Validate schema.toml and environment
  check     Check whether a tool is actually installed (runs the adapter check)
  status    Show tool status vs schema
  remove    Remove tools from the system
  forget    Forget tools from state (without uninstalling)
  undo      Revert installation via snapshot
  why       Explain why a method was selected
  graph     Show dependency graph
  sbom      Export SBOM (CycloneDX or SPDX)
  diff      Compare state between machines
  completion Generate autocomplete script
Global flags:
  --version, -v   Show version
  --help, -h      Show help

Use "depengine <command> --help" for command-specific flags.
`)
}
