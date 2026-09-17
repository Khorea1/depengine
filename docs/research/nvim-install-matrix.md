# Veredito: ergonomia do schema, via stress-test com o nvim

Branch: `analysis/nvim-schema-ergonomics`. Artefato irmão:
`docs/research/nvim-install-matrix.toml` (validado de verdade contra o
binário compilado a partir desta árvore — não é análise só de leitura de
código).

Nota de escopo: `docs/research/` guarda investigação pontual, não
referência viva do schema nem exemplo real — não copiar o `.toml` para
`schema.example.toml`/`manifest.example.toml` (ver E1 em `.dev/TODO.md`,
arquivo local não versionado).

Fontes: `nvim-sources.md` (neovim.io/doc/install + release v0.12.5 do GitHub),
cruzado com `pkg/methodkind/methodkind.go`, `pkg/native/registry.go`,
`docs/schema-reference.md` e os testdata de `pkg/validate`.

---

## 1. O schema é ergonômico? — Para o caso comum, sim. Sem ressalvas relevantes.

```toml
simple = ["zsh", "bat", "kitty"]
fd     = { apt = "fd-find" }
ruff   = { python = true }
```

Isso é genuinamente bom design:

- **Sem ambiguidade estrutural**: `merge:"overwrite"` / `merge:"union"` /
  `merge:"methods"` em `pkg/config/model.go` são explícitos por campo — não
  há "quem ganha" implícito.
- **JSON Schema gerado, não escrito à mão** (`cmd/schema-gen/main.go` lê
  `pkg/methodkind.Contracts` como fonte única de verdade).
  `additionalProperties: false` em cada nível barra typo de campo já no
  editor (taplo/VSCode), antes mesmo de rodar `validate`. Isso resolve
  metade do problema de "auditabilidade" antes que ele exista.
- **`depengine why <tool>`** é a ferramenta de depuração certa: mostra,
  candidato a candidato, por que cada um foi pulado ou está pronto. Testei —
  funciona exatamente como documentado (ver seção 3).
- **Erros de validação são específicos e acionáveis**
  (`E_DANGLING_REF`, `W_MISPLACED_PLACEHOLDER`, `W_AUTO_CHECKSUM`...), não
  "parse error genérico".

Um usuário novo, para 80% dos casos reais, escreve isso corretamente na
primeira tentativa. O README não exagera ao chamar isso de "três linhas".

## 2. Onde a ergonomia quebra: o caso "todos os métodos"

