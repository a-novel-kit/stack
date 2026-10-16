# Work in parallel stacks

A stack is one complete checkout of the workspace: the stack repository with its own `app/` and `kit/`. Your default stack is the one setup cloned. A second stack lets you, or an agent, work on a separate task without touching the branches and running services of the first. The daemon serves every registered stack at once.

## Create a stack

Name the stack, then clone and fill it:

```bash
STACK_NAME=review
a-novel core stacks new "$STACK_NAME"
```

The command prints the stack's directory and the entry to add to `A_NOVEL_STACKS`, the variable that registers stacks with the daemon as comma-separated `name:path` pairs, the first one the default. A new stack lands in the system's temporary directory unless `--root` names a durable place, so one you forget expires on its own.

Fill it with the repositories. Without `--root`, the stack's directory is the one below; otherwise use the directory the previous command printed:

```bash
STACK_DIR="${TMPDIR:-/tmp}/a-novel-stacks/$STACK_NAME"
a-novel core sync --root="$STACK_DIR"
```

Add the printed entry to `A_NOVEL_STACKS` in your shell profile, then restart the daemon so it serves the new stack. `--preserve-targets` starts again what was running:

```bash
a-novel core restart --preserve-targets
```

`run` commands take `--stack` to act on a stack other than the default:

```bash
a-novel run start service-json-keys/rest --stack="$STACK_NAME"
```

## List and remove stacks

```bash
a-novel core stacks list                          # every stack, its running services and volumes
a-novel core stacks prune "$STACK_NAME" --dry-run # what removal would reclaim
a-novel core stacks prune "$STACK_NAME"
```

A stack holds more than files: running containers keep ports, and volumes keep data. `prune` releases all three. It refuses the default stack, and a stack whose checkouts hold work that exists nowhere else: uncommitted changes, another branch, or unpushed commits. Volume backups survive the prune. Remove the stack's entry from `A_NOVEL_STACKS` afterwards; prune prints it.

## Run one command in a throwaway stack

`--sandbox` runs a single command in a fresh stack, cloned and synced for it, then deletes the stack, even when the command fails. It isolates the working tree from your checkouts, which suits a command that reads the whole workspace:

```bash
a-novel --sandbox test -y
```

It works for the commands that need no daemon, and comes first on the command line.
