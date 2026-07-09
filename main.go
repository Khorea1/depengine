package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"depengine/pkg/engine"
	"depengine/pkg/exec"
	"depengine/pkg/git"
	"depengine/pkg/httpdownload"
	"depengine/pkg/lang"
	"depengine/pkg/log"
	"depengine/pkg/native"
	"depengine/pkg/run"
	"depengine/pkg/schema"
)

func main() {
	// Register all adapters before any command runs.
	initAdapters()

	installCmd := flag.NewFlagSet("install", flag.ExitOnError)
	installSchema := installCmd.String("schema", "schema.toml", "path to schema.toml")
	installDryRun := installCmd.Bool("dry-run", false, "show what would be installed")
	installVerbose := installCmd.Bool("verbose", false, "detailed output")
	installJSON := installCmd.Bool("json", false, "JSON output")
	installOnly := installCmd.String("only", "", "only install specific tool")
	installSkip := installCmd.String("skip", "", "skip specific tools (comma-separated)")
	installDiagnose := installCmd.Bool("diagnose", false, "diagnostic mode: DEBUG + dry-run + verbose")
	installLogLevel := installCmd.String("log-level", "", "log level: debug, info, warn, error")
	installSortBy := installCmd.String("sort-by", "", "sort output by: name, status, method")

	checkCmd := flag.NewFlagSet("check", flag.ExitOnError)
	checkSchema := checkCmd.String("schema", "schema.toml", "path to schema.toml")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(0)
	}

	// Create root logger. trace_id propagates from env automatically via
	// pkg/log init(); explicit --log-level overrides the default.
	lg := log.Default

	switch os.Args[1] {
	case "install":
		installCmd.Parse(os.Args[2:])

		// --diagnose implies --dry-run + verbose + DEBUG level.
		if *installDiagnose {
			*installDryRun = true
			*installVerbose = true
			lg = log.New(os.Stderr, slog.LevelDebug)
		}
		if *installLogLevel != "" {
			lg = log.New(os.Stderr, log.LevelFromString(*installLogLevel))
		}

		ctx := context.Background()

		if *installSortBy != "" {
			if _, ok := exec.ParseSortField(*installSortBy); !ok {
				lg.Error("invalid --sort-by value", "value", *installSortBy, "valid", "name, status, method")
				os.Exit(2)
			}
		}

		s, clan, facts, err := loadSchema(*installSchema)
		if err != nil {
			lg.Error("load schema", "error", err)
			os.Exit(exitCodeForError(err))
		}

		fmt.Fprintf(os.Stderr, "depengine install: distro=%s clan=%s arch=%s tools=%d\n",
			facts.DistroID, clan, facts.TargetArch, len(s.Tools))

		ex := exec.New()
		exec.WithAdapters(
			git.NewGitAdapter(),
			httpdownload.NewHTTPAdapter(),
			exec.NewNativeAdapter(clan),
		)(ex)
		exec.WithLogger(lg)(ex)
		exec.WithRunner(run.NewLoggingRunner(run.OSExecRunner{}, lg))(ex)
		if *installDryRun {
			exec.WithDryRun()(ex)
		}
		if *installSortBy != "" {
			exec.WithSortBy(exec.SortField(*installSortBy))(ex)
		}

		s.Tools = filterTools(s.Tools, *installOnly, *installSkip)

		if *installDiagnose {
			lg.Debug("facts", "facts", facts)
			lg.Debug("schema", "tools", len(s.Tools))
		}

		report, err := ex.Execute(ctx, s, clan)
		if err != nil {
			lg.Error("execute failed", "error", err)
			os.Exit(2)
		}

		if *installJSON {
			fmt.Println(report.JSON())
		} else if *installVerbose || *installDryRun {
			fmt.Fprint(os.Stderr, report.Detail())
		} else {
			fmt.Fprintln(os.Stderr, report.Summary())
		}

		if report.Failed > 0 {
			os.Exit(1)
		}

	case "check":
		checkCmd.Parse(os.Args[2:])
		args := checkCmd.Args()
		if len(args) < 1 {
			lg.Error("usage: depengine check <tool>")
			os.Exit(1)
		}
		toolName := args[0]

		s, _, _, err := loadSchema(*checkSchema)
		if err != nil {
			lg.Error("load schema", "error", err)
			os.Exit(exitCodeForError(err))
		}

		tool, ok := s.Tools[toolName]
		if !ok {
			lg.Error("tool not found", "tool", toolName)
			os.Exit(1)
		}

		for _, method := range tool.Methods {
			adapter := exec.Lookup(method.Kind)
			if adapter == nil {
				continue
			}
			if adapter.Check(context.Background(), run.OSExecRunner{}, tool, method) {
				fmt.Printf("✓ %s is installed (via %s)\n", toolName, method.Kind)
				os.Exit(0)
			}
		}
		fmt.Printf("✗ %s is not installed\n", toolName)
		os.Exit(1)

	case "version":
		fmt.Println("depengine v0.1.0")
		fmt.Println("Motor distro-agnostic de instalação de dependências")

	case "help", "-h", "--help":
		printUsage()

	default:
		showNativeCommands(os.Args[1])
	}
}

