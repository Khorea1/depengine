# Depengine: proposta de schema e plano de implementação (E: distributions)

> **Estado: proposta recebida, NÃO aprovada. Requer aprovação formal da fronteira
> antes de qualquer código.**
>
> - Recebido em: 2026-10-11 · Origem: proposta externa (texto importado na íntegra abaixo).
> - Escopo: `dep-1n0` (reduzir duplicação de candidatos GitHub por sistema e libc).
> - Relação com o spike A×C (`dep-1n0.1`–`.4`, branch `spike/cases-expansion`):
>   a proposta recomenda adotar E e descartar C (`cases`, `asset_variants`, overrides
>   universais). O spike C continua válido como experimento, mas deixaria de ser o
>   caminho de implementação se E for aprovada.
> - **Conflito aberto**: os critérios atuais de `dep-1n0` (revisados em 2026-10-11 para
>   "declaração compacta → N candidatos convencionais") são incompatíveis com E
>   ("uma distribuição nunca cria outro método"). Adotar E exige revisar `dep-1n0`
>   novamente. Não fazer isso silenciosamente.
> - Tracking: issue Beads filha de `dep-1n0` para a proposta E (ver `br list`).

---

# Depengine: proposta de schema e plano de implementação

Minha recomendação é adotar E, mas numa versão mais restrita: métodos independentes, distribuições dentro dos métodos que precisam delas e matching explícito de assets.

Não adotaria a proposta anterior de `overrides` universais, nem a arquitetura com cinco camadas obrigatórias. Isso seria outra forma de multiplicar conceitos.

A decisão mais importante é distinguir quatro coisas sem exigir quatro objetos no TOML:

| Conceito         | Nome no schema                          | Responsabilidade                             |
| ---------------- | --------------------------------------- | -------------------------------------------- |
| Ferramenta       | `tools.<name>` / `packages.<name>`      | Identidade e dependências                    |
| Método           | `github`, `aur`, `native`, `cargo` etc. | Alternativa de instalação e fallback         |
| Distribuição     | `distributions`                         | Escolher a embalagem compatível com o host   |
| Seletor de asset | `asset`                                 | Identificar um arquivo dentro de uma release |

Uma distribuição nunca cria outro método. Essa é a mudança estrutural em relação a C.

Também manteria `schema_version = 1`, conforme a política pré-freeze do projeto. A implementação nova substitui a gramática antiga onde houver conflito, sem parser legado, aliases de migração ou compatibilidade artificial.

Há, porém, uma fronteira que não eliminaria: locks e estado persistido precisam falhar com segurança diante de identidades incompatíveis. Descartar retrocompatibilidade não significa autorizar remoções incorretas.

## 1. Nomenclatura definitiva proposta

Eu usaria:

- `distributions`, e não `variants`, `targets` ou `cases`. Representa alternativas de distribuição de um mesmo software, não estratégias diferentes.
- `asset`, mantendo o nome já usado pelo adapter GitHub.
- `asset = "..."` para o padrão atual com `{version}`, `{arch_any}` e `{os_any}`.
- `asset = { regex = '...' }` para reconhecimento explícito por expressão regular.
- `when` para a elegibilidade de cada distribuição, reutilizando as condições existentes.
- `entrypoints`, `binary`, `strip_components` e `extract_to` com seus nomes atuais.

Não acrescentaria `priority`, `fallback`, `extends`, `default_distribution`, `asset_strategy`, `asset_mode` nem `platform_regex`.

Um método simples deve continuar simples:

```
[packages.example.github]
repo = "owner/example"
asset = "example-{os_any}-{arch_any}.tar.gz"
checksum = "sha256:auto"
scope = "user"
```

Sem `distributions`. Sem estrutura adicional.

A complexidade só aparece quando a publicação do upstream realmente exige regras distintas.

## 2. Contrato das distribuições

A principal decisão é permitir campos comuns na receita-base e exceções apenas nas distribuições.

O schema conceitual seria:

