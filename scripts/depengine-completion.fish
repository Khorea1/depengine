
# fish completion for depengine

function _depengine_completions
    set --local cur "$argv[1]"
    set --local prev "$argv[2]"
    set --local cmds "install check status remove version validate help completion undo sbom graph diff update why forget"
    set --local flags "--dry-run --verbose --json --diagnose --log-level --sort-by --only --skip --check-env --format --strict --schema"
    set --local dynamic_flags=(
        "--log-level:error:warn:info:debug"
        "--sort-by:name:status:method"
        "--format:text:json"
        "--completion:bash:zsh:fish"
    )

    if string match "--*" $cur
        echo $flags | string split " " | string match -r "^($flags)"
    else if string match "--*" $prev
        for dynamic in $dynamic_flags
            set --local flag_name (echo $dynamic | string split ":" -f 1)
            set --local flag_values (echo $dynamic | string split ":" -f 2- | string replace ":" " ")
            if test "$prev" = "$flag_name"
                echo $flag_values | string split " " | string match -r "^($flag_values)"
                return
            end
        end
        echo $cmds | string split " " | string match -r "^($cmds)"
    else
        echo $cmds | string split " " | string match -r "^($cmds)"
    end
end

complete -c depengine -f -a "(_depengine_completions)"
