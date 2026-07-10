
# bash completion for depengine

_depengine() {
    local cur prev opts cmds flags dynamic_flags
    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev="${COMP_WORDS[COMP_CWORD-1]}"

    cmds="install check status remove version validate help completion"
    flags="--dry-run --verbose --json --diagnose --log-level --sort-by --only --skip --check-env --format --strict --schema"
    dynamic_flags=(
        "--log-level:error:warn:info:debug"
        "--sort-by:name:status:method"
        "--format:text:json"
        "--completion:bash:zsh:fish"
    )

    if [[ ${cur} == --* ]] || [[ ${prev} == --* ]]; then
        # If current word is a flag or previous word was a flag, suggest flags
        for opt in $flags; do
            if [[ $opt == *${cur}* ]]; then
                COMPREPLY+=($opt)
            fi
        done
    else
        # Suggest commands or dynamic flags
        for cmd in $cmds;
        do
            if [[ $cmd == *${cur}* ]]; then
                COMPREPLY+=($cmd)
            fi
        done

        # Check for dynamic flags and their values
        for dynamic in "${dynamic_flags[@]}"; do
            local flag_name="${dynamic%%:*}"
            local flag_values="${dynamic#*:}"
            if [[ $prev == $flag_name* ]]; then
                for value in ${flag_values//:/ }; do
                    if [[ $value == *${cur}* ]]; then
                        COMPREPLY+=($value)
                    fi
                done
            fi
        done
    fi

    return 0
}

complete -F _depengine depengine
