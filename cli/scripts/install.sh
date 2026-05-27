#!/bin/bash

# install.sh — install the a-novel CLI with a graceful daemon handoff.
#
# Sequence (spec §3.6):
#   1. If a daemon is running, ask it to checkpoint its go-exec targets
#      and shut down (`a-novel core prepare-reinstall`). Containers
#      survive because they're independent of the daemon process.
#   2. Build + install the new binary via `go install ./cmd/a-novel`.
#   3. Bring the new daemon up (`a-novel core start`). It reads the
#      checkpoint and re-launches the recorded go-exec targets, then
#      deletes the checkpoint.
#
# End-state: ideally identical to pre-install (same containers, same
# go-exec targets running). Degraded states are explicit:
#   - go-exec target fails to relaunch → marked terminated/crashed.
#   - Checkpoint missing (daemon SIGKILL'd or PrepareReinstall not
#     called) → go-exec targets are simply gone; containers survive.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLI_DIR="$(dirname "$SCRIPT_DIR")"
cd "$CLI_DIR"

# Pre-install: ask the daemon to checkpoint if it's running. The
# command itself handles "daemon not running" silently — we only need
# to call it if the binary already exists on PATH.
if command -v a-novel >/dev/null 2>&1; then
    if a-novel core status >/dev/null 2>&1; then
        echo "▸ Checkpointing running daemon..."
        a-novel core prepare-reinstall
    fi
fi

# Build + install.
echo "▸ go install ./cmd/a-novel"
go install ./cmd/a-novel

# Post-install: bring the daemon back up. It will read the checkpoint
# if present and re-launch go-exec targets.
echo "▸ Starting new daemon..."
a-novel core start

echo "✓ install complete."
