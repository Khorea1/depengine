# Auditoria incremental de testes: contratos JSON da CLI

**Skill:** `felipessoal11/gjalla-test-audit` v1 (Skillsync)
**Data:** 2026-11-15 (America/Recife)
**Escopo:** `internal/app/machine_output_test.go`, `internal/app/remove_undo_forget_test.go`, `internal/app/import_test.go`, `internal/app/import.go` e contrato em `docs/cli-reference.md`. Revisão estática das mudanças recentes; não é uma nova auditoria da suíte inteira.
**Contexto:** a auditoria anterior está registrada em `dep-test-audit-2026-11-13-g8g`. Os problemas abaixo não duplicam suas filhas existentes.

## Veredito

**Dois achados verificáveis por inspeção**: um teste de contrato com verificações que podem passar indevidamente (Tier 2) e uma falha de saída JSON sem teste de regressão (Tier 4 + defeito no caminho de erro). Os testes auditados chamam código real; não há motivo para excluir arquivos inteiros.

## Testes que podem aprovar comportamento errado

### A1 — Tier 2: o teste de envelope aceita saída inadequada para scripts

**Evidência:** `internal/app/remove_undo_forget_test.go:64-66` combina stdout e stderr em um único `strings.Builder`. `internal/app/machine_output_test.go:24-34` procura o início de um objeto JSON com `version` como primeira chave e ignora todo o prefixo. `TestMachineOutputEnvelope` (linhas 193-218) decodifica apenas a partir desse ponto. Mesmo quando existe uma função `tc.assert`, um erro ao converter `data` para `map[string]any` provoca `return` imediato (linhas 213-217), sem executar a asserção do caso.

**Por que importa:** o contrato de `docs/cli-reference.md:764-768` exige **exatamente um** documento JSON em stdout e progresso/avisos em stderr. Um comando que imprimir texto antes do JSON em stdout pode passar no teste. Um payload de tipo incorreto, como lista em vez de objeto para `why` ou `audit`, também pode passar apesar das asserções específicas configuradas.

**Ação sugerida:** testar stdout e stderr separadamente em `TestMachineOutputEnvelope`; decodificar o stdout completo; falhar se o tipo de `data` contrariar o formato esperado de cada comando; acrescentar regressões negativas para um prefixo de texto em stdout e um payload em lista no lugar de objeto. Preservar os testes positivos e o harness existente onde ele ainda serve aos testes de CLI humana.

## Lacuna de cobertura e defeito correspondente

### A2 — Tier 4: `import --json` não produz envelope em falha de escrita

**Evidência:** `internal/app/import_test.go:256-313` cobre preview, gravação bem-sucedida, conflito, TOML inválido e formato desconhecido. Não cobre falha após a validação. Em `internal/app/import.go:223-226`, o erro de `writeRecipeFile` escreve apenas em stderr e retorna código 3, sem chamar `writeReport` nem `writeMachineOutput`. O contrato da documentação exige documento JSON em stdout para `--json` (`docs/cli-reference.md:764-768`).

**Cenário determinístico a testar:** criar um diretório no lugar de `<catalog>/tool.toml` e executar `import <receita-válida> --catalog <catalog> --force --json`. A inspeção do destino passa, mas renomear o arquivo temporário sobre o diretório falha; nesse ramo, o stdout fica vazio. O cenário foi inferido diretamente do fluxo `Lstat -> writeRecipeFile -> os.Rename`; **não foi executado**, porque as dependências do pacote `internal/app` não estão no ambiente.

**Ação sugerida:** primeiro fixar o contrato de falha JSON com teste de comando real e filesystem temporário; depois emitir um resultado de erro estruturado e documentado também no ramo de escrita, sem sinalizar sucesso ou ocultar efeitos parciais. Cobrir separadamente falha na escrita do próprio documento JSON, que ocorre depois da gravação, para não reportar incorretamente uma operação efetivada como não realizada.

## Exclusões e impacto

- **Tier 1 (testes que não exercitam código real):** nenhum no recorte.
- **Tier 2:** somente o helper de envelope e suas verificações, sem recomendar apagar a suíte.
- **Tier 3 (redundantes com teste real):** nenhum comprovado.
- **Tier 4:** caso de falha de escrita de `import --json` sem teste.
- **Bugs em código de produção:** ramo de erro de escrita do `import --json` não respeita a saída documentada; classificação baseada em leitura de código, sem reprodução binária.
- **Cobertura:** não há medição confiável de cobertura do `internal/app` neste ambiente; excluir testes reduziria proteção real e não é recomendado.

## Validação e limites

- `go test -race -count=1 ./internal/run`: **passou**, mas não valida os achados de `internal/app`.
- `GOTOOLCHAIN=local GOPROXY=off go test ./internal/app -run '^TestImport' -count=1`: **não compilou**, porque `cobra`, `pflag` e `go-toml/v2` não estão disponíveis no cache local.
- **Nenhum código ou teste alterado nesta auditoria.** A limpeza da skill (fase 5) exige aprovação posterior. Próxima ação: implementar testes negativos e corrigir o contrato de erro com as dependências disponíveis.