```
MethodCandidate
  kind: string
  label?: string
  when?: Condition
  config: MethodConfig
  distributions?: Distribution[]

Distribution
  when: Condition                   # obrigatório
  asset?: AssetSelector
  checksum?: string
  checksum_url?: string
  checksum_file_format?: string
  signature_url?: string
  binary?: string
  entrypoints?: map[string]string
  strip_components?: integer
  extract_to?: string

AssetSelector
  string                           # template existente
  OU
  { regex: string }                # regex explícita
```

`Distribution` não deve virar parte de `plan.Artifacts`: esse tipo já representa payloads concretos, enquanto `distributions` representa alternativas de seleção.

### Semântica exata

| Situação                                              | Decisão                                                     |
| ----------------------------------------------------- | ----------------------------------------------------------- |
| Método sem `distributions`                            | Funciona com os campos diretos atuais                       |
| Método com `distributions`                            | A lista é exaustiva; não existe distribuição-base implícita |
| Uma distribuição compatível                           | Herda campos comuns e aplica suas exceções                  |
| Nenhuma distribuição compatível                       | Método inelegível, permitindo próximo método                |
| Várias distribuições compatíveis                      | Erro de ambiguidade, antes de consultar GitHub              |
| Campo ausente numa distribuição                       | Herda o valor-base                                          |
| Campo escalar declarado                               | Substitui o valor-base                                      |
| `entrypoints` declarado                               | Substitui o mapa inteiro                                    |
| `repo`, `release`, `branch`, `scope` por distribuição | Proibido nesta primeira versão                              |

O `when` do método é uma restrição adicional ao `when` de cada distribuição. Não substitui nem faz OR com ele.

A lista é uma allowlist de ambientes suportados. Isso evita que a receita-base se aplique acidentalmente a um sistema não declarado.

### Uma escolha importante: não permitir patches de mapas

Exemplo:

```
entrypoints = { br = "br" }
```

Uma distribuição Windows informa:

```
entrypoints = { br = "br.exe" }
```

O segundo mapa substitui o primeiro integralmente.

Não utilizaria deep merge. Com um merge parcial, um entrypoint Linux poderia sobreviver indevidamente numa distribuição Windows.

Para os outros campos, a herança é direta: campo ausente significa manter o valor comum. Não precisamos de `extends`, `inherit = true` ou `default = true`.

### Sobre a allowlist

A lista inicial contém somente campos de seleção, integridade e materialização. Não incluiria `post_install`, `pre_install`, `requires`, `repo`, `release` ou `kind`.

Entretanto, não manteria essa lista duplicada no parser, no validador e no JSON Schema. O contrato de cada método em `internal/methodkind` deve identificar quais campos admitem especialização por distribuição.

Isso permite eventualmente oferecer o recurso a outro adapter sem inventar mais uma política de validação.

## 3. Schemas reais propostos

### 3.1. `br`: quatro distribuições, uma origem

```
schema_version = 1

[packages.br.github]
repo = "Dicklesworthstone/beads_rust"
asset = "br-{version}-{os_any}_{arch_any}.tar.gz"
checksum = "sha256:auto"
scope = "user"
entrypoints = { br = "br" }

[[packages.br.github.distributions]]
when = { os = ["darwin"] }

[[packages.br.github.distributions]]
when = { os = ["linux"], libc = ["glibc"], is_android = false }

[[packages.br.github.distributions]]
when = { os = ["linux"], libc = ["musl"], is_android = false }
asset = "br-{version}-{os_any}_musl_{arch_any}.tar.gz"

[[packages.br.github.distributions]]
when = { os = ["windows"], arch = ["x86_64"] }
asset = "br-{version}-{os_any}_{arch_any}.zip"
entrypoints = { br = "br.exe" }
```

As regras correspondem aos nomes de assets do seu manifesto. Acrescentei `is_android = false` nas distribuições Linux como proteção intencional contra Termux/Android.

O significado é preciso: uma única origem GitHub, com um único método na preferência e quatro distribuições admitidas.

