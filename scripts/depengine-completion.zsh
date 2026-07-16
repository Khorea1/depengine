
# zsh completion for depengine

_depengine() {
    local ret=1
    local cur="$2"
    local prev="$3"

    local -A cmd_flags
    cmd_flags=(
        install  "--schema --dry-run --verbose --json --only --skip --sort-by --log-level --diagnose --jobs --profile --allow-arbitrary-code --frozen-lockfile"
        validate "--schema --check-env --format --strict"
        status   "--schema --json --orphans --format"
        remove   "--schema --all --dry-run"
        update   "--schema --profile --frozen-lockfile --dry-run --lock"
        why      "--json --format"
        graph    "--schema --format --only --skip --profile"
        undo     "--list --snapshot"
        sbom     "--format"
        diff     "--json --other"
        completion ""
        check    ""
        forget   ""
        version  ""
        help     ""
    )

    local -a commands
    commands=(
        install
        check
        status
        remove
        version
        validate
        help
        completion
        undo
        sbom
        graph
        diff
        update
        why
        forget
    )

    # First argument: suggest commands
    if [[ $CURRENT -eq 2 ]]; then
        _describe 'commands' commands
        return
    fi

    local cmd="$words[2]"

    # Dynamic flag values
    case "$prev" in
        --log-level)
            local -a levels=( error warn info debug )
            _describe 'log-level' levels
            return
            ;;
        --sort-by)
            local -a fields=( name status method )
            _describe 'sort-by' fields
            return
            ;;
        --format)
            case "$cmd" in
                graph)
                    local -a formats=( mermaid dot text )
                    _describe 'format' formats
                    ;;
                sbom)
                    local -a formats=( cyclonedx spdx )
                    _describe 'format' formats
                    ;;
                *)
                    local -a formats=( text json )
                    _describe 'format' formats
                    ;;
            esac
            return
            ;;
        --jobs)
            local -a counts=( 1 2 4 8 )
            _describe 'jobs' counts
            return
            ;;
        --schema|--lock|--snapshot|--other)
            _files
            return
            ;;
        --only|--skip)
            local -a tools
            tools=( $(depengine validate --format=json 2>/dev/null | grep -oP '"tool":\s*"\K[^"]+') )
            _describe 'tool' tools
            return
            ;;
    esac

    # Suggest flags for the current command
    local flags="${cmd_flags[$cmd]}"
    if [[ -n "$flags" ]]; then
        local -a flag_list=( ${=flags} )
        _describe 'flags' flag_list
    fi

    # For completion command, suggest shells
    if [[ "$cmd" == "completion" ]]; then
        local -a shells=( bash zsh fish )
        _describe 'shell' shells
        return
    fi

    return ret
}

compdef _depengine depengine
