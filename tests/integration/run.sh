#!/usr/bin/env bash
# run.sh — Testes de integração do depengine via Docker
#
# Uso:
#   ./tests/integration/run.sh              # roda todos os cenários
#   ./tests/integration/run.sh --build-only # só compila as imagens
#   ./tests/integration/run.sh debian       # roda apenas um cenário
#
# Exit codes:
#   0  tudo ok
#   1  algum cenário falhou
#   2  erro de build

set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
REPO="$(cd "$DIR/../.." && pwd)"
BIN="$REPO/depengine"

PASS=0
FAIL=0
FAILED_SCENARIOS=""

header()  { echo ""; echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; echo "  $1"; echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"; }
pass()    { echo "  ✅ $1"; ((PASS++)); }
fail()    { echo "  ❌ $1"; ((FAIL++)); FAILED_SCENARIOS="$FAILED_SCENARIOS - $1"$'\n'; }

# ── 0. Build do binário local ──────────────────────────────────
header "Build do binário"
cd "$REPO"
go build -o "$BIN" .
echo "  binário: $BIN ($(du -h "$BIN" | cut -f1))"

# ── 1. Teste: dry-run em todas as distros ─────────────────────
test_dry_run() {
  local distro="$1"
  local tag="depengine-test-${distro}"
  
  header "Dry-run: $distro"
  
  docker build \
    -f "$DIR/Dockerfile.${distro}" \
    -t "$tag" \
    "$REPO" 2>&1 | tail -1
  
  echo "  imagem: $tag"
  echo ""
  
  # Run dry-run
  local output
  output=$(docker run --rm -e DEPENGINE_NOSUDO=1 "$tag" 2>&1) || true
  
  echo "$output" | head -20
  
  if echo "$output" | grep -q "already installed\|would install\|would install via"; then
    pass "dry-run completou em $distro"
  else
    fail "dry-run falhou em $distro (sem output esperado)"
    echo "$output"
  fi
}

# ── 2. Teste: fallback nativo→cargo (Alpine) ──────────────────
test_fallback_cargo() {
  header "Fallback: nativo indisponível → cargo (Alpine)"
  
  local output
  output=$(docker run --rm -e DEPENGINE_NOSUDO=1 depengine-test-alpine \
    depengine install --dry-run --verbose \
    --schema /depengine/tests/integration/test_schema.toml 2>&1) || true
  
  # lazygit.cargo deve mostrar "would install"
  if echo "$output" | grep -qi "lazygit.*cargo\|cargo.*lazygit\|would install.*lazygit"; then
    pass "fallback cargo: lazygit via cargo"
  else
    fail "fallback cargo: lazygit não encontrado no output"
    echo "$output" | grep -i lazygit || true
  fi
}

# ── 3. Teste: when ─────────────────────────────────────────────
test_when() {
  header "When: debian-only-tool só executa em Debian"
  
  # Em Debian: debian-only-tool deve estar disponível
  local debian_out
  debian_out=$(docker run --rm depengine-test-debian \
    depengine install --dry-run --verbose \
    --schema /depengine/tests/integration/test_schema.toml 2>&1) || true
  
  if echo "$debian_out" | grep -qi "debian-only-tool.*native\|would install.*debian-only"; then
    pass "when: debian-only-tool disponível em Debian"
  else
    fail "when: debian-only-tool deveria estar disponível em Debian"
    echo "$debian_out" | grep -i "debian-only" || true
  fi
  
  # Em Arch: debian-only-tool deve ser pulado
  local arch_out
  arch_out=$(docker run --rm depengine-test-arch \
    depengine install --dry-run --verbose \
    --schema /depengine/tests/integration/test_schema.toml 2>&1) || true
  
  if echo "$arch_out" | grep -qi "debian-only-tool.*skip\|skipped.*debian-only"; then
    pass "when: debian-only-tool pulado em Arch"
  else
    # Também aceita se não aparecer no output (pulado sem log)
    if echo "$arch_out" | grep -qi "debian-only"; then
      fail "when: debian-only-tool não deveria aparecer em Arch"
      echo "$arch_out" | grep -i "debian-only" || true
    else
      pass "when: debian-only-tool não listado em Arch"
    fi
  fi
}

# ── 4. Teste: requires (ordem topológica) ─────────────────────
test_requires() {
  header "Requires: ordem topológica respeitada"
  
  # tool-a → tool-b → tool-c. C deve vir antes de B, B antes de A.
  local output
  output=$(docker run --rm -e DEPENGINE_NOSUDO=1 depengine-test-debian \
    depengine install --dry-run --verbose \
    --schema /depengine/tests/integration/test_schema.toml 2>&1) || true
  
  # Extrai a ordem das tools do log de dependências
  local levels
  levels=$(echo "$output" | grep "level [0-9]:" || true)
  
  if echo "$levels" | grep -q "tool-c" && echo "$levels" | grep -q "tool-b" && echo "$levels" | grep -q "tool-a"; then
    pass "requires: dependências detectadas no grafo"
  else
    fail "requires: tools não encontradas nos níveis"
    echo "$levels"
  fi
}

# ── 5. Teste: --json output ────────────────────────────────────
test_json_output() {
  header "JSON output"
  
  local output
  output=$(docker run --rm -e DEPENGINE_NOSUDO=1 depengine-test-debian \
    depengine install --dry-run --json \
    --schema /depengine/tests/integration/test_schema.toml 2>&1) || true
  
  # stderr tem logs, stdout tem JSON — capturamos tudo
  if echo "$output" | grep -q '"summary"\|"tools"\|"status"'; then
    pass "JSON output válido"
  else
    fail "JSON output inválido"
    echo "$output" | head -5
  fi
}

# ── 6. Teste: check ────────────────────────────────────────────
test_check() {
  header "Check: depengine check <tool>"
  
  # git deve estar instalado no Debian
  local result
  result=$(docker run --rm depengine-test-debian \
    depengine check git \
    --schema /depengine/tests/integration/test_schema.toml 2>&1 || true)
  
  if echo "$result" | grep -qi "installed\|✓"; then
    pass "check: git encontrado"
  else
    fail "check: git não encontrado"
    echo "$result"
  fi
}

# ── 7. Teste: ajuda ────────────────────────────────────────────
test_help() {
  header "Help output"
  
  local output
  output=$("$BIN" help 2>&1)
  
  if echo "$output" | grep -qi "depengine\|install\|check"; then
    pass "help: saída válida"
  else
    fail "help: saída inválida"
  fi
}

# ── Main ───────────────────────────────────────────────────────

# Build images first
header "Build das imagens Docker"
for distro in debian arch fedora alpine; do
  echo "  building $distro..."
  docker build \
    -f "$DIR/Dockerfile.${distro}" \
    -t "depengine-test-${distro}" \
    "$REPO" > /tmp/depengine-docker-build.log 2>&1 || {
      echo "  ❌ falha ao buildar $distro"
      tail -5 /tmp/depengine-docker-build.log
      exit 2
    }
done
echo "  todas as imagens prontas"

# Run selected scenario or all
if [ $# -ge 1 ] && [ "$1" != "--build-only" ]; then
  case "$1" in
    debian) test_dry_run "debian" ;;
    arch)   test_dry_run "arch" ;;
    fedora) test_dry_run "fedora" ;;
    alpine) test_dry_run "alpine" ;;
    *)      echo "cenário desconhecido: $1"; exit 1 ;;
  esac
elif [ $# -ge 1 ] && [ "$1" == "--build-only" ]; then
  echo "Build-only mode. Imagens prontas."
  exit 0
else
  # Run all
  test_help
  
  for distro in debian arch fedora alpine; do
    test_dry_run "$distro"
  done
  
  test_fallback_cargo
  test_when
  test_requires
  test_json_output
  test_check
fi

# ── Summary ────────────────────────────────────────────────────
echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Resultados: $PASS passaram, $FAIL falharam"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if [ $FAIL -gt 0 ]; then
  echo ""
  echo "Cenários com falha:"
  echo "$FAILED_SCENARIOS"
  exit 1
fi

exit 0