O `.dev/TODO.md` já prevê exatamente esse risco (item E1: *"não fazer 'cada
tool com todos os métodos'... declarar métodos sem distribuição real cria
fallbacks que sempre falham"*) — e o exemplo `[tools.neovim.http]` hoje em
`schema.example.toml` já é, sem ironia, um bug ao vivo desse tipo exato
(TODO E2). Construí `analysis-nvim-kitchen-sink.toml` como o pior caso real
que o próprio site do neovim documenta, e **validei contra o binário
compilado desta branch** — não é especulação:

```
$ depengine validate --schema analysis-nvim-kitchen-sink.toml
✓ schema is valid

$ depengine why nvim --schema analysis-nvim-kitchen-sink.toml
  ✓ native         → ready to install
  ✓ appimage       → ready to install
  ✓ git            → ready to install
  ✓ github         → ready to install
  ✓ linux-tarball  → ready to install   # ← quebrado, e "pronto" mesmo assim
  – scoop / choco / msi / win-zip  skipped (when != windows)
  ✗ snap / winget   unavailable (binário não está no PATH desta máquina)
```

`linux-tarball` e `github` recebem `✓ ready to install` do próprio motor,
mas ambos delegam extração para o adapter `http`, que não sabe lidar com o
formato real que o release do neovim publica: um diretório raiz aninhado
(`nvim-linux-x86_64/bin/nvim`), não um binário solto. `Check` depois disso
procura um binário num lugar que nunca vai existir. **Isso não é um caso
hipotético** — é literalmente o bug já sinalizado no TODO contra
`schema.example.toml`, e eu o reproduzi de forma independente contra a
distribuição Windows (`.zip`) e via `github` (asset-pattern), confirmando
que a causa raiz é estrutural (no adapter `http`), não um exemplo mal
escrito isoladamente.

### Gaps encontrados, por severidade

| # | Método real do nvim | O que falta no schema | Classe |
|---|---|---|---|
| 1 | `.tar.gz`/`.zip` oficiais (Linux e Windows) | sem "strip do diretório raiz do arquivo" nem gestão de `PATH` — `http`/`github`/`appimage` (parcial) não resolvem isso | **Silencioso** — valida e "instala" sem funcionar |
| 2 | MSI (nightly Windows) | `http` não reconhece `.msi`; não existe "rodar instalador após o download" | **Silencioso** — baixa o arquivo e para aí |
| 3 | `snap install nvim --classic` | contrato `snap` só tem `{pkg}`, sem flag de confinamento | **Fatal e silencioso na escrita** — falha só em runtime |
| 4 | `scoop bucket add main` / `dnf copr enable` / `add-apt-repository ppa:...` | nenhum conceito de "registrar fonte/bucket/tap/PPA" — só existe o hook cru `pre_install`, tool-level, não idempotente | **Estrutural**, aparece 3× de formas independentes (Scoop, Fedora Copr, Ubuntu PPA) — não é acidente, é uma classe de requisito ausente |
| 5 | `choco install neovim --pre` | nenhum manager "nativo" tem campo de flag/canal — só `github` resolve canal (`release`/`branch`) | **Assimetria** de design, não bug |
| 6 | `make install` com múltiplos caminhos (`bin` + `share`) | `git`'s `Remove` só rastreia um `binary` | **Cosmético** — `install` funciona, `remove` deixa lixo |
| 7 | `requires_when` amarrado a fatos de plataforma, não ao candidato de método vencedor | dependência de PPA instala mesmo quando o candidato PPA nunca roda | **Sutil**, só aparece quando `requires` depende de qual método específico ganhou |

O que **não** é gap de schema, e eu descartei deliberadamente para não
inflar a lista: falta de `nix`, `guix`, `eopkg`, `urpmi`, `poldek`, GoboLinux
etc. em `pkg/native/registry.go`. O `Manager` ali é 100% dado declarativo — é
trabalho de adicionar entradas, não uma limitação do formato do schema.
Confundir isso com ergonomia do schema seria dishonesto.

## 3. "Um macaco preencheria isso facilmente?"

**Depende de qual pergunta o macaco está respondendo.**

- *"Instale zsh, bat, nvim do jeito óbvio"* → sim, sem dúvida. Copia o
  padrão de `schema.example.toml`, o JSON Schema barra erro de digitação, e
  funciona.
- *"Cubra todo método real que o neovim.io documenta"* → não. Nesse ponto
  o macaco precisa saber, de cabeça, sem o schema te avisar:
  - que `snap` exige `--classic` e o schema não vai reclamar se você
    esquecer;
  - que arquivo `.tar.gz`/`.zip` "vai instalar" mas o binário pode não
    ficar em lugar nenhum alcançável;
  - que existe uma classe inteira de passo de setup (bucket/tap/PPA/copr)
    que o schema simplesmente não tem vocabulário para descrever, então a
    "solução" vira um hook `pre_install` cru, sem verificação de idempotência,
    escrito à mão — nesse momento você não está mais preenchendo um
    schema declarativo, está escrevendo um script de shell dentro de uma
    string TOML.

Isso não é "virou uma linguagem de programação" no sentido de sintaxe (a
sintaxe continua simples e legível célula por célula). É pior, na verdade:
**o schema aceita e valida o caso quebrado como se fosse igual ao caso que
funciona.** `linux-tarball` e `native` aparecem lado a lado como `✓ ready to
install` na saída de `why` — nada nessa interface distingue "vai funcionar"
de "vai parecer que funcionou e vai falhar depois". Uma linguagem de
programação real pelo menos falharia em compile-time ou em runtime de forma
ruidosa; aqui você só descobre rodando `status` depois e vendo que `nvim`
não está no `PATH`.

**Veredito final:** o schema é ergonômico *dentro do envelope que ele foi
desenhado para cobrir* — gerenciador nativo, ecossistema de linguagem, git,
http/github "limpo" (arquivo único, sem diretório aninhado). Fora desse
envelope — instaladores de plataforma (MSI), managers com flags
obrigatórias fora de `{pkg}` (snap `--classic`), e qualquer coisa que exija
"registrar uma fonte antes de instalar" (bucket/tap/PPA/copr) — o schema não
fica difícil, fica **silenciosamente incorreto**, o que é objetivamente pior
para quem está preenchendo (audita "válido ✓" e segue a vida) do que ficar
difícil.

## 4. Se for extrapolar em código (não fiz — é análise, não patch)

Pontos com melhor relação esforço/ganho, em ordem:
1. `http`/`github`: campo `strip_components` (ou `root = "auto"`, à la
   `tar --strip-components`) — resolve os gaps #1 e, por extensão, o TODO
   E2 já existente. Maior alavancagem de todos: um campo remove o bug já
   documentado e destrava `.tar.gz`/`.zip` oficiais de qualquer projeto, não
   só nvim.
2. `snap`: campo `classic`/`confinement` booleano — pequeno, evita falha
   garantida em runtime para qualquer pacote que precise dele.
3. Um `ensure`/`source` genérico (bucket/tap/ppa/copr) com `Check` próprio —
   maior esforço, resolve a classe inteira #4 de uma vez. Candidato a spec
   dedicada, não a um campo solto.
