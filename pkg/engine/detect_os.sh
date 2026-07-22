#!/bin/sh
# detect_os.sh — detecção portátil de arquitetura/distro/SO
# POSIX sh puro (testado em dash, bash --posix, ash/busybox).
# Sem dependências que geralmente exigem instalação manual (nada de
# ripgrep, gawk, jq, python...). Usa apenas utilitários que já vêm
# de fábrica em praticamente qualquer sistema Unix-like: uname, sed,
# grep, tr, cut, cat, kill, sleep, e opcionalmente mktemp.
#
# Uso:
#   ./detect_os.sh              # saída legível (stdout normal, prompts em stderr)
#   ./detect_os.sh --env        # saída KEY='value' segura para eval
#   ./detect_os.sh --json       # saída JSON
#   ./detect_os.sh --no-prompt  # nunca pergunta nada interativamente
#
# Exit codes:
#   0 = detecção completa e confiável
#   1 = detecção parcial (algum campo ficou "unknown", ou confiança
#       baixa/manual)
#   2 = falha total em modo não-interativo (nada pôde ser detectado
#       e não havia como perguntar ao usuário)
set -u
LC_ALL=C
LANG=C
export LC_ALL LANG
# ---------------------------------------------------------------
# CLI args
# ---------------------------------------------------------------
OUT_FORMAT="text"
NO_PROMPT=0
for _arg in "$@"; do
    case "$_arg" in
        --env) OUT_FORMAT="env" ;;
        --json) OUT_FORMAT="json" ;;
        --no-prompt) NO_PROMPT=1 ;;
        -h|--help)
            cat <<EOF
Uso: $0 [--env|--json] [--no-prompt]
  --env         saída como KEY='value' (seguro para eval)
  --json        saída em JSON
  --no-prompt   nunca faz perguntas interativas
EOF
            exit 0
            ;;
    esac
