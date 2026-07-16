
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

    local cmds="install check status remove version validate help completion undo sbom graph diff update why forget"

    # Map each command to its flags
    local install_flags="--schema --dry-run --verbose --json --only --skip --sort-by --log-level --diagnose --jobs --profile --allow-arbitrary-code --frozen-lockfile"
    local validate_flags="--schema --check-env --format --strict"
    local status_flags="--schema --json --orphans --format"
    local remove_flags="--schema --all --dry-run"
    local update_flags="--schema --profile --frozen-lockfile --dry-run --lock"
    local why_flags="--json --format"
    local graph_flags="--schema --format --only --skip --profile"
    local undo_flags="--list --snapshot"
    local sbom_flags="--format"
    local diff_flags="--json --other"
    local check_flags=""
    local forget_flags=""
    local completion_flags=""

    # Dynamic flag values (--flag:values...)
    local log_levels="error warn info debug"
    local sort_by="name status method"
    local formats="text json"
    local graph_formats="mermaid dot text"
    local sbom_formats="cyclonedx spdx"
    local completion_shells="bash zsh fish"
    local profile_values=""

    if [[ $cword -eq 1 ]]; then
        COMPREPLY=($(compgen -W "$cmds" -- "$cur"))
        return
    fi

    local cmd="${words[1]}"
    local prev_arg="${words[cword-1]}"

    # Dynamic value completion for flags that take arguments
    case "$prev_arg" in
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
                graph)    COMPREPLY=($(compgen -W "$graph_formats" -- "$cur")) ;;
                sbom)     COMPREPLY=($(compgen -W "$sbom_formats" -- "$cur")) ;;
                validate|diff|why|status)
                          COMPREPLY=($(compgen -W "$formats" -- "$cur")) ;;
                *)        COMPREPLY=($(compgen -W "$formats $graph_formats $sbom_formats" -- "$cur")) ;;
            esac
            return
            ;;
        --schema|--lock|--snapshot|--other)
            # File path completion
            COMPREPLY=($(compgen -f -- "$cur"))
            return
            ;;
        --only|--skip)
            # Tool name completion from schema.toml (basic: suggest any word)
            COMPREPLY=($(compgen -W "$(depengine validate --format=json 2>/dev/null | grep -oP '"tool":\s*"\K[^"]+' 2>/dev/null || echo '')" -- "$cur"))
            return
            ;;
        --profile)
            COMPREPLY=($(compgen -W "$profile_values" -- "$cur"))
            return
            ;;
        --jobs)
            COMPREPLY=($(compgen -W "1 2 4 8" -- "$cur"))
            return
            ;;
    esac

    # Get flags for current command
    local cmd_flags=""
    case "$cmd" in
        install)   cmd_flags="$install_flags" ;;
        validate)  cmd_flags="$validate_flags" ;;
        status)    cmd_flags="$status_flags" ;;
        remove)    cmd_flags="$remove_flags" ;;
        update)    cmd_flags="$update_flags" ;;
        why)       cmd_flags="$why_flags" ;;
        graph)     cmd_flags="$graph_flags" ;;
        undo)      cmd_flags="$undo_flags" ;;
        sbom)      cmd_flags="$sbom_flags" ;;
        diff)      cmd_flags="$diff_flags" ;;
        check)     cmd_flags="$check_flags" ;;
        forget)    cmd_flags="$forget_flags" ;;
        completion) cmd_flags="$completion_flags" ;;
        help)      cmd_flags="" ;;
        version)   cmd_flags="" ;;
    esac

    # If we're completing a flag value, we handled it above
    if [[ "$cur" == -* ]]; then
        COMPREPLY=($(compgen -W "$cmd_flags" -- "$cur"))
        return
    fi

    # For completion command, suggest shells after it
    if [[ "$cmd" == "completion" ]]; then
        COMPREPLY=($(compgen -W "$completion_shells" -- "$cur"))
        return
    fi
}

complete -F _depengine depengine
