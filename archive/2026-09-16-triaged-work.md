# depengine — fila triada (arquivada)

> Fila triada em 2026-09-16 e reconciliada com o estado do repositório em
> 2026-09-17. O conteúdo original, inclusive decisões negativas e critérios de
> aceitação, foi preservado como memória histórica. Evidências de implementação
> identificáveis no Git foram acrescentadas às tarefas concluídas.

> Scratchfile de meta-development. Não mencionar este arquivo em commits.
> Triagem atualizada em 2026-09-16 contra o código da branch
> `docs/triage-todo`. Esta fila registra decisões e trabalho executável; pesquisa
> bruta, documentação copiada de upstream e ideias sem decisão não ficam aqui.

## Ordem de execução

1. Corrigir o contrato documental e os exemplos que hoje ensinam comportamento
   incorreto.
2. Tornar a documentação da CLI derivada da árvore Cobra.
3. Endurecer o suporte existente a WinGet.
4. Só então ampliar o contrato de instalação/download.

Cada tarefa deve terminar com `go test ./...`, `go vet ./...`, `gofmt -l .`
sem saída e build limpo. Testes que dependem de Windows real ou de rede são
explicitamente separados dos unitários.

---

## P0 — contrato e documentação

### D1. Auditar a referência do schema e os exemplos

**Implementado:** `ed4da07`, `8fe6090` e `93fd176` documentaram e fixaram o
contrato público do schema; `e5bee00` consolidou as receitas canônicas.

**Decisão:** fazer agora. Há contradições verificáveis entre a documentação e o
contrato implementado.

**Ler e corrigir nesta ordem:**

1. `docs/schema-reference.md`;
2. `schema.example.toml` e `manifest.example.toml`;
3. `docs/cheatsheet.md`;
4. `README.md`.

**Correções já identificadas:**

- URLs literais de `http`, `appimage` e `android` aceitam `{latest}`, `{arch}` e
  `{os}`, mas não `{version}`, `{arch_any}` nem `{os_any}`; esses três últimos
  pertencem ao matching de assets do método `github`.
- Os exemplos de `appimage`/`android` não podem usar `{version}` na própria URL
  enquanto afirmam que isso não é suportado.
- `kind` é o adapter executado; o nome da subtabela é apenas o label do
  candidato. Ordem de declaração TOML não define prioridade.
- Hooks usam somente `pre_install` e `post_install`; formas legadas ou campos
  inexistentes não devem aparecer como contrato atual.
- Tools exclusivas do manifest são descartadas silenciosamente por padrão e só
  entram com `[manifest] allow_new_tools = true`.
- `schema_version = 1`, tools virtuais e `method_only` exclusivo precisam estar
  descritos sem ambiguidades.
- A lista de campos do `git` no `manifest.example.toml` não deve anunciar
  `checksum`/`signature_url`, pois o contrato de `git` não os aceita.
- Listas de kinds, aliases e defaults derivadas de `pkg/methodkind` não devem
  depender de contagens ou cópias manuais sem teste de paridade.

**Implementar em:**

- documentação: arquivos acima;
- prova do contrato: teste de exemplos em nível de aplicação, fora de
  `pkg/config`, que faça parse **e** validação semântica de
  `schema.example.toml` e `manifest.example.toml`;
- fonte de verdade: `pkg/methodkind/methodkind.go`; não criar uma segunda lista.

**Aceitação:**

- [ ] busca global não encontra claims obsoletos sobre `method_order` como
      alias, prioridade por file order ou `{version}` em URL literal;
- [ ] ambos os arquivos de exemplo passam por parse e validação;
- [ ] todo snippet TOML completo da referência é exercitado como fixture, ou é
      marcado explicitamente como fragmento não autônomo;
- [ ] `go test ./...`, `go vet ./...` e `gofmt -l .` limpos.

### D2. Gerar a referência da CLI a partir da árvore Cobra

**Implementado:** `cd2f757` (`feat: generate CLI documentation from Cobra`).

**Decisão:** fazer depois de D1. Editar help, Markdown e man page manualmente é
triplicação e continuará divergindo.

**Como implementar:**

- extrair da árvore criada por `newRootCmd` comandos, aliases, argumentos,
  flags locais, flags persistentes e defaults;
- criar um gerador determinístico para `docs/cli-reference.md` e
  `docs/depengine.1`, preservando apenas introduções conceituais escritas à mão;
- adicionar teste golden que falha quando a árvore Cobra muda sem regenerar os
  artefatos;
- expor a regeneração por um comando versionado (`go generate` ou
  `go run ./cmd/...`), sem depender do binário instalado.

**Cobertura mínima:** `install --verbose`, flags completas de `check`, `graph`,
`remove`, `diff` e `sbom`, `help --man`, `--version` e todos os comandos
registrados em `cli.go`.

**Implementar em:** `cli.go`, um gerador dedicado sob `cmd/`, teste golden na
raiz, `docs/cli-reference.md` e `docs/depengine.1`.

**Aceitação:**

- [ ] um único comando regenera ambos os documentos sem diff numa segunda
      execução;
