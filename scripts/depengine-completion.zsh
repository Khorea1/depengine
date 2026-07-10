
# zsh completion for depengine

_depengine() {
    local ret="$1"
    local cur="$2"
    local prev="$3"
    local cmdwords="@("
    local commands=(
        install
        check
        status
        remove
        version
        validate
        help
        completion
    )
    local flags=(
        "--dry-run"
        "--verbose"
        "--json"
        "--diagnose"
        "--log-level"
        "--sort-by"
        "--only"
        "--skip"
        "--check-env"
        "--format"
        "--strict"
        "--schema"
    )
    local dynamic_flags=(
        "--log-level:error:warn:info:debug"
        "--sort-by:name:status:method"
        "--format:text:json"
        "--completion:bash:zsh:fish"
    )

    # Commands
    _describe 'commands' commands

    # Flags
    _describe 'flags' flags

    # Dynamic flags and their values
    for flag in "${dynamic_flags[@]}"; do
        local flag_name="${flag%%:*}"
        local flag_values="${flag#*:}"
        if [[ "$cur" == "$flag_name"* ]] || [[ "$prev" == "$flag_name"* ]]; then
            _describe "values for $flag_name" "${flag_values//:/ }"
        fi
    done

    return 0
}

compdef _depengine depengine
