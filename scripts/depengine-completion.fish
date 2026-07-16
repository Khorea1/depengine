
# fish completion for depengine

function _depengine_completions
    set --local cmds install check status remove version validate help completion undo sbom graph diff update why forget

    # Commands and their flags
    set --local install_flags "--schema --dry-run --verbose --json --only --skip --sort-by --log-level --diagnose --jobs --profile --allow-arbitrary-code --frozen-lockfile"
    set --local validate_flags "--schema --check-env --format --strict"
    set --local status_flags "--schema --json --orphans --format"
    set --local remove_flags "--schema --all --dry-run"
    set --local update_flags "--schema --profile --frozen-lockfile --dry-run --lock"
    set --local why_flags "--json --format"
    set --local graph_flags "--schema --format --only --skip --profile"
    set --local undo_flags "--list --snapshot"
    set --local sbom_flags "--format"
    set --local diff_flags "--json --other"

    set --local cmd (commandline -opc)
    set --local cmd_count (count $cmd)

    if test $cmd_count -eq 1
        # Suggest commands
        for c in $cmds
            echo $c
        end
        return
    end

    set --local subcmd $cmd[2]
    set --local prev $cmd[(math "$cmd_count - 1")]

    # Dynamic flag values
    switch "$prev"
        case --log-level
            echo -e "error\nwarn\ninfo\ndebug"
            return
        case --sort-by
            echo -e "name\nstatus\nmethod"
            return
        case --format
            switch "$subcmd"
                case graph
                    echo -e "mermaid\ndot\ntext"
                case sbom
                    echo -e "cyclonedx\nspdx"
                case '*'
                    echo -e "text\njson"
            end
            return
        case --jobs
            echo -e "1\n2\n4\n8"
            return
        case completion
            echo -e "bash\nzsh\nfish"
            return
    end

    # Tool name completion for --only/--skip
    switch "$prev"
        case --only --skip
            set --local tools (depengine validate --format=json 2>/dev/null | string match -r '"tool":\s*"([^"]+)"')
            for t in $tools
                echo $t
            end
            return
    end

    # Suggest flags for current command
    switch "$subcmd"
        case install
            echo $install_flags | string split " "
        case validate
            echo $validate_flags | string split " "
        case status
            echo $status_flags | string split " "
        case remove
            echo $remove_flags | string split " "
        case update
            echo $update_flags | string split " "
        case why
            echo $why_flags | string split " "
        case graph
            echo $graph_flags | string split " "
        case undo
            echo $undo_flags | string split " "
        case sbom
            echo $sbom_flags | string split " "
        case diff
            echo $diff_flags | string split " "
        case completion
            echo -e "bash\nzsh\nfish"
    end
end

complete -c depengine -f -a "(_depengine_completions)"
