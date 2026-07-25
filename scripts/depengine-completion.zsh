#compdef depengine
# zsh completion for depengine

_depengine_commands() {
  local -a commands
  commands=(
    'install:Instalar ferramentas do schema.toml'
    'update:Resolver {latest} e travar versões no depengine.lock'
    'validate:Validar schema.toml e ambiente'
    'check:Verificar se ferramentas já estão instaladas'
    'status:Mostrar estado das ferramentas em relação ao schema'
    'remove:Remover ferramentas do sistema'
    'forget:Esquecer ferramentas do estado (sem desinstalar)'
    'undo:Reverter instalação via snapshot'
    'why:Explicar por que um método foi escolhido'
    'graph:Mostrar grafo de dependências'
    'sbom:Exportar SBOM (CycloneDX ou SPDX)'
    'diff:Comparar estado entre máquinas'
    'completion:Gerar script de autocomplete'
    'version:Mostrar versão'
    'help:Mostrar ajuda'
  )
  _describe -t commands 'comando' commands
}

_depengine() {
  local ret=1
  local cur="$2"
  local prev="$3"

  # First argument: suggest commands
  if [[ $CURRENT -eq 2 ]]; then
    _depengine_commands
    return $?
  fi

  local cmd="$words[2]"

  # Dynamic flag values
  case "$prev" in
    --schema)
      _files && ret=0
      ;;
    --log-level)
      _values -V single 'nível' 'debug[Depuração]' 'info[Informação]' 'warn[Avisos]' 'error[Erros]' && ret=0
      ;;
    --sort-by)
      _values -V single 'ordenar por' 'name[Nome]' 'status[Estado]' 'method[Método]' && ret=0
      ;;
    --format)
      case "$cmd" in
        graph)
          _values -V single 'formato' 'mermaid[Mermaid]' 'dot[DOT]' 'text[Texto]' && ret=0
          ;;
        sbom)
          _values -V single 'formato' 'cyclonedx[CycloneDX]' 'spdx[SPDX]' && ret=0
          ;;
        *)
          _values -V single 'formato' 'text[Texto]' 'json[JSON]' && ret=0
          ;;
      esac
      ;;
    --jobs)
      _values -V single 'jobs' '1' '2' '4' '8' && ret=0
      ;;
    --only|--skip)
      # Tools; dynamic completion would require parsing schema
      _message -r 'tool' && ret=0
      ;;
    --snapshot|--lock|--other)
      _files && ret=0
      ;;
    completion)
      _values -V single 'shell' 'bash[Bash]' 'zsh[Zsh]' 'fish[Fish]' && ret=0
      ;;
    --profile)
      # Tags are user-defined; generic text input
      _message -r 'tag' && ret=0
      ;;
    --only)
      _message -r 'tool' && ret=0
      ;;
  esac

  # Suggest flags for the current command
  if [[ "$cur" == -* ]]; then
    local -a flags
    case "$cmd" in
      install)
        flags=(
          '--schema[Caminho para o schema]'
          '--dry-run[Simulação sem alterações]'
          '--verbose[Saída detalhada por tool]'
          '--json[Saída em JSON]'
          '--only[Instalar apenas uma tool específica]'
          '--skip[Pular tools (separadas por vírgula)]'
          '--sort-by[Ordenar output: name, status, method]'
          '--log-level[Nível de log: debug, info, warn, error]'
          '--diagnose[Modo diagnóstico (DEBUG + dry-run + verbose)]'
          '--jobs[Número de instalações simultâneas]'
          '--profile[Filtrar tools por tag (ex: desktop, server)]'
          '--allow-arbitrary-code[Permitir scripts de build arbitrários]'
          '--frozen-lockfile[Abortar se depengine.lock não existir]'
        )
        ;;
      validate)
        flags=(
          '--schema[Caminho para o schema]'
          '--check-env[Verificar se tools necessárias estão no PATH]'
          '--format[Formato de saída: text, json]'
          '--strict[Tratar warnings como erros (exit code 1)]'
        )
        ;;
      status)
        flags=(
          '--schema[Caminho para o schema]'
          '--json[Saída em JSON]'
          '--orphans[Mostrar apenas tools não-schema instaladas]'
          '--format[Formato de saída: text, json]'
        )
        ;;
      remove)
        flags=(
          '--schema[Caminho para o schema]'
          '--all[Remover todas as tools]'
          '--dry-run[Simulação sem remover]'
        )
        ;;
      update)
        flags=(
          '--schema[Caminho para o schema]'
          '--profile[Filtrar tools por tag]'
          '--frozen-lockfile[Abortar se depengine.lock não existir]'
          '--dry-run[Simulação sem escrever lockfile]'
          '--lock[Caminho para o lockfile]'
        )
        ;;
      graph)
        flags=(
          '--schema[Caminho para o schema]'
          '--format[Formato: text, mermaid, dot]'
          '--only[Subgrafo de uma tool específica]'
          '--skip[Pular tools do grafo]'
          '--profile[Filtrar tools por tag]'
        )
        ;;
      why)
        flags=(
          '--json[Saída em JSON]'
          '--format[Formato de saída: text, json]'
        )
        ;;
      undo)
        flags=(
          '--list[Listar snapshots disponíveis]'
          '--snapshot[Reverter para um snapshot específico]'
        )
        ;;
      sbom)
        flags=(
          '--format[Formato: cyclonedx, spdx]'
        )
        ;;
      diff)
        flags=(
          '--json[Saída em JSON]'
          '--other[Segundo arquivo de estado para comparar]'
        )
        ;;
    esac
    _describe -t flags 'flag' flags && ret=0
  fi

  # For completion command, suggest shells
  if [[ "$cmd" == "completion" && "$cur" != -* ]]; then
    local -a shells
    shells=('bash[Bash]' 'zsh[Zsh]' 'fish[Fish]')
    _describe -t shells 'shell' shells && ret=0
  fi

  return ret
}

compdef _depengine depengine