O ZIP Windows continua independente da issue `dep-laz`. O `.exe` está dentro do arquivo compactado, não é o artefato baixado diretamente.

### 3.2. `omp`: binários brutos com exceção musl

```
[packages.omp.github]
repo = "can1357/oh-my-pi"
asset = "omp-{os_any}-{arch_any}"
checksum = "sha256:auto"
checksum_url = "https://github.com/can1357/oh-my-pi/releases/latest/download/SHA256SUMS.txt"
checksum_file_format = "sha256sum"
scope = "user"

[[packages.omp.github.distributions]]
when = { os = ["darwin"] }

[[packages.omp.github.distributions]]
when = { os = ["linux"], libc = ["glibc"], is_android = false }

[[packages.omp.github.distributions]]
when = { os = ["linux"], libc = ["musl"], is_android = false }
asset = "omp-linux-musl-{arch_any}"
```

Aqui fica especialmente evidente o benefício: `repo`, checksum, formato do checksum e escopo aparecem uma única vez.

A escolha da distribuição é resolvida antes da instalação. O método continua podendo instalar um binário bruto pelo pipeline HTTP existente.

Há um detalhe a verificar na implementação: quando uma receita usa uma release fixada, o `checksum_url` precisa corresponder à mesma release do asset escolhido. Não devemos resolver o binário numa tag e conferir um arquivo agregado de outra tag.

### 3.3. `wt`: GitHub como alternativa, AUR preservado

```
[packages.wt]
method_only = ["native", "aur", "cargo", "conda", "github"]

post_install = [
  { run = ["wt", "config", "shell", "install"],
    when = { target_family = ["unix"] } },

  { run = ["git-wt", "config", "shell", "install"],
    when = { target_family = ["windows"] } },
]

[packages.wt.native]
pkg_overrides = { brew = "worktrunk", winget = "max-sixty.worktrunk" }
when = { os = ["darwin", "windows"] }

[packages.wt.aur]
pkg = "worktrunk-bin"
when = { distro_family = ["arch"] }

[packages.wt.cargo]
pkg = "worktrunk"

[packages.wt.conda]
pkg = "worktrunk"
channels = ["conda-forge"]

[packages.wt.github]
repo = "max-sixty/worktrunk"
asset = "worktrunk-{arch}-unknown-linux-musl.tar.xz"
checksum = "sha256:auto"
strip_components = 1
scope = "user"
entrypoints = { wt = "wt", git-wt = "git-wt" }

[[packages.wt.github.distributions]]
when = { os = ["linux"], is_android = false }

[[packages.wt.github.distributions]]
when = { os = ["darwin"] }
asset = "worktrunk-{arch}-apple-darwin.tar.xz"
```

A escolha do `worktrunk-bin` no Arch permanece uma preferência legítima de método.

Observe que o `post_install` não foi copiado para nenhuma distribuição. Ele já possui condições próprias e deve continuar pertencendo ao ciclo de vida da ferramenta.

Isso é algo que não mudaria: não usar distribuições para resolver problemas que os hooks condicionais já resolvem.

## 4. Regex: contrato de matching, não heurística de seleção

O Depengine já possui matching baseado em regex em `internal/ghrelease/assetmatch.go`.

Atualmente, os placeholders `{os_any}` e `{arch_any}` viram grupos de aliases. A implementação já exige uma correspondência única na lista de assets.

A extensão correta, portanto, é pequena em conceito:

```
asset = "atlas-{os_any}-{arch_any}.tar.gz"
```

Continua significando um template literal, com placeholders reconhecidos.

A nova alternativa seria:

```
asset = {
  regex = 'atlas-{version}-(?:linux|gnu-linux)-{arch_any}\.tar\.gz'
}
```

Exemplo de sintaxe proposta, ainda não suportada. A regex completa é ancorada automaticamente pelo motor.

### Decisões sobre regex