done
[ -n "${CI:-}" ] && NO_PROMPT=1
[ -n "${NONINTERACTIVE:-}" ] && NO_PROMPT=1
# ---------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------
have_cmd() {
    command -v "$1" >/dev/null 2>&1
}
# gera um arquivo temporário sem depender obrigatoriamente de mktemp
_mktemp_safe() {
    _mts_cnt=0
    if have_cmd mktemp; then
        _mts_f=$(mktemp 2>/dev/null) && [ -n "$_mts_f" ] && { printf '%s' "$_mts_f"; return 0; }
    fi
    _mts_f="/tmp/.detect_os.$$.$_mts_cnt"
    while ! (umask 077; : > "$_mts_f") 2>/dev/null; do
        _mts_cnt=$((_mts_cnt + 1))
        _mts_f="/tmp/.detect_os.$$.$_mts_cnt"
    done
    printf '%s' "$_mts_f"
    return 0
}
# sanitize <valor> [permitir_numerico(0|1)]
# devolve o valor limpo via stdout, código de saída 0/1
sanitize() {
    _sv_val=$1
    _sv_allownum=${2:-0}
    [ -z "$_sv_val" ] && return 1
    # trim inicial + normaliza \n num único sed; colapsa quebras reais
    _sv_val=$(printf '%s' "$_sv_val" | sed -e '
        s/^[ \t]*//; s/[ \t]*$//
        s/\\n/ /g
    ' | tr '\n' ' ')
    # remove UMA aspa (simples ou dupla) na borda esquerda
    case "$_sv_val" in
        \"*) _sv_val=${_sv_val#\"} ;;
        \'*) _sv_val=${_sv_val#\'} ;;
    esac
    # remove UMA aspa na borda direita
    case "$_sv_val" in
        *\") _sv_val=${_sv_val%\"} ;;
        *\') _sv_val=${_sv_val%\'} ;;
    esac
    # trim pós-aspas: cobre casos como NAME=" Ubuntu "
    _sv_val=$(printf '%s' "$_sv_val" | sed 's/^[ \t]*//; s/[ \t]*$//')
    # defesa contra campo corrompido/hostil: trunca em 256 chars
    _sv_len=${#_sv_val}
    if [ "$_sv_len" -gt 256 ]; then
        _sv_val=$(printf '%s' "$_sv_val" | cut -c1-256)
    fi
    [ -z "$_sv_val" ] && return 1
    # rejeita caracteres de controle (ASCII < 32 ou 127), exceto TAB.
    # tab é removido de uma cópia só pra teste, sem alterar o valor real.
    _sv_notab=$(printf '%s' "$_sv_val" | tr -d '\t')
    if printf '%s' "$_sv_notab" | grep -q '[[:cntrl:]]' 2>/dev/null; then
        return 1
    fi
    if [ "$_sv_allownum" != "1" ]; then
        case "$_sv_val" in
            ''|*[!0-9]*) : ;;   # tem pelo menos um não-dígito -> ok
            *) return 1 ;;      # só dígitos -> rejeita (regra de ID/NAME)
        esac
    fi
    printf '%s' "$_sv_val"
    return 0
}
# run_cmd_safe <timeout_seg> <binario> [args...]
# roda o comando com timeout manual (sem depender do utilitário
# externo "timeout", que não existe por padrão no macOS/BSD),
# captura stdout, e sempre passa o resultado por sanitize()
# permitindo numérico (essa regra é de ID/NAME de distro, não faz
# sentido aplicá-la a saída de comando genérica como versão do Android).
run_cmd_safe() {
    _rcs_timeout=$1; shift
    _rcs_bin=$1
    have_cmd "$_rcs_bin" || return 1
    _rcs_out_f=$(_mktemp_safe) || return 1
    _rcs_rc_f="${_rcs_out_f}.rc"
    (
        "$@" >"$_rcs_out_f" 2>/dev/null
        echo "$?" > "$_rcs_rc_f"
    ) &
    _rcs_pid=$!
    _rcs_elapsed=0
    while kill -0 "$_rcs_pid" 2>/dev/null; do
        if [ "$_rcs_elapsed" -ge "$((_rcs_timeout * 10))" ]; then
            kill -TERM "$_rcs_pid" 2>/dev/null
            sleep 0.5
            kill -KILL "$_rcs_pid" 2>/dev/null
            wait "$_rcs_pid" 2>/dev/null
            rm -f "$_rcs_out_f" "$_rcs_rc_f"
            return 1
        fi
        sleep 0.1
        _rcs_elapsed=$((_rcs_elapsed + 1))
    done
    wait "$_rcs_pid" 2>/dev/null
    _rcs_rc=1
    [ -f "$_rcs_rc_f" ] && _rcs_rc=$(cat "$_rcs_rc_f" 2>/dev/null)
    _rcs_out=""
    [ -f "$_rcs_out_f" ] && _rcs_out=$(cat "$_rcs_out_f" 2>/dev/null)
    rm -f "$_rcs_out_f" "$_rcs_rc_f"
    if [ "${_rcs_rc:-1}" -ne 0 ] 2>/dev/null || [ -z "$_rcs_out" ]; then
        return 1
    fi
    sanitize "$_rcs_out" 1
    return $?
}
# run_fast_cmd — rodagem direta para comandos que nunca bloqueiam
# (ex: uname). Sem fork/exec extra, sem temporários, sem timeout.
run_fast_cmd() {
    _rfc_out=$("$@" 2>/dev/null) || return 1
    [ -z "$_rfc_out" ] && return 1
    sanitize "$_rfc_out" 1
}
# aspas seguras para o modo --env (para poder dar eval na saída sem
# risco de injeção via $, `, ; etc. que o sanitize() NÃO remove)
shquote() {
    printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}
jsonescape() {
    printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}
bool_str() {
    [ "$1" -eq 1 ] 2>/dev/null && printf 'true' || printf 'false'
}
# parse_os_release <arquivo>
# faz parsing manual, sem "source"/eval, tolerante a CRLF e a linhas
# que não batem com CHAVE=VALOR.
parse_os_release() {
    _por_file=$1
    [ -r "$_por_file" ] || return 1
    ENV_ID="" ; ENV_NAME="" ; ENV_PRETTY_NAME="" ; ENV_ID_LIKE="" ; ENV_VERSION_ID=""
    while IFS='=' read -r _por_key _por_val; do
        case "$_por_key" in
            \#*|'') continue ;;
        esac
        case "$_por_key" in
            ID)          ENV_ID=$(sanitize "$_por_val" 0) || ENV_ID="" ;;
            NAME)        ENV_NAME=$(sanitize "$_por_val" 0) || ENV_NAME="" ;;
            PRETTY_NAME) ENV_PRETTY_NAME=$(sanitize "$_por_val" 0) || ENV_PRETTY_NAME="" ;;
            ID_LIKE)     ENV_ID_LIKE=$(sanitize "$_por_val" 0) || ENV_ID_LIKE="" ;;
            VERSION_ID)  ENV_VERSION_ID=$(sanitize "$_por_val" 1) || ENV_VERSION_ID="" ;;
        esac
    done <<POR_EOF
