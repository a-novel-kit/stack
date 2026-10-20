#!/bin/bash

# Fetches one Discord REST API path with the A-Novel Reader bot token and prints the JSON response.
# Usage: discord-api.sh <path>, relative to https://discord.com/api/v10/.

set -e

if [ $# -ne 1 ]; then
    printf "Usage: %s <api-path>\n" "$0" >&2
    exit 1
fi

# Re-run under a-novel secrets, which puts the token in this process's environment only.
if [ -z "${DISCORD_READER_TOKEN:-}" ]; then
    exec a-novel secrets exec --env DISCORD_READER_TOKEN=DISCORD_READER_TOKEN -- bash "$0" "$1"
fi

# printf is a builtin and curl reads the header from stdin, so the token never reaches an argv.
printf 'Authorization: Bot %s\n' "$DISCORD_READER_TOKEN" |
    curl -sS --fail-with-body -H @- "https://discord.com/api/v10/$1"