| Regra                         | Comportamento                                            |
| ----------------------------- | -------------------------------------------------------- |
| Motor                         | RE2 via `regexp` do Go                                   |
| Case sensitivity              | Insensível a maiúsculas/minúsculas, como o matcher atual |
| Âncoras                       | `^` e `$` aplicados pelo Depengine                       |
| `{arch_any}`                  | Grupo escapado dos aliases conhecidos do host            |
| `{os_any}`                    | Grupo escapado dos aliases conhecidos do host            |
| `{version}`                   | Tag real da release, inserida com escaping               |
| Regex inválida                | Erro de validação                                        |
| Zero assets correspondentes   | Erro de resolução                                        |
| Vários assets correspondentes | Erro de ambiguidade                                      |
| Um asset correspondente       | Prossegue à validação do formato e integridade           |

Não introduziria seleção por pontuação, ordem da publicação, preferência por extensão ou escolha automática do asset mais recente entre os encontrados.

Uma expressão como `win|windows` isoladamente é ruim. Além de não identificar arquitetura ou formato, `win` pode ocorrer em `darwin`.

Para nomes convencionais, `{os_any}` já resolve melhor esse problema.

Regex é especialmente útil quando os nomes publicados misturam tokens e convenções que os placeholders atuais não expressam.

### Segurança e determinismo

Há três propriedades diferentes:

Determinismo: dadas a mesma release, os mesmos fatos de host e a mesma expressão, a seleção produz o mesmo resultado ou erro.

Integridade: o arquivo escolhido é verificado contra um digest esperado.

Compatibilidade: o arquivo realmente funciona naquela plataforma.

Regex ajuda na primeira. Checksum ajuda na segunda. Nenhuma das duas garante a terceira.

Por isso, regex continua vinculada ao `when` da distribuição e ao contrato do método. Um nome que corresponda à expressão, mas possua formato proibido pelo adapter, continua sendo rejeitado.

Eu também imporia limites de tamanho da expressão e de quantidade de distribuições, evitando padrões e manifests patológicos. Não fixaria os números antes de medir o matcher e os limites existentes.

### Não aplicar regex arbitrária aos pacotes AUR

`worktrunk-bin`, `worktrunk-git` e `worktrunk` podem ter políticas de atualização diferentes. Correspondência de nomes não significa equivalência operacional.

O matching por regex deve atuar inicialmente sobre assets de uma release, não sobre nomes de pacotes a serem instalados.

Para Native, AUR e gerenciadores de ecossistema, continuaria utilizando identidade explícita de pacote e as regras próprias de cada adapter.

## 5. O ponto mais importante da implementação: especialização antes do plano

Depois de revisar `internal/planner/intent.go`, eu não colocaria a seleção de distribuições somente dentro de `GitHubAdapter.ResolvePlan()`.

Isso seria tarde demais.

`BuildCandidateIntent()` já projeta identidade, artefatos, operações e hooks. Se escolhermos o asset depois dessa etapa, poderemos acabar com um plano que descreve os entrypoints Linux e instala o binário Windows.

A seleção da distribuição deve ocorrer antes da construção do primeiro plano efetivo.

1\. Parse + validação

Preserva e valida todas as distribuições

2\. Merge de camadas

Combina schema e manifesto

3\. Seleção de método

Prioridade, elegibilidade e fallback

4\. Especialização da distribuição

Seleciona uma distribuição e materializa campos efetivos

5\. BuildCandidateIntent

Produz o plano usando somente campos efetivos

6\. Resolução da release

Encontra exatamente um asset e verifica o contrato

7\. Execução, estado e lock

Consomem a mesma identidade resolvida

A etapa 4 deve ser uma função pura, sem rede e sem efeitos sobre o host.

Uma assinatura conceitual:

```
func SpecializeDistribution(
    method *config.MethodCandidate,
    facts *platform.Facts,
) (*config.MethodCandidate, error)
```

Ela retorna uma cópia especializada do candidato convencional.

Não modifica a configuração original. Não escolhe outro método. Não consulta GitHub. Não instala nada.

O resultado é consumido pelos componentes existentes.

### Semântica do merge