$(tr -d '\r' < "$_por_file")
POR_EOF
    return 0
}
# ---------------------------------------------------------------
# Estado inicial
# ---------------------------------------------------------------
TARGET_ARCH=""
DISTRO_ID=""
DISTRO_NAME=""
DISTRO_ID_LIKE=""
TARGET_FAMILY="unknown"
DETECTION_METHOD=""
IS_WSL=0
IS_CONTAINER=0
IS_ANDROID=0
CONFIDENCE="low"
DETECTED=0
_kernel=""
KERNEL=""
LIBC=""
INIT_SYSTEM=""
OS=""
# ---------------------------------------------------------------
# Passo 0: arquitetura (sempre primeiro, barato, quase universal)
# ---------------------------------------------------------------
TARGET_ARCH=$(run_fast_cmd uname -m)
[ -z "$TARGET_ARCH" ] && TARGET_ARCH="unknown"
# ---------------------------------------------------------------
# Passo 1: sinais de ambiente (flags, não decidem distro sozinhos)
# ---------------------------------------------------------------
if [ -e /.dockerenv ] || [ -e /run/.containerenv ]; then
    IS_CONTAINER=1
elif [ -n "${container:-}" ]; then
    IS_CONTAINER=1
elif [ -r /proc/1/cgroup ] && grep -Eq 'docker|containerd|lxc' /proc/1/cgroup 2>/dev/null; then
    IS_CONTAINER=1
elif [ -r /proc/self/cgroup ] && grep -Eq 'docker|containerd|lxc' /proc/self/cgroup 2>/dev/null; then
    IS_CONTAINER=1
fi
if [ -r /proc/version ] && grep -qi 'microsoft' /proc/version 2>/dev/null; then
    IS_WSL=1
elif [ -n "${WSL_DISTRO_NAME:-}" ] || [ -n "${WSL_INTEROP:-}" ]; then
    IS_WSL=1
fi
# ---------------------------------------------------------------
# Tentativa 1: Termux
# ---------------------------------------------------------------
if [ "$DETECTED" -eq 0 ]; then
    _is_termux=0
    if [ -n "${TERMUX_VERSION:-}" ]; then
        _is_termux=1
    elif [ -n "${PREFIX:-}" ]; then
        case "$PREFIX" in
            *com.termux*) _is_termux=1 ;;
        esac
    fi
    if [ "$_is_termux" -eq 1 ]; then
        DISTRO_ID="termux"
        DISTRO_NAME="Termux"
        TARGET_FAMILY="unix"
        DETECTION_METHOD="termux"
        IS_ANDROID=1
        CONFIDENCE="high"
        DETECTED=1
    fi
fi
# ---------------------------------------------------------------
# Tentativa 2: Android genérico (não-Termux)
# ---------------------------------------------------------------
if [ "$DETECTED" -eq 0 ]; then
    if have_cmd getprop || [ -e /system/build.prop ]; then
        DISTRO_ID="android"
        _andver=$(run_cmd_safe 2 getprop ro.build.version.release)
        if [ -n "$_andver" ]; then
            DISTRO_NAME="Android $_andver"
        else
            DISTRO_NAME="Android"
        fi
        TARGET_FAMILY="unix"
        DETECTION_METHOD="android-generic"
        IS_ANDROID=1
        CONFIDENCE="medium"
        DETECTED=1
    fi
fi
# ---------------------------------------------------------------
# Tentativa 3: Linux padrão via /etc/os-release (com fallback)
# ---------------------------------------------------------------
if [ "$DETECTED" -eq 0 ]; then
    _osrel=""
    if [ -r /etc/os-release ]; then
        _osrel=/etc/os-release
    elif [ -r /usr/lib/os-release ]; then
        _osrel=/usr/lib/os-release
    fi
    if [ -n "$_osrel" ]; then
        parse_os_release "$_osrel"
        if [ -n "$ENV_ID" ] || [ -n "$ENV_NAME" ]; then
            if [ -n "$ENV_ID" ]; then
                DISTRO_ID=$(printf '%s' "$ENV_ID" | tr '[:upper:]' '[:lower:]')
            else
                DISTRO_ID=$(printf '%s' "$ENV_NAME" | tr '[:upper:]' '[:lower:]' | tr ' ' '-')
            fi
            if [ -n "$ENV_PRETTY_NAME" ]; then
                DISTRO_NAME="$ENV_PRETTY_NAME"
            elif [ -n "$ENV_NAME" ]; then
                DISTRO_NAME="$ENV_NAME"
            else
                DISTRO_NAME="$DISTRO_ID"
            fi
            DISTRO_ID_LIKE="$ENV_ID_LIKE"
            TARGET_FAMILY="unix"
            DETECTION_METHOD="os-release"
            CONFIDENCE="high"
            DETECTED=1
        fi
        # se ID e NAME vieram vazios, arquivo está corrompido/incompleto:
        # não seta DETECTED, cai naturalmente pra tentativa 4
    fi
