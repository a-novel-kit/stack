# Work in parallel stacks

A stack is one complete checkout of the workspace: the stack repository with its own `app/` and `kit/`. Your default stack is the one setup cloned. A second stack lets you, or an agent, work on a separate task without touching the branches and running services of the first. The daemon serves every registered stack at once.

## Create a stack

Name the stack, then clone and fill it. A new stack lands in the system's temporary directory, so one you forget expires on its own; `--root` on `stacks new` puts it somewhere durable, and the directory it prints then replaces `STACK_DIR`.

```bash
STACK_NAME=review
STACK_DIR="${TMPDIR:-/tmp}/a-novel-stacks/$STACK_NAME"
a-novel core stacks new "$STACK_NAME"
a-novel core sync --root="$STACK_DIR"
```

`A_NOVEL_STACKS` lists every stack the daemon serves, the default first. The export below keeps the stacks you already have, or names your default one when the variable was unset, and the restart hands the list to the daemon while starting again what was running:

```bash
export A_NOVEL_STACKS="${A_NOVEL_STACKS:-default:$HOME/git-projects/a-novel},$STACK_NAME:$STACK_DIR"
a-novel core restart --preserve-targets
```

The export lasts as long as this shell, which suits a scratch task; to keep the stack, put the same value in your shell profile.

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