O merge entre schema de projeto e manifesto pessoal acontece antes da especialização.

Para evitar outro subsistema de herança:

- A configuração do mesmo método continua usando a precedência existente entre camadas.
- `distributions` é uma lista atômica: uma camada que a redefine substitui a lista inteira.
- Não existe merge por índice ou tentativa de combinar distribuições por `when`.
- Os campos comuns continuam seguindo suas regras normais de merge.
- As exceções da distribuição são aplicadas somente depois do merge.

Assim, não precisamos de labels de distribuição para implementar herança entre camadas.

Uma consequência importante: se uma camada superior trocar o `repo` e mantiver distribuições herdadas, ela estará assumindo explicitamente que elas continuam válidas. O validador verificará sua estrutura, mas não poderá garantir offline que a nova release publica aqueles assets.

## 6. Modelo de falhas: separar inelegibilidade de defeito

Esse é um contrato essencial para não transformar o fallback em um mecanismo que esconde configurações quebradas.

| Situação                                          | Classe sugerida          | Comportamento                 |
| ------------------------------------------------- | ------------------------ | ----------------------------- |
| Método não suporta o host                         | `unavailable_candidate`  | Tentar próximo método         |
| Nenhuma distribuição corresponde ao host          | `host_incompatibility`   | Tentar próximo método         |
| Duas distribuições correspondem                   | `invalid_manifest`       | Interromper com erro          |
| Regex inválida                                    | `invalid_manifest`       | Interromper com erro          |
| Distribuição selecionada, nenhum asset encontrado | `resolution_failure`     | Erro, sem fallback silencioso |
| Dois assets encontrados                           | `resolution_failure`     | Erro, sem escolha arbitrária  |
| Artefato proibido pelo adapter                    | `unsupported_capability` | Erro explícito                |
| Checksum não corresponde                          | Falha de integridade     | Abortar instalação            |

As classes sugeridas aproveitam categorias já existentes em `plan.PlannerError`, quando aplicáveis.

O detalhe decisivo é a diferença entre método inelegível e método elegível cuja distribuição publicada está quebrada.

No segundo caso, não permitiria simplesmente tentar Cargo ou AUR em silêncio. Isso ocultaria alterações inesperadas do upstream.

### E as condições sobrepostas?

Não tentaria implementar um solucionador completo de satisfatibilidade para `when`.

O validador pode rejeitar antecipadamente interseções óbvias, como duas distribuições com `os = ["windows"]` e `arch = ["x86_64"]`.

Mas, como `when` possui distribuição, versão, libc, WSL, container e outras dimensões, a detecção estática universal seria complexa.

A garantia definitiva acontece na especialização: se mais de uma distribuição corresponder aos fatos reais, o Depengine falha.

Isso é determinístico e fail-closed, sem criar outro motor lógico.

## 7. Como escalar para 10 ou mais métodos

O schema não deve ganhar uma matriz global de plataformas.

Cada método controla a própria variabilidade, conforme seu contrato.

Um exemplo conceitual:

```
[packages.atlas.native]
pkg = "atlas"
pkg_overrides = { apt = "atlas-cli", pacman = "atlas" }

[packages.atlas.aur]
pkg = "atlas-bin"
when = { distro_family = ["arch"] }

[packages.atlas.cargo]
pkg = "atlas"

[packages.atlas.go]
pkg = "example.org/atlas"

[packages.atlas.npm]
pkg = "@example/atlas"

[packages.atlas.pipx]
pkg = "atlas-cli"

[packages.atlas.conda]
pkg = "atlas"
channels = ["conda-forge"]

[packages.atlas.nix]
pkg = "atlas"

[packages.atlas.github]
repo = "example/atlas"
checksum = "sha256:auto"
scope = "user"

[[packages.atlas.github.distributions]]
when = { os = ["linux"], arch = ["x86_64"] }
asset = "atlas-linux-amd64.tar.gz"

[[packages.atlas.github.distributions]]
when = { os = ["windows"], arch = ["x86_64"] }
asset = "atlas-windows-amd64.zip"
entrypoints = { atlas = "atlas.exe" }
```

