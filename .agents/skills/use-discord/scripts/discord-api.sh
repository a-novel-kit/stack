#!/bin/bash

# Calls one Discord REST API path and prints the JSON response.
# Usage: discord-api.sh <api-path> [json-body | PUT | DELETE], relative to https://discord.com/api/v10/.
# A path alone reads. A body is posted, and PUT or DELETE send that method with an empty body, both
# with the A-Novel Writer token.

set -e

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
    printf "Usage: %s <api-path> [json-body | PUT | DELETE]\n" "$0" >&2
    exit 1
fi

BODY_ARGS=()
if [ $# -eq 2 ]; then
    case "$2" in
        PUT | DELETE) BODY_ARGS=(-X "$2") ;;
        *) BODY_ARGS=(-H 'Content-Type: application/json' --data "$2") ;;
    esac
fi

# Re-run under a-novel secrets, which puts the token in this process's environment only. Writing
# needs the Writer token. Reading takes the Reader token, or the Writer's, which reads too, when the
# developer holds only that one.
if [ -z "${DISCORD_TOKEN:-}" ]; then
    TOKEN_ID=DISCORD_WRITER_TOKEN
    if [ $# -eq 1 ] && a-novel secrets ls | grep -qx DISCORD_READER_TOKEN; then
        TOKEN_ID=DISCORD_READER_TOKEN
    fi
    exec a-novel secrets exec --env "DISCORD_TOKEN=$TOKEN_ID" -- bash "$0" "$@"
fi

# printf is a builtin and curl reads the header from stdin, so the token never reaches an argv.
printf 'Authorization: Bot %s\n' "$DISCORD_TOKEN" |
    curl -sS --fail-with-body -H @- "${BODY_ARGS[@]}" "https://discord.com/api/v10/$1"