fi
# ---------------------------------------------------------------
# Tentativa 4: macOS
# ---------------------------------------------------------------
if [ "$DETECTED" -eq 0 ]; then
    _kernel=$(run_fast_cmd uname -s)
    if [ "$_kernel" = "Darwin" ]; then
        DISTRO_ID="macos"
        _pname=$(run_cmd_safe 2 sw_vers -productName)
        _pver=$(run_cmd_safe 2 sw_vers -productVersion)
        if [ -n "$_pname" ] && [ -n "$_pver" ]; then
            DISTRO_NAME="$_pname $_pver"
        elif [ -n "$_pname" ]; then
            DISTRO_NAME="$_pname"
        else
            DISTRO_NAME="macOS"
        fi
        TARGET_FAMILY="unix"
        DETECTION_METHOD="macos"
        CONFIDENCE="high"
        DETECTED=1
    fi
fi
# ---------------------------------------------------------------
# Tentativa 5: BSDs
# ---------------------------------------------------------------
if [ "$DETECTED" -eq 0 ]; then
    case "$_kernel" in
        FreeBSD|OpenBSD|NetBSD|DragonFly)
            _kver=$(run_fast_cmd uname -r)
            DISTRO_ID=$(printf '%s' "$_kernel" | tr '[:upper:]' '[:lower:]')
            if [ -n "$_kver" ]; then
                DISTRO_NAME="$_kernel $_kver"
            else
                DISTRO_NAME="$_kernel"
            fi
            TARGET_FAMILY="unix"
            DETECTION_METHOD="bsd"
            CONFIDENCE="high"
            DETECTED=1
            ;;
    esac
fi
# ---------------------------------------------------------------
# Tentativa 6: Windows via camada POSIX (Cygwin/MSYS/MINGW)
# ---------------------------------------------------------------
if [ "$DETECTED" -eq 0 ] && [ -n "$_kernel" ]; then
    case "$_kernel" in
        *MINGW*|*CYGWIN*|*MSYS*)
            DISTRO_ID="windows"
            DISTRO_NAME="Windows (via $_kernel)"
            TARGET_FAMILY="windows"
            DETECTION_METHOD="windows-posix-layer"
            CONFIDENCE="high"
            DETECTED=1
            ;;
    esac
fi
# ---------------------------------------------------------------
# Tentativa 7: fallback genérico via uname
# ---------------------------------------------------------------
if [ "$DETECTED" -eq 0 ] && [ -n "$_kernel" ]; then
    _kver=$(run_fast_cmd uname -r)
    DISTRO_ID="unknown"
    if [ -n "$_kver" ]; then
        DISTRO_NAME="$_kernel $_kver"
    else
        DISTRO_NAME="$_kernel"
    fi
    TARGET_FAMILY="unix"
    DETECTION_METHOD="uname-fallback"
    CONFIDENCE="low"
    DETECTED=1
