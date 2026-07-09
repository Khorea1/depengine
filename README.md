# depengine

Motor distro-agnostic de instalação de dependências, guiado por `schema.toml`.
Escrito em Go: compila para binário estático único, sem runtime dependency
(Python, Node etc. não precisam existir na máquina alvo — só o binário e o
`scripts/detect_os.sh`).

## Por que Go

O `detect_os.sh` já é POSIX sh puro justamente para não exigir nada além do
que uma máquina Unix-like já tem de fábrica. Go estende essa filosofia pro
motor: `go build` gera um binário estático, cross-compilável pra qualquer
`GOOS/GOARCH` (linux, darwin, freebsd, openbsd, windows...) sem toolchain
extra na máquina de destino. Bibliotecas de terceiros (ex: parser de TOML,
na próxima etapa) são dependência só de **build**, nunca de runtime.

## Estrutura atual

```
depengine/
├── scripts/
│   └── detect_os.sh     # fetcher (fornecido, não modificado) — POSIX sh
├── pkg/
│   ├── run/              # seam de subprocesso: Runner interface + OSExecRunner
│   └── native/           # registro declarativo (clan -> Manager) + BuildXxxCmd
├── facts.go              # invoca detect_os.sh via Runner; parse JSON -> Facts (puro)
├── resolve.go            # Facts (distro_id/id_like) -> "clan" (ResolveFamily + MatchesDistroFamily)
├── util.go               # helper de timeout pra subprocessos
├── engine_test.go        # integração: GatherFacts, invariância de Facts, fallback
└── main.go               # CLI de demonstração (consome pkg/native + pkg/run)
```

1. **`Facts`** (`facts.go`): roda `detect_os.sh --json --no-prompt` como
   subprocesso (timeout de 10s) via `pkg/run.Runner`, e faz parse pro
   struct `Facts`. O struct é espelho 1:1 do JSON do fetcher — nada é
   derivado ou mutado aqui. O clan é computado pelo chamador com
   `ResolveFamily`, quando precisa.

2. **`ResolveFamily`** (`resolve.go`): o `target_family` do script é só
   "unix/windows/unknown" — granularidade grossa demais pra escolher entre
   apt/pacman/dnf/zypper. `ResolveFamily` deriva um **clan** (`debian`,
   `arch`, `fedora`, `suse`, `alpine`, `void`, `gentoo`, `macos`,
   `termux`, `freebsd`, `openbsd`, `netbsd`), com dois níveis:
   - match direto por `distro_id` (tabela fixa, cobre as distros comuns);
   - fallback por tokens de `distro_id_like` (cobre distros novas/nicho
     sem precisar cadastrar cada uma — coberto em `engine_test.go`).

   Clan é o quê `when = { distro_family = [...] }` compara (via
   `MatchesDistroFamily(clan, allowed)` — a função recebe o clan já
   resolvido, não lê do `Facts`). É distinto da chave de manager: quando
   windows/winget/choco/scoop enfim entrarem, o mapa `pkg/native` ganha
   chaves mais finas enquanto `when.distro_family` continua comparando
   clan. Veja o doc do pacote `pkg/native`.

3. **`pkg/native`**: dicionário puramente declarativo (dado, não código)
   de clan -> `Manager` (instalar / checar / sincronizar). Adicionar uma
   distro nova é editar o mapa, não escrever função nova. `BuildXxxCmd`
   faz a substituição de `{pkg}` + sudo. O acesso ao manager passa
   exclusivamente por `native.Lookup(clan)` — `main.go` não indexa o
   mapa diretamente.

## Rodando

```sh
export GOPROXY=direct GOSUMDB=off   # só necessário se seu ambiente não
                                     # tiver acesso a proxy.golang.org
go build -o depengine .
./depengine git       # mostra os comandos que seriam rodados pra "git"
go test ./...         # roda a suíte cobrindo o dicionário inteiro
```

Por padrão o binário procura `scripts/detect_os.sh` ao lado de si mesmo.
Para apontar outro caminho: `DEPENGINE_DETECT_SCRIPT=/caminho/script.sh ./depengine`.

## O que falta (próximas etapas, em ordem sugerida)

1. **Parser do `schema.toml`** usando `github.com/pelletier/go-toml/v2`
   (já resolvido via `go get` direto do GitHub, sem depender do proxy
   oficial do Go). Precisa achatar as 3 formas de declarar um método
   (lista `simple`, inline table, bloco `[tools.x.metodo]`) numa única
   struct interna — ver a conversa anterior sobre o formato do schema.

2. **Adapters de linguagem**: `cargo`, `go install`, `pip`, `pipx`, `uv`,
   cada um com sua sintaxe de install/check própria (não são "família de
   distro", são por ferramenta — não entram em `NativeManagers`).

3. **Adapters de `git` e `http`**: clone+build manual, e download de
   asset com resolução de `{latest}` (API do GitHub) + checksum.

4. **Executor com `method_order` e fallback**: tenta os métodos de uma
   tool na ordem certa, distinguindo "manager não existe nessa máquina"
   (pula) de "existe mas falhou" (decide se aborta ou tenta o próximo).

5. **Resolução de `requires`**: instalar dependências entre tools antes
   da tool que depende delas (grafo simples, sem ciclos esperados).

6. **`postinstall`**: hook rodado após qualquer método ter sucesso.