Exemplo de estrutura, não fixture validada. Os nomes de pacotes e repositórios são fictícios.

Os métodos que não precisam de distribuições continuam declarativos e curtos.

Se amanhã Cargo precisar de target-specific configuration, a evolução deverá ocorrer no contrato Cargo, não em um mecanismo global que reinterpreta todos os métodos.

### E se um MSI também vier do GitHub?

Aqui existe uma oportunidade concreta que torna desnecessário criar uma hierarquia universal de origens.

No código atual, o adapter `msi` já admite a alternativa `repo + asset`, além de `url`. O resolvedor GitHub também é utilizado pela infraestrutura compartilhada de download.

Portanto, podemos ter algo conceitualmente equivalente a:

```
[packages.atlas.msi]
repo = "example/atlas"
asset = "atlas-{version}-windows-{arch_any}.msi"
pkg = "Atlas"
publisher = "Example Inc."
when = { os = ["windows"] }
```

O `github` representa o método de download portátil; `msi` representa um método com ciclo de instalação próprio. Ambos podem resolver assets publicados no GitHub sem que precisemos transformar o método `github` num instalador universal.

Eu manteria `distributions` inicialmente no método `github`, mas faria a seleção de assets de maneira reutilizável. Os outros adapters que já oferecem `repo + asset` poderiam optar pela mesma capacidade posteriormente, através de seus contratos.

Isso evita a inconsistência arquitetural que preocupava nossa análise anterior, sem criar uma abstração obrigatória de `source` para todos os métodos.

## 8. Plano de implementação

Eu dividiria em sete entregas, todas dentro de uma única iniciativa arquitetural, mas com commits e testes independentes.

1. Fechar o contrato e substituir a proposta C

   Formalizar a decisão no `dev-notes` e atualizar `dep-1n0`: um candidato por método, distribuições internas sem labels operacionais, erro diante de seleção ambígua.

   Registrar explicitamente o descarte de `cases`, `asset_variants`, overrides universais e fallback entre distribuições.

   Entrega: especificação de semântica e casos de teste, sem alterar execução.
2. Implementar o modelo de configuração

   Introduzir o tipo `Distribution` e o tipo discriminado `AssetSelector` em `internal/config`.

   Estender parser, clonagem, merge, JSON Schema e validação estrita. Validar todas as distribuições, inclusive as inativas no host atual.

   Adicionar metadados de campos especializáveis em `internal/methodkind`, evitando uma segunda allowlist.

   Entrega: os três manifests reais são aceitos na nova gramática e produzem erros precisos para entradas inválidas.
3. Implementar a especialização por host

   Criar uma função pura de especialização, usando `Condition.Match()` e campos herdados.

   Aplicá-la depois do merge e antes de `BuildCandidateIntent()`, sem alterar a identidade do método.

   Cobrir ausência, unicidade e ambiguidade de distribuições. Preservar a ordenação entre métodos.

   Entrega: um único `github` por ferramenta produz candidatos efetivos corretos em Linux, macOS e Windows.
4. Refinar o matcher GitHub

   Reutilizar `internal/ghrelease/assetmatch.go`.

   Preservar o matching literal existente, acrescentar regex explícita com expansão segura de placeholders e exigir correspondência única.

   Separar o matcher dos acessos à API GitHub para permitir testes com listas de assets em memória.

   Entrega: nenhuma escolha por heurística, ordem da lista ou pontuação.
5. Integrar planejamento, instalação e estado

   Garantir que `BuildCandidateIntent`, `ResolvePlan`, `why`, instalação, observação e remoção compartilhem o candidato efetivo.

   Persistir as informações concretas necessárias ao ciclo de vida, especialmente asset, entrypoints e caminhos instalados. A remoção deve respeitar o registro anterior, não especializar uma distribuição nova.

   Tratar locks antigos incompatíveis com erro explícito; não implementar migrações ou parsers legados.

   Entrega: plano, lock e estado concordam sobre a mesma instalação.
