 // Premissas de Design:
//   - Usar map[string]interface{} como estrutura intermediária bruta após decode TOML.
//   - Mapas não preservam ordem; a ordem de tentativa dos métodos é definida por defaults.method_order.
//   - Normalização manual: converter dados brutos em estruturas internas canônicas.

// ------------------------------------------------------------------
// ESTRUTURAS NORMALIZADAS (Alvo)
// ------------------------------------------------------------------

Schema {
    Defaults : Defaults
    Tools    : map<string, Tool>   // chave = nome da tool
}

Defaults {
    Manager     : string    // "native" se omitido
    AurHelper   : string    // "paru" se omitido
    MethodOrder : []string  // ordem canônica de tentativa dos métodos
}

Tool {
    Name        : string
    Requires    : []string             // dependências entre tools
    PostInstall : string               // hook pós-instalação
    Methods     : []MethodCandidate    // lista ordenada por MethodOrder
    IsSimple    : bool                 // veio da lista `simple`
}

MethodCandidate {
    Kind   : string                 // "native", "cargo", "git", "http", etc.
    When   : Condition              // nulo se incondicional
    Config : map<string, interface> // campos específicos do método (pkg, url, build, etc.)
}

Condition {
    DistroFamily : []string         // ex: ["arch"], ["debian"]
}

// ------------------------------------------------------------------
// FLUXO PRINCIPAL
// ------------------------------------------------------------------

função ParseSchema(caminhoArquivo):
    raw = TOML_DecodeParaMap(caminhoArquivo)
    // raw é um map[string]interface{}

    defaults = ExtrairDefaults(obter raw["defaults"])
    tools    = NormalizarTools(obter raw["tools"], defaults)

    retornar Schema{Defaults: defaults, Tools: tools}
fim função

// ------------------------------------------------------------------
// EXTRAÇÃO DE DEFAULTS
// ------------------------------------------------------------------

função ExtrairDefaults(rawDefaults):
    d = novo Defaults()

    // Aplicar valores padrão antes de sobrescrever
    d.Manager     = "native"
    d.AurHelper   = "paru"
    d.MethodOrder = ["native", "cargo", "go", "pip", "pipx", "uv", "aur", "git", "http"]

    se rawDefaults existe:
        se rawDefaults possui chave "manager":
            d.Manager = rawDefaults["manager"] como string
        fim se

        se rawDefaults possui chave "aur_helper":
            d.AurHelper = rawDefaults["aur_helper"] como string
        fim se

        se rawDefaults possui chave "method_order":
            d.MethodOrder = rawDefaults["method_order"] como []string
        fim se
    fim se

    retornar d
fim função

// ------------------------------------------------------------------
// NORMALIZAÇÃO DE TOOLS
// ------------------------------------------------------------------

função NormalizarTools(rawTools, defaults):
    tools = map<string, Tool> vazio

    // 1. Processar lista `simple` primeiro.
    //    Ferramentas simples usam o manager padrão com o próprio nome como pacote.
    se rawTools possui chave "simple" e rawTools["simple"] é lista:
        para cada item em rawTools["simple"]:
            nome = item como string

            tool = novo Tool()
            tool.Name     = nome
            tool.IsSimple = verdadeiro

            mc = novo MethodCandidate()
            mc.Kind        = defaults.Manager
            mc.Config["pkg"] = nome

            tool.Methods = [mc]
            tools[nome]    = tool
        fim para
    fim se

    // 2. Processar demais entries de tools (tabelas detalhadas).
    para cada (nome, valor) em rawTools:
        se nome == "simple":
            continuar
        fim se

        tool      = novo Tool()
        tool.Name = nome

        se valor é map:
            valMap = valor como map<string, interface>

            // Campos de nível tool
            se valMap possui chave "requires":
                tool.Requires = ConverteParaStringSlice(valMap["requires"])
            fim se

            se valMap possui chave "postinstall":
                tool.PostInstall = valMap["postinstall"] como string
            fim se

            // Extrair métodos declarados: todo campo restante é um método candidato
            metodos = lista vazia de MethodCandidate
            para cada (chave, val) em valMap:
                se chave em ["requires", "postinstall"]:
                    continuar
                fim se

                metodo = ParseMetodo(chave, val)
                adicionar metodo em metodos
            fim para

            // Ordenar de acordo com o method_order definido em defaults
            tool.Methods = OrdenarPorReferencia(metodos, defaults.MethodOrder)
        fim se

        tools[nome] = tool
    fim para

    retornar tools
fim função

// ------------------------------------------------------------------
// PARSE DE MÉTODO INDIVIDUAL
// ------------------------------------------------------------------

função ParseMetodo(kind, val):
    mc            = novo MethodCandidate()
    mc.Kind       = kind
    mc.Config     = map<string, interface> vazio
    mc.When       = nulo

    se val é string:
        // Caso inline simples: apt = "fd-find"
        mc.Config["pkg"] = val

    senao se val é map:
        valMap = val como map<string, interface>

        // Condição `when` é extraída separadamente do config genérico
        se valMap possui chave "when":
            mc.When = ParseCondition(valMap["when"])
        fim se

        para cada (k, v) em valMap:
            se k == "when":
                continuar
            fim se
            mc.Config[k] = v
        fim para
    fim se

    retornar mc
fim função

// ------------------------------------------------------------------
// PARSE DE CONDIÇÃO WHEN
// ------------------------------------------------------------------

função ParseCondition(raw):
    se raw não é map:
        retornar nulo
    fim se

    valMap = raw como map<string, interface>
    cond   = novo Condition()

    se valMap possui chave "distro_family":
        cond.DistroFamily = ConverteParaStringSlice(valMap["distro_family"])
    fim se

    // Se nenhuma condição foi preenchida, retornar nulo (incondicional)
    se lista cond.DistroFamily está vazia:
        retornar nulo
    fim se

    retornar cond
fim função

// ------------------------------------------------------------------
// ORDENAÇÃO POR METHOD_ORDER
// ------------------------------------------------------------------

função OrdenarPorReferencia(metodos, ordem):
    // Construir mapa de prioridade baseado na ordem canônica
    prio = map<string, int> vazio
    para i de 0 até tamanho(ordem) - 1:
        prio[ordem[i]] = i
    fim para

    // Ordenação estável
    ordenar metodos usando comparador:
        aExiste = a.Kind existe em prio
        bExiste = b.Kind existe em prio

        se aExiste e bExiste:
            retornar prio[a.Kind] - prio[b.Kind]
        fim se

        se aExiste:
            retornar -1   // a tem prioridade conhecida, vem antes
        fim se

        se bExiste:
            retornar 1    // b tem prioridade conhecida, vem antes
        fim se

        retornar 0        // nenhum tem prioridade; manter ordem relativa original
    fim ordenar

    retornar metodos
fim função

// ------------------------------------------------------------------
// UTILITÁRIOS
// ------------------------------------------------------------------

função ConverteParaStringSlice(raw):
    se raw é []string:
        retornar raw
    fim se

    se raw é []interface:
        resultado = lista vazia de string
        para cada item em raw:
            resultado.adicionar(item como string)
        fim para
        retornar resultado
    fim se

    retornar lista vazia de string
fim função