fi
# ---------------------------------------------------------------
# Tentativa 8: input manual (só se tudo falhou)
# ---------------------------------------------------------------
if [ "$DETECTED" -eq 0 ]; then
    if [ -t 0 ] && [ "$NO_PROMPT" -eq 0 ]; then
        printf 'Detecção automática falhou.\n' >&2
        if [ "$TARGET_ARCH" = "unknown" ]; then
            printf 'Arquitetura (ex: x86_64, arm64) [enter p/ pular]: ' >&2
            IFS= read -r -t 60 _ans_arch 2>/dev/null || _ans_arch=""
            _san=$(sanitize "$_ans_arch" 1) && [ -n "$_san" ] && TARGET_ARCH="$_san"
        fi
        printf 'Nome da distro/SO [enter p/ pular]: ' >&2
        IFS= read -r -t 60 _ans_name 2>/dev/null || _ans_name=""
        _san_name=$(sanitize "$_ans_name" 0) || _san_name=""
        printf 'Família (unix/windows/outro) [enter p/ pular]: ' >&2
        IFS= read -r -t 60 _ans_fam 2>/dev/null || _ans_fam=""
        _san_fam=$(sanitize "$_ans_fam" 0) || _san_fam=""
        if [ -n "$_san_name" ]; then
            DISTRO_NAME="$_san_name"
            DISTRO_ID=$(printf '%s' "$_san_name" | tr '[:upper:]' '[:lower:]' | tr ' ' '-')
        else
            DISTRO_ID="unknown"
            DISTRO_NAME="unknown"
        fi
        case "$_san_fam" in
            unix|Unix|UNIX) TARGET_FAMILY="unix" ;;
            windows|Windows|WINDOWS) TARGET_FAMILY="windows" ;;
            *) TARGET_FAMILY="unknown" ;;
        esac
        DETECTION_METHOD="user-input"
        CONFIDENCE="manual"
        DETECTED=1
    else
        # não trava pipelines/CI esperando input que nunca vai vir
        DISTRO_ID="unknown"
        DISTRO_NAME="unknown"
        TARGET_FAMILY="unknown"
        DETECTION_METHOD="failed-noninteractive"
        CONFIDENCE="none"
        DETECTED=1
    fi
fi
# ---------------------------------------------------------------
# Passo extra: detecção de kernel, libc, init_system, os
# ---------------------------------------------------------------
# _kernel já pode ter sido setado nas tentativas (macOS/BSD/uname-fallback).
# Aqui garantimos o valor de uname -s caso ainda esteja vazio, e populamos
# KERNEL com a versão completa (uname -r), OS com o nome do SO (uname -s),
# LIBC (best-effort via ldd/getconf) e INIT_SYSTEM (systemd/openrc/...).
if [ -z "$_kernel" ]; then
    _kernel=$(run_fast_cmd uname -s)
fi
OS=$(printf '%s' "$_kernel" | tr '[:upper:]' '[:lower:]')
KERNEL=$(run_fast_cmd uname -r)
[ -z "$KERNEL" ] && KERNEL="unknown"
[ -z "$OS" ] && OS="unknown"
LIBC="unknown"
if have_cmd ldd; then
    # ldd --version imprime glibc/musl/... na primeira linha
    _ldd_out=$(ldd --version 2>/dev/null | head -n1)
    case "$_ldd_out" in
        *musl*) LIBC="musl" ;;
        *GLIBC*|*glibc*|*"GNU C Library"*) LIBC="glibc" ;;
        *"uClibc"*) LIBC="uClibc" ;;
        *FreeBSD*) LIBC="freebsd-libc" ;;
        *) : ;;
    esac
fi
if [ "$LIBC" = "unknown" ] && have_cmd getconf; then
    # heurística: getconf GNU_LIBC_VERSION existe em glibc
    _gv=$(getconf GNU_LIBC_VERSION 2>/dev/null)
    [ -n "$_gv" ] && LIBC="glibc"
fi
INIT_SYSTEM="unknown"
if [ -d /run/systemd/system ] || [ "$(readlink /sbin/init 2>/dev/null)" = "/usr/lib/systemd/systemd" ]; then
    INIT_SYSTEM="systemd"
elif [ -x /sbin/openrc-run ] || [ -d /etc/openrc ]; then
    INIT_SYSTEM="openrc"
elif have_cmd runit; then
    INIT_SYSTEM="runit"
elif [ -x /sbin/init ] && [ "$(readlink /sbin/init 2>/dev/null)" != "" ]; then
    _init_tgt=$(readlink /sbin/init 2>/dev/null)
    case "$_init_tgt" in
        *systemd*) INIT_SYSTEM="systemd" ;;
        *openrc*)  INIT_SYSTEM="openrc" ;;
        *runit*)   INIT_SYSTEM="runit" ;;
        *sysvinit*|*SysV*) INIT_SYSTEM="sysvinit" ;;
        *) INIT_SYSTEM="unknown" ;;
    esac
fi
# ---------------------------------------------------------------
# Exit code
# ---------------------------------------------------------------
EXIT_CODE=0
if [ "$DETECTION_METHOD" = "failed-noninteractive" ]; then
    EXIT_CODE=2