// loadSchema is the shared bootstrap for install/check: gather facts,
// resolve clan, build placeholder map, parse schema. Returns an error
// suitable for exitCodeForError.
func loadSchema(path string) (*schema.Schema, string, *engine.Facts, error) {
	facts, err := engine.GatherFacts(run.OSExecRunner{})
	if err != nil {
		return nil, "", nil, err
	}
	clan := engine.ResolveFamily(facts)
	s, err := schema.ParseSchema(path, schema.BuildMap(facts, clan))
	if err != nil {
		return nil, "", nil, err
	}
	if verr, warnings := schema.Validate(s, exec.RegisteredKinds()); verr != nil {
		return nil, "", nil, verr
	} else if len(warnings) > 0 {
		for _, w := range warnings {
			log.Default.Warn(w)
		}
	}
	return s, clan, facts, nil
}

// exitCodeForError maps common bootstrap errors to exit codes.
func exitCodeForError(err error) int {
	var schemaErr *schema.ParseSchemaError
	if errors.As(err, &schemaErr) {
		return 2 // schema error (malformed TOML, validation, etc.)
	}
	return 3 // runtime error (detect_os.sh not found, etc.)
}

// filterTools applies --only and --skip filters to the tool map.
// Both filters are processed; the result is the intersection of both.
func filterTools(tools map[string]*schema.Tool, only, skip string) map[string]*schema.Tool {
	if only == "" && skip == "" {
		return tools
	}
	skipSet := make(map[string]bool)
	for _, name := range strings.Split(skip, ",") {
		skipSet[strings.TrimSpace(name)] = true
	}
	filtered := make(map[string]*schema.Tool, len(tools))
	for name, tool := range tools {
		if skipSet[name] {
			continue
		}
		if only != "" && name != only {
			continue
		}
		filtered[name] = tool
	}
	return filtered
}

func printUsage() {
	fmt.Println(`depengine - Motor distro-agnostic de instalação de dependências

Uso:
  depengine install [flags]        Instala ferramentas do schema.toml
  depengine check <tool>           Verifica se uma ferramenta está instalada
  depengine version                Mostra a versão
  depengine help                   Mostra esta ajuda

Flags (install):
  --schema <path>   Caminho para schema.toml (default: schema.toml)
  --dry-run         Mostra o que seria instalado sem executar
  --verbose, -v     Log detalhado
  --json            Saída em JSON
  --only <tool>     Instala apenas uma ferramenta
  --skip <tools>    Pula ferramentas (separadas por vírgula)
   --diagnose        Modo diagnóstico: DEBUG + dry-run + verbose
   --log-level <lvl> Nível de log: debug, info, warn, error
   --sort-by <campo> Ordena output: name, status, method

Exit codes:
  0   Sucesso (todas as ferramentas ok)
  1   Alguma ferramenta falhou
  2   Erro de schema
  3   Erro de runtime (detect_os.sh não encontrado, etc.)`)
}

func showNativeCommands(pkgName string) {
	facts, err := engine.GatherFacts(run.OSExecRunner{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "erro: %v\n", err)
		os.Exit(3)
	}
	clan := engine.ResolveFamily(facts)
	fmt.Printf("Facts: distro=%s, clan=%s\n", facts.DistroID, clan)

	mgr, ok := native.Lookup(clan)
	if !ok {
		fmt.Printf("Nenhum native manager conhecido para %q.\n", clan)
		return
	}
	fmt.Printf("Native manager: %s\n", mgr.Name)
	fmt.Printf("  check:   %s\n", strings.Join(native.BuildCheckCmd(clan, pkgName), " "))
	fmt.Printf("  install: %s\n", strings.Join(native.BuildInstallCmd(clan, pkgName), " "))
}

func initAdapters() {
	// Native adapter (canonical "native" kind, clan empty — resolved at
	// runtime by detectClan). Per-instance WithAdapters in the install
	// command shadows this with the authoritative clan from ResolveFamily.
	exec.Register(exec.NewNativeAdapter(""))
	// Also register manager-name aliases (apt, pacman, dnf, …) so that
	// schema entries like `apt = "fd-find"` resolve to the native adapter.
	exec.RegisterNativeManagerAliases()

	// Language adapters.
	lang.RegisterAll("paru")
	// Git adapter.
	exec.Register(git.NewGitAdapter())
	// HTTP adapter.
	exec.Register(httpdownload.NewHTTPAdapter())
}