- [ ] o teste detecta comando ou flag ausente, extra ou com default divergente;
- [ ] help Cobra, referência Markdown e man page expõem o mesmo contrato.

---

## P1 — exemplos e Windows

### E1. Tornar os exemplos representativos, não enciclopédicos

**Implementado:** `e5bee00` (`docs: consolidate canonical example recipes`).

**Decisão:** fazer, mas não como “cada tool com todos os métodos”. Um exemplo de
referência deve cobrir cada forma do contrato uma vez com uma receita verdadeira;
declarar métodos sem distribuição real cria fallbacks que sempre falham e ensina
configuração ruim.

**Como implementar:**

- adicionar ao `manifest.example.toml` um exemplo canônico de `github` com
  `repo`, `asset`, placeholders de matching e checksum; preferir converter um
  exemplo GitHub/HTTP duplicado em vez de apenas aumentar o arquivo;
- consolidar os exemplos conflitantes `nvim` e `neovim`; manter um único nome e
  demonstrar apenas métodos reais e compatíveis com o contrato atual;
- usar Neovim para mostrar IDs nativos divergentes no Windows
  (`winget = "Neovim.Neovim"`, Scoop/Chocolatey = `neovim`), sem copiar para o
  schema a documentação de instalação de todas as distros;
- manter agrupamento pedagógico por caso/ecossistema. Ordenar alfabeticamente
  apenas itens equivalentes dentro do mesmo grupo;
- montar no teste uma matriz `kind -> exemplo` e exigir cobertura de todos os
  contratos públicos de `pkg/methodkind`, permitindo uma allowlist comentada
  apenas para casos que não cabem num exemplo seguro.

**Implementar em:** `schema.example.toml`, `manifest.example.toml` e teste de
contrato dos exemplos.

**Aceitação:**

- [ ] `github` aparece nos dois exemplos com uma receita parseável e realista;
- [ ] não há duas tools que pretendam representar o mesmo executável Neovim;
- [ ] nenhuma recipe promete instalação que o adapter não consegue concluir ou
      verificar;
- [ ] cobertura dos kinds é derivada do código, não de uma contagem hardcoded.

### E2. Remover ou corrigir o exemplo inválido de archive do Neovim

**Evidência de implementação:** `01fef63` adicionou instalação transacional de
artefatos próprios; `e5bee00` consolidou os exemplos que exercitam o contrato.

**Concluído (2026-09-16):** archives agora usam staging transacional,
`strip_components`, `entrypoints` e ownership simétrico; os exemplos de `fd`,
`node_exporter` e Neovim foram corrigidos.

**Decisão:** correção obrigatória dentro de E1.

O bloco atual `[tools.neovim.http]` extrai `nvim-linux-*.tar.gz` em
`~/.local/bin`, preservando a árvore `nvim-linux-*/bin/nvim`; depois o check
procura `~/.local/bin/neovim`. Ele nunca prova a instalação e o diretório
extraído não fica automaticamente no `PATH`.

**Ação imediata:** remover esse fallback do exemplo ou substituí-lo por um caso
que o contrato atual instala e verifica corretamente. Não mascarar o problema
com `post_install` arbitrário.

**Possível feature separada:** suporte estruturado a archives que carregam um
prefixo completo (strip do diretório raiz + entrypoint/link estável + remoção
simétrica). Isso exige spec própria porque afeta `pkg/httpdownload`, estado e
Windows; não bloquear E1 por ela.

### W1. Endurecer o suporte existente a WinGet

**Implementado:** `70bb1f1`
(`fix(native): harden winget install/check/search/remove flags`).

**Decisão:** fazer. WinGet já está implementado como manager nativo da família
`windows` em `pkg/native/registry.go`; o alias explícito `winget` também é
registrado por `RegisterNativeManagerAliases`. Não criar um terceiro adapter em
`pkg/exec/win.go` — ali ficam apenas Scoop e Chocolatey.

**Problema real:** os comandos atuais usam matching não exato, podem abrir
prompts e não possuem `SearchCmd`, portanto um pacote inexistente pode consumir
a tentativa nativa antes do fallback.

**Como implementar:**

- install: selecionar por `--id --exact`, executar em modo silencioso/não
  interativo e aceitar de forma explícita os agreements de package/source;
- check: `winget list --id {pkg} --exact`, evitando falso positivo por
  substring;
- search: usar consulta read-only exata (`winget show --id {pkg} --exact`) para
  permitir fallback quando o ID não existe;
- remove: selecionar por ID exato e impedir prompts; não fixar `--source winget`
  globalmente, pois isso excluiria packages de outras sources;
- manter `AtomicBatch = false` e nenhuma elevação automática: o instalador de
  cada pacote decide se solicitará UAC.

**Implementar em:** `pkg/native/registry.go`, `pkg/native/native_test.go` e,
somente se a resolução do alias exigir, `pkg/exec/native_adapter_test.go`.
Atualizar os exemplos/docs que ainda apresentam WinGet como ausente.

**Verificação:**

