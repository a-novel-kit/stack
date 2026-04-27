#!/bin/bash

# Shared style and logging helpers for shell scripts in the a-novel ecosystem.
# Source this file (do not execute it) to get color-aware logging primitives:
#
#   . "$(dirname "$0")/lib/style.sh"
#   banner "Doing the thing"
#   log_step  "Phase 1"
#   log_info  "starting"
#   log_success "done"
#   log_warn  "skipped — already up to date"
#   log_error "remote not reachable"
#
# Honors NO_COLOR (force off) and FORCE_COLOR (force on even when stdout is not a TTY).
# Designed to remain readable on both light- and dark-themed terminals: it sets only
# foreground colors, never backgrounds, and avoids pure white / pure black.

# Idempotent — safe to source multiple times across composed scripts.
[ -n "${ANOVEL_STYLE_LOADED:-}" ] && return 0
ANOVEL_STYLE_LOADED=1

__style_use_color() {
    [ -n "${NO_COLOR:-}" ] && return 1
    [ -n "${FORCE_COLOR:-}" ] && return 0
    [ -t 1 ] || return 1
    [ -n "${TERM:-}" ] && [ "${TERM}" != "dumb" ]
}

if __style_use_color; then
    STYLE_RESET=$'\033[0m'
    STYLE_BOLD=$'\033[1m'
    STYLE_DIM=$'\033[2m'

    STYLE_RED=$'\033[31m'
    STYLE_GREEN=$'\033[32m'
    STYLE_YELLOW=$'\033[33m'
    STYLE_BLUE=$'\033[34m'
    STYLE_MAGENTA=$'\033[35m'
    STYLE_CYAN=$'\033[36m'

    STYLE_RED_B=$'\033[1;31m'
    STYLE_GREEN_B=$'\033[1;32m'
    STYLE_YELLOW_B=$'\033[1;33m'
    STYLE_BLUE_B=$'\033[1;34m'
    STYLE_MAGENTA_B=$'\033[1;35m'
    STYLE_CYAN_B=$'\033[1;36m'
else
    STYLE_RESET="" STYLE_BOLD="" STYLE_DIM=""
    STYLE_RED="" STYLE_GREEN="" STYLE_YELLOW="" STYLE_BLUE="" STYLE_MAGENTA="" STYLE_CYAN=""
    STYLE_RED_B="" STYLE_GREEN_B="" STYLE_YELLOW_B="" STYLE_BLUE_B="" STYLE_MAGENTA_B="" STYLE_CYAN_B=""
fi

# Glyphs — Unicode for richness, ASCII fallback would hurt clarity more than help portability.
STYLE_SYM_INFO="•"
STYLE_SYM_OK="✓"
STYLE_SYM_WARN="!"
STYLE_SYM_ERR="✗"
STYLE_SYM_ARROW="▸"
STYLE_SYM_BULLET="·"

# Compute a sensible width for separators / banners.
__style_width() {
    local w="${COLUMNS:-}"
    [ -z "$w" ] && w="$(tput cols 2>/dev/null || echo 72)"
    [ "$w" -gt 80 ] 2>/dev/null && w=80
    [ "$w" -gt 0 ] 2>/dev/null || w=72
    printf '%s' "$w"
}

# Build a horizontal rule of `width` `─` glyphs into the named variable.
# Pure bash — `tr ' ' '─'` would corrupt UTF-8 because tr is byte-oriented.
__style_hrule() {
    local __out_var="$1" __width="$2" __i=0 __line=""
    while [ "${__i}" -lt "${__width}" ]; do
        __line="${__line}─"
        __i=$((__i + 1))
    done
    printf -v "${__out_var}" '%s' "${__line}"
}

# banner "<title>" — boxed-style title for the top of a script run.
banner() {
    local title="$1"
    local width line
    width="$(__style_width)"
    __style_hrule line "${width}"
    printf "\n%s%s%s\n" "${STYLE_DIM}" "$line" "${STYLE_RESET}"
    printf "%s%s  %s%s\n" "${STYLE_BOLD}" "${STYLE_CYAN}" "$title" "${STYLE_RESET}"
    printf "%s%s%s\n" "${STYLE_DIM}" "$line" "${STYLE_RESET}"
}

separator() {
    local width line
    width="$(__style_width)"
    __style_hrule line "${width}"
    printf "%s%s%s\n" "${STYLE_DIM}" "$line" "${STYLE_RESET}"
}

# log_step <text> — major section heading; leading blank line for breathing room.
log_step() {
    printf "\n%s%s%s %s%s%s\n" \
        "${STYLE_MAGENTA_B}" "${STYLE_SYM_ARROW}" "${STYLE_RESET}" \
        "${STYLE_BOLD}" "$*" "${STYLE_RESET}"
}

log_info() {
    printf "  %s%s%s %s\n" "${STYLE_CYAN}" "${STYLE_SYM_INFO}" "${STYLE_RESET}" "$*"
}

log_success() {
    printf "  %s%s%s %s\n" "${STYLE_GREEN_B}" "${STYLE_SYM_OK}" "${STYLE_RESET}" "$*"
}

log_warn() {
    printf "  %s%s%s %s\n" "${STYLE_YELLOW_B}" "${STYLE_SYM_WARN}" "${STYLE_RESET}" "$*" >&2
}

log_error() {
    printf "  %s%s%s %s\n" "${STYLE_RED_B}" "${STYLE_SYM_ERR}" "${STYLE_RESET}" "$*" >&2
}

# log_dim <text> — secondary detail line, indented under a primary log entry.
log_dim() {
    printf "    %s%s%s\n" "${STYLE_DIM}" "$*" "${STYLE_RESET}"
}
