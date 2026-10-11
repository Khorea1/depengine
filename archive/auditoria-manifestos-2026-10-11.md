# Depengine — auditoria dos manifests pessoais (11/10/2026)

## Escopo e fontes

- Código-fonte: snapshot `depengine.tar.zst`, HEAD `11bf4d4` (`master`, 2026-10-11).
- Binário testado: `/mnt/data/depengine`, `depengine --version` -> `depengine dev` (fornecido após a primeira rodada).
- Configuração: `depends(2).toml` e `manifest(2).toml` fornecidos pelo usuário.
- Beads: tracker de `depengine/.dev/.beads`, localizado através de `.beads/redirect`. Escritas **somente na cópia local** do snapshot; nenhuma publicação remota.
- Limitação: sandbox sem saída para `api.github.com`; não houve instalação nem teste operacional nativo em Windows, macOS ou Linux/musl.

## Validação executada

```
/mnt/data/depengine validate \
  --schema '/mnt/data/depends(2).toml' \
  --manifest '/mnt/data/manifest(2).toml' --format json
```

Resultado: 0 erros e 9 avisos `W_AUTO_CHECKSUM`, distribuídos em `br` (4), `omp` (3) e `wt` (2). Exit code 0. Com `--strict`, exit code 1. O problema anterior envolvendo `nix` veio de executável antigo presente no pacote; a versão atual o valida corretamente.

## Achados confirmados

### 1. `.exe` portátil é inexpressável em GitHub/HTTP — `dep-laz`, P1

Receita mínima testada:

```toml
schema_version = 1
[tools]
cli = { method_only = ["github"], github = { repo = "org/cli", asset = "cli-windows-amd64.exe", binary = "cli.exe", scope = "user", when = { os = ["windows"] } } }
```

Retorno: `E_INVALID_VALUE` em `tools.cli.methods[0].asset` alegando instalador `.exe` não suportado. Usando `http.url = "https://example.test/cli-windows-amd64.exe"`, falha equivalente. A mesma receita com artefato `.zip` contendo `cli.exe` valida normalmente. A receita própria `exe` foi aceita, mas representa *instalador Windows* e demanda `product_name`, `publisher`, `uninstall_exe` etc.; não representa um arquivo portátil.

Causa: `internal/artifact/contract.go` contém `.exe` em `PlatformInstallerExtensions`; os contratos de `github` e `http` em `internal/methodkind/methodkind.go` proíbem essas extensões.

Proposta mínima: opt-in inequívoco para *portable executable* em `github/http` com host Windows; download/cópia/remoção sem invocar o `.exe`. Preservar o fail-closed para instaladores tradicionais e para os demais formatos bloqueados. Testar pin/lock, path e verificação/remoção. Não liberar `.exe` incondicionalmente.

### 2. Duplicação de candidatos por plataforma — `dep-1n0`, P2

O caso real usa quatro métodos `github` para `br` (darwin, Linux glibc, Linux musl, Windows), três para `omp` e dois para `wt`, repetindo `repo`, `checksum` e/ou `scope`. `{os_any}`, `{arch_any}`, `{version}` resolvem sinônimos, mas `asset` é um único padrão string por candidato; `when` condiciona o candidato inteiro. Quando variam extensão, libc ou nome de entrypoint, o usuário deve clonar o método.

Proposta: extensão aditiva do método GitHub para variantes de *asset* com seletor `os/arch/libc` e overrides necessários, sem criar herança de métodos genérica, nova DSL ou resolução de primeira correspondência silenciosa. Seleção única ou erro claro quando ambígua; preservar candidate ordering e lock/projection.

**Não é duplicação indevida:** manter `worktrunk-bin` via AUR antes de `native` no Arch. A preferência é legítima, não algo a eliminar por normalização.

### 3. `W_AUTO_CHECKSUM` mistura verificação de integridade com confiança — `dep-d1g`, P2

`internal/validate/structural.go` emite `"hash is NOT verified"` para qualquer `<algoritmo>:auto`. Já `internal/httpdownload/adapter.go` (`verifyChecksum`, `resolveAutoChecksum`) busca o checksum complementar, calcula/compara o digest via `VerifyChecksum` e preserva `ResolvedChecksum`. Assim o arquivo baixado **é comparado** ao hash do sidecar, mas a autenticidade do checksum **não é ancorada independentemente** quando vem da mesma fonte sem assinatura.

Adicionalmente, os nove avisos de ferramentas não relacionadas reaparecem em `why wt`, `why delta`, `why br`, poluindo diagnóstico. Manter o alerta de TOFU onde apropriado, corrigir a linguagem e não inundar comandos de inspeção por ferramenta com avisos alheios.

### 4. `why` mostra nome nativo incorreto no texto — `dep-4x1`, P3

Neste ambiente Debian 13, `depengine why delta` apresenta `native ... [pkg=delta]`; `why delta --json` revela que `plan_intent.identity.package = "git-delta"` para o apt, conforme `native.pkg_overrides`. A seleção efetiva está correta, mas o resumo para humanos é enganoso. Apresentar a identidade concreta do plano também no texto.

## Outros limites já rastreados (não duplicados)

- `dep-2nx`: `.deb` de releases sem ciclo de vida dpkg.
- `dep-bj8`: backends `mise/asdf` não expressam certos specs.
- `dep-mhp`: instaladores remotos via shell não representados.
- `dep-i4c`: MacPorts.
- `dep-d5z`: nomes scoped de pacotes registry.
- `dep-wbg`: evolução para lock v2 em configurações nativas.

## Priorização

1. Corrigir `dep-laz` isoladamente; há bloqueio de capacidade de uso real em Windows.
2. Evoluir a seleção de artefato sob `dep-1n0` sem afetar os múltiplos métodos deliberados nem alterar o contrato de ordenação.
3. Corrigir `dep-d1g` e `dep-4x1` como pequenas mudanças de diagnóstico; ambas têm reproduções locais objetivas.

Todas as issues novas foram persistidas via `br create` e são exibidas por `br list` na cópia local. Artefato complementar: `depengine-audit-beads-novos.jsonl` com somente essas quatro issues, sem caminhos absolutos locais.