6. Executar testes de estresse e propriedades

   Migrar as fixtures de `br`, `omp` e `wt` para E. Acrescentar uma ferramenta sintética com pelo menos 12 métodos, distribuições cruzadas, condições conflitantes e assets malformados.

   Testar resoluções offline com catálogos GitHub simulados, entradas determinísticas e testes de propriedades para ordem de assets, aliases e ambiguidades.

   Incluir ZIP, binários brutos, `.exe` portátil, formatos proibidos, checksums incorretos e falhas durante instalação/remoção.

   Entrega: a nova representação demonstra escalabilidade e comportamento fail-closed, não apenas redução de TOML.
7. Corte definitivo do schema

   Atualizar `schema/depengine.schema.json`, exemplos, referência, documentação e testes contratuais na mesma mudança.

   Manter `schema_version = 1`, sem `cases`, sem sintaxe paralela de compatibilidade e sem migração automática.

   Remover o spike C depois de preservar o aprendizado relevante em testes definitivos e documentação da decisão.

   Entrega: uma gramática canônica, documentada e testada.

## 9. Critérios de aprovação

Eu usaria estes critérios para autorizar o merge:

| Dimensão       | Critério                                                      |
| -------------- | ------------------------------------------------------------- |
| Identidade     | Uma distribuição não cria candidatos extras                   |
| Determinismo   | Mesmos fatos + mesma release = mesma seleção                  |
| Ambiguidade    | Nenhuma seleção arbitrária                                    |
| Ergonomia      | Métodos simples não recebem campos extras                     |
| Integridade    | Checksums e assinaturas continuam ligados ao asset concreto   |
| Planejamento   | Nenhum campo é reespecializado após criar o plano             |
| Persistência   | Lock e estado preservam a identidade efetivamente instalada   |
| Segurança      | Regex não contorna restrições de formato ou ciclo de vida     |
| Isolamento     | Métodos não relacionados não são afetados por `distributions` |
| Escalabilidade | Novas dimensões não exigem duplicar todos os métodos          |

Não exigiria que o novo schema produza os mesmos `MethodsHash` do antigo. Estamos mudando deliberadamente a representação.

Exigiria, isso sim, que qualquer lock ou estado incompatível seja identificado sem adivinhação, corrupção de identidade ou perda silenciosa de ownership.

## 10. Decisões que eu deixaria fora desta implementação

Existem oportunidades tentadoras de ampliar o escopo: normalizar todo pacote nativo por distribuição, criar um sistema genérico de fontes, substituir todos os métodos por estratégias tipadas, inferir automaticamente arquitetura a partir de regex e eliminar todas as formas de shorthand.

Eu não faria isso nesta PR.

A exceção é `dep-laz`, que pode ser implementada em paralelo ou antes da integração final para permitir `.exe` portátil baixado diretamente. Ela precisa preservar a distinção entre copiar um executável e executar um instalador.

Também manteria a correção da inferência nativa implícita como tarefa separada. É uma inconsistência relevante, mas não é pré-requisito para distribuições GitHub.

## Decisão final recomendada

O desenho que eu levaria à implementação é:

`MethodCandidate` com distribuições opcionais, especialização determinística antes do planejamento e `AssetSelector` literal ou regex explícita.

Sem seleção por especificidade, sem prioridade entre distribuições, sem heurística sobre nomes de pacotes e sem abstração universal de overrides.

Isso não promete resolver toda diferença de plataforma para todo adapter. Promete algo mais sustentável: o Depengine oferece um mecanismo pequeno e consistente para o problema de distribuição de artefatos, deixando cada método responsável por suas particularidades.

A única decisão que eu exigiria antes de iniciar o código é aprovar formalmente essa fronteira: `distributions` especializa uma embalagem dentro do mesmo método; nunca troca o método, a origem ou o ciclo de instalação.

Se essa regra permanecer intacta, o modelo tem boas chances de escalar sem repetir o ciclo de refatorações anteriores.