elif [ "$TARGET_ARCH" = "unknown" ] || [ "$DISTRO_ID" = "unknown" ] || \
     [ "$TARGET_FAMILY" = "unknown" ] || [ "$CONFIDENCE" = "low" ] || \
     [ "$CONFIDENCE" = "manual" ] || [ "$CONFIDENCE" = "none" ]; then
    EXIT_CODE=1
fi
# ---------------------------------------------------------------
# Saída
# ---------------------------------------------------------------
case "$OUT_FORMAT" in
    json)
        printf '{\n'
        printf '  "target_arch": "%s",\n'      "$(jsonescape "$TARGET_ARCH")"
        printf '  "distro_id": "%s",\n'        "$(jsonescape "$DISTRO_ID")"
        printf '  "distro_name": "%s",\n'      "$(jsonescape "$DISTRO_NAME")"
        printf '  "distro_id_like": "%s",\n'   "$(jsonescape "$DISTRO_ID_LIKE")"
        printf '  "target_family": "%s",\n'    "$(jsonescape "$TARGET_FAMILY")"
        printf '  "detection_method": "%s",\n' "$(jsonescape "$DETECTION_METHOD")"
        printf '  "confidence": "%s",\n'       "$(jsonescape "$CONFIDENCE")"
        printf '  "is_wsl": %s,\n'             "$(bool_str "$IS_WSL")"
        printf '  "is_container": %s,\n'       "$(bool_str "$IS_CONTAINER")"
        printf '  "is_android": %s,\n'         "$(bool_str "$IS_ANDROID")"
        printf '  "kernel": "%s",\n'          "$(jsonescape "$KERNEL")"
        printf '  "libc": "%s",\n'           "$(jsonescape "$LIBC")"
        printf '  "init_system": "%s",\n'     "$(jsonescape "$INIT_SYSTEM")"
        printf '  "os": "%s"\n'              "$(jsonescape "$OS")"
        printf '}\n'
        ;;
    env)
        printf 'TARGET_ARCH=%s\n'       "$(shquote "$TARGET_ARCH")"
        printf 'DISTRO_ID=%s\n'         "$(shquote "$DISTRO_ID")"
        printf 'DISTRO_NAME=%s\n'       "$(shquote "$DISTRO_NAME")"
        printf 'DISTRO_ID_LIKE=%s\n'    "$(shquote "$DISTRO_ID_LIKE")"
        printf 'TARGET_FAMILY=%s\n'     "$(shquote "$TARGET_FAMILY")"
        printf 'DETECTION_METHOD=%s\n'  "$(shquote "$DETECTION_METHOD")"
        printf 'CONFIDENCE=%s\n'        "$(shquote "$CONFIDENCE")"
        printf 'IS_WSL=%s\n'            "$(bool_str "$IS_WSL")"
        printf 'IS_CONTAINER=%s\n'      "$(bool_str "$IS_CONTAINER")"
        printf 'IS_ANDROID=%s\n'        "$(bool_str "$IS_ANDROID")"
        printf 'KERNEL=%s\n'           "$(shquote "$KERNEL")"
        printf 'LIBC=%s\n'            "$(shquote "$LIBC")"
        printf 'INIT_SYSTEM=%s\n'     "$(shquote "$INIT_SYSTEM")"
        printf 'OS=%s\n'              "$(shquote "$OS")"
        ;;
    *)
        printf 'TARGET_ARCH: %s\n'       "$TARGET_ARCH"
        printf 'DISTRO_ID: %s\n'         "$DISTRO_ID"
        printf 'DISTRO_NAME: %s\n'       "$DISTRO_NAME"
        printf 'DISTRO_ID_LIKE: %s\n'    "$DISTRO_ID_LIKE"
        printf 'TARGET_FAMILY: %s\n'     "$TARGET_FAMILY"
        printf 'DETECTION_METHOD: %s\n'  "$DETECTION_METHOD"
        printf 'CONFIDENCE: %s\n'        "$CONFIDENCE"
        printf 'IS_WSL: %s\n'            "$(bool_str "$IS_WSL")"
        printf 'IS_CONTAINER: %s\n'      "$(bool_str "$IS_CONTAINER")"
        printf 'IS_ANDROID: %s\n'        "$(bool_str "$IS_ANDROID")"
        printf 'KERNEL: %s\n'           "$KERNEL"
        printf 'LIBC: %s\n'            "$LIBC"
        printf 'INIT_SYSTEM: %s\n'     "$INIT_SYSTEM"
        printf 'OS: %s\n'              "$OS"
        ;;
esac
