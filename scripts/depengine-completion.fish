# fish completion for depengine

function _depengine_completions
    set --local cmds install check status remove version validate help completion undo sbom graph diff update why forget

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
            echo -e "debug\tDepuração\ninfo\tInformação\nwarn\tAvisos\nerror\tErros"
            return
        case --sort-by
            echo -e "name\tNome\nstatus\tEstado\nmethod\tMétodo"
            return
        case --format
            switch "$subcmd"
                case graph
                    echo -e "mermaid\tMermaid\ndot\tDOT\ntext\tTexto"
                case sbom
                    echo -e "cyclonedx\tCycloneDX\nspdx\tSPDX"
                case '*'
                    echo -e "text\tTexto\njson\tJSON"
            end
            return
        case --jobs
            echo -e "1\n2\n4\n8"
            return
        case completion
            echo -e "bash\tBash\nzsh\tZsh\nfish\tFish"
            return
        case --schema --snapshot --lock --other
            # File path completion — fish handles _files native suggestions
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
            echo -e "--schema\tCaminho para o schema"
            echo -e "--dry-run\tSimulação sem alterações"
            echo -e "--verbose\tSaída detalhada por tool"
            echo -e "--json\tSaída em JSON"
            echo -e "--only\tInstalar apenas uma tool específica"
            echo -e "--skip\tPular tools (separadas por vírgula)"
            echo -e "--sort-by\tOrdenar output: name, status, method"
            echo -e "--log-level\tNível de log: debug, info, warn, error"
            echo -e "--diagnose\tModo diagnóstico (DEBUG + dry-run + verbose)"
            echo -e "--jobs\tNúmero de instalações simultâneas"
            echo -e "--profile\tFiltrar tools por tag (ex: desktop, server)"
            echo -e "--allow-arbitrary-code\tPermitir scripts de build arbitrários"
            echo -e "--frozen-lockfile\tAbortar se schema.lock não existir"
        case validate
            echo -e "--schema\tCaminho para o schema"
            echo -e "--check-env\tVerificar se tools necessárias estão no PATH"
            echo -e "--format\tFormato de saída: text, json"
            echo -e "--strict\tTratar warnings como erros (exit code 1)"
        case status
            echo -e "--schema\tCaminho para o schema"
            echo -e "--json\tSaída em JSON"
            echo -e "--orphans\tMostrar apenas tools não-schema instaladas"
            echo -e "--format\tFormato de saída: text, json"
        case remove
            echo -e "--schema\tCaminho para o schema"
            echo -e "--all\tRemover todas as tools"
            echo -e "--dry-run\tSimulação sem remover"
        case update
            echo -e "--schema\tCaminho para o schema"
            echo -e "--profile\tFiltrar tools por tag"
            echo -e "--frozen-lockfile\tAbortar se schema.lock não existir"
            echo -e "--dry-run\tSimulação sem escrever lockfile"
            echo -e "--lock\tCaminho para o lockfile"
        case graph
            echo -e "--schema\tCaminho para o schema"
            echo -e "--format\tFormato: text, mermaid, dot"
            echo -e "--only\tSubgrafo de uma tool específica"
            echo -e "--skip\tPular tools do grafo"
            echo -e "--profile\tFiltrar tools por tag"
        case why
            echo -e "--json\tSaída em JSON"
            echo -e "--format\tFormato de saída: text, json"
        case undo
            echo -e "--list\tListar snapshots disponíveis"
            echo -e "--snapshot\tReverter para um snapshot específico"
        case sbom
            echo -e "--format\tFormato: cyclonedx, spdx"
        case diff
            echo -e "--json\tSaída em JSON"
            echo -e "--other\tSegundo arquivo de estado para comparar"
        case completion
            echo -e "bash\tBash\nzsh\tZsh\nfish\tFish"
    end
end

complete -c depengine -f -a "(_depengine_completions)"
