# bash completion for depengine

_depengine_has() {
    local word="$1"
    shift
    for w in "$@"; do
        [[ "$w" == "$word" ]] && return 0
    done
    return 1
}

_depengine() {
    local cur prev words cword
    _init_completion -n = || return

    local cmds="install init check status remove version validate help completion undo sbom graph diff update why forget"

    # Dynamic flag values (--flag:values...)
    local log_levels="error warn info debug"
    local sort_by="name status method"
    local formats="text json"
    local graph_formats="mermaid dot text"
    local sbom_formats="cyclonedx spdx"
    local completion_shells="bash zsh fish"

    if [[ $cword -eq 1 ]]; then
        COMPREPLY=($(compgen -W "$cmds" -- "$cur"))
        return
    fi

    local cmd="${words[1]}"
    local prev_arg="${words[cword-1]}"

    # Dynamic value completion for flags that take arguments
    case "$prev_arg" in
        --schema|--snapshot|--lock|--other)
            # File path completion
            COMPREPLY=($(compgen -f -- "$cur"))
            return
            ;;
        --log-level)
            COMPREPLY=($(compgen -W "$log_levels" -- "$cur"))
            return
            ;;
        --sort-by)
            COMPREPLY=($(compgen -W "$sort_by" -- "$cur"))
            return
            ;;
        --format)
            case "$cmd" in
                graph) COMPREPLY=($(compgen -W "$graph_formats" -- "$cur")) ;;
                sbom)  COMPREPLY=($(compgen -W "$sbom_formats" -- "$cur")) ;;
                *)     COMPREPLY=($(compgen -W "$formats" -- "$cur")) ;;
            esac
            return
            ;;
        --jobs)
            COMPREPLY=($(compgen -W "1 2 4 8" -- "$cur"))
            return
            ;;
        completion)
            COMPREPLY=($(compgen -W "$completion_shells" -- "$cur"))
            return
            ;;
    esac

    # Get flags for current command (with descriptions)
    local -a flag_descs
    case "$cmd" in
        install)
            flag_descs=(
                "--schema	Caminho para o schema"
                "--dry-run	Simulação sem alterações"
                "--verbose	Saída detalhada por tool"
                "--json	Saída em JSON"
                "--only	Instalar apenas uma tool específica"
                "--skip	Pular tools (separadas por vírgula)"
                "--sort-by	Ordenar output: name, status, method"
                "--log-level	Nível de log: debug, info, warn, error"
                "--diagnose	Modo diagnóstico (DEBUG + dry-run + verbose)"
                "--jobs	Número de instalações simultâneas"
                "--profile	Filtrar tools por tag (ex: desktop, server)"
                "--allow-arbitrary-code	Permitir scripts de build arbitrários"
                "--frozen-lockfile	Abortar se depengine.lock não existir"
            )
            ;;
        validate)
            flag_descs=(
                "--schema	Caminho para o schema"
                "--check-env	Verificar se tools necessárias estão no PATH"
                "--format	Formato de saída: text, json"
                "--strict	Tratar warnings como erros (exit code 1)"
            )
            ;;
        status)
            flag_descs=(
                "--schema	Caminho para o schema"
                "--json	Saída em JSON"
                "--orphans	Mostrar apenas tools não-schema instaladas"
                "--format	Formato de saída: text, json"
            )
            ;;
        remove)
            flag_descs=(
                "--schema	Caminho para o schema"
                "--all	Remover todas as tools"
                "--dry-run	Simulação sem remover"
            )
            ;;
        update)
            flag_descs=(
                "--schema	Caminho para o schema"
                "--profile	Filtrar tools por tag"
                "--frozen-lockfile	Abortar se depengine.lock não existir"
                "--dry-run	Simulação sem escrever lockfile"
                "--lock	Caminho para o lockfile"
            )
            ;;
        graph)
            flag_descs=(
                "--schema	Caminho para o schema"
                "--format	Formato: text, mermaid, dot"
                "--only	Subgrafo de uma tool específica"
                "--skip	Pular tools do grafo"
                "--profile	Filtrar tools por tag"
            )
            ;;
        why)
            flag_descs=(
                "--json	Saída em JSON"
                "--fields	Mostrar proveniência dos campos"
            )
            ;;
        undo)
            flag_descs=(
                "--list	Listar snapshots disponíveis"
                "--snapshot	Reverter para um snapshot específico"
            )
            ;;
        sbom)
            flag_descs=(
                "--format	Formato: cyclonedx, spdx"
            )
            ;;
        diff)
            flag_descs=(
                "--json	Saída em JSON"
                "--other	Segundo arquivo de estado para comparar"
            )
            ;;
    esac

    # Complete flags or values for current command
    if [[ "$cur" == -* ]]; then
        # Build flag list with descriptions (tab-separated for compatible terminals)
        local flags_txt=""
        local desc_txt=""
        local flag
        local desc
        local IFS=$'\t'
        for entry in "${flag_descs[@]}"; do
            flag="${entry%%$'\t'*}"
            desc="${entry#*$'\t'}"
            [[ -z "$flags_txt" ]] || flags_txt+=" "
            # For COMPREPLY, emit flag + description; bash-completion shows
            # descriptions in terminals that support them
            flags_txt+="$flag"
        done
        COMPREPLY=($(compgen -W "$flags_txt" -- "$cur"))
        return
    fi

    # For completion command, suggest shells after it
    if [[ "$cmd" == "completion" ]]; then
        COMPREPLY=($(compgen -W "$completion_shells" -- "$cur"))
        return
    fi
}

complete -F _depengine depengine