- [ ] unit tests fixam argv completo de install/check/search/remove;
- [ ] `GOOS=windows go build ./...` compila;
- [ ] smoke test em Windows real cobre pacote instalado, ausente e ID ambíguo;
- [ ] instalação automatizada não espera input interativo.

Referências oficiais (Microsoft Learn, consultadas em 2026-09-16):

- [install](https://learn.microsoft.com/windows/package-manager/winget/install);
- [list](https://learn.microsoft.com/windows/package-manager/winget/list);
- [show](https://learn.microsoft.com/windows/package-manager/winget/show);
- [uninstall](https://learn.microsoft.com/windows/package-manager/winget/uninstall).

---

## P2 — downloads especializados

### A1. Permitir que `android` resolva assets de GitHub Releases

**Evidência de implementação:** `130dce5`, `e763089` e `30da1a4` entregaram o
fluxo determinístico de GitHub Releases e seu pin no lockfile; `f94dc4f`
definiu o contrato consolidado de capacidades; `1edc4ff` adicionou dependências
e sources por candidato; `4056c77` passou a dirigir a validação pelos contratos.

**Concluído (2026-09-16):** `android`, `appimage`, `msi`, `github` e `http`
compartilham o contrato `url` ou `repo + asset`, incluindo pin no lockfile.

**Decisão:** procede, depois de D1/E1/W1. Não adicionar uma heurística específica
para nomes de APK.

**Direção:** reutilizar o resolver determinístico de `pkg/ghrelease`. O método
`android` deve aceitar exatamente uma das fontes:

- `url`; ou
- `repo` + `asset`, com `release`/`branch` opcionais.

O segundo formato resolve o asset e delega o download/checksum ao backend comum;
o pós-processamento continua pertencendo ao `AndroidAdapter` (`termux-open`).

**Implementar em:** contrato/validação de grupos mutuamente exclusivos em
`pkg/methodkind` e `pkg/validate`; resolver compartilhado em `pkg/ghrelease` ou
`pkg/httpdownload`; `pkg/httpdownload/android_adapter.go`; JSON Schema, lockfile,
docs e testes.

**Aceitação:**

- [ ] zero ou múltiplos assets são erro, nunca “primeiro match”;
- [ ] a release resolvida é pinada em `depengine.lock`;
- [ ] `url` e `repo`/`asset` juntos são rejeitados;
- [ ] fluxo Android continua sem fingir que o aceite humano do APK é síncrono.

### A2. Especificar instalação de archives com layout de prefixo

**Evidência de implementação:** `01fef63` entregou artefatos próprios
transacionais; `d88e8ac` adicionou MSI dedicado; `ed4da07`, `8fe6090` e
`93fd176` documentaram e fixaram o contrato público correspondente.

**Concluído (2026-09-16):** implementação e spec estão em
`docs/schema-reference.md`; não há camada de migração de schemas antigos.

**Decisão:** investigar; não implementar junto com E2. É o gap que impede usar
corretamente releases como o tarball do Neovim, que contém `bin/`, `lib/` e
`share/` sob um diretório raiz.

Antes de código, definir numa spec:

- strip seguro de componentes para tar e zip;
- destino privado por tool, entrypoint estável no `PATH` e comportamento em
  Windows sem depender de symlink privilegiado;
- check e remove simétricos, sem apagar diretórios compartilhados;
- migração dos campos atuais `extract_to`/`binary` sem quebrar schemas v1.

---

## P3 — ideias em incubação

### R1. Release automation e self-update

**Decisão:** automação de release procede; self-update é outra feature. Separar
CI de release reproduzível da CLI `self-update`. A segunda só avança com
checksum/assinatura, rollback e opt-in explícito; nunca atualizar
silenciosamente.

### R2. Repensar `asdf`/mise

**Decisão:** procede como discovery. Levantar a sobreposição real entre os dois
e desenhar um contrato de version manager comum antes de criar adapters
duplicados. Preservar funcionamento offline e ausência de telemetria.

### R3. Registry comunitário

**Decisão:** adiado. Definir primeiro o modelo de confiança: recipes versionadas,
revisão, pinning e proibição/alerta forte para hooks/build arbitrários. Um
catálogo remoto sem isso vira canal de execução de código.

---

## Decisões negativas desta triagem

- **Não** tornar cada tool “extensiva” com todos os métodos possíveis. Cobertura
  pertence à matriz de contrato/testes; recipes devem refletir distribuições
  reais.
- **Não** ordenar `schema.example.toml` e `manifest.example.toml` globalmente por
  alfabeto. Isso destruiria os grupos pedagógicos e não muda a semântica TOML.
- **Não** manter no TODO a cópia integral da página de instalação do Neovim.
  Pacman/APT/DNF/Brew/etc. já são abstraídos pelo método `native`; managers não
  suportados exigem decisão própria, não uma exceção por tool.
- **Não** adicionar o script PowerShell/MSI como hook: há WinGet e assets de
  release, enquanto hooks arbitrários são bloqueados por padrão por segurança.
- **Não** congelar hashes/nomes de uma release específica do Neovim no backlog;
  fixtures determinísticas usam dados controlados, e recipes reais usam lockfile
  e verificação de checksum.
