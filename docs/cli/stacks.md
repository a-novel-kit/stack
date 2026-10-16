# Work in parallel stacks

A stack is a full workspace. A second one lets you, or an agent, work on another task without touching your branches or running services.

## Create a stack

It lives in the temporary directory unless `stacks new --root` puts it elsewhere:

```bash
STACK_NAME=review
STACK_DIR="${TMPDIR:-/tmp}/a-novel-stacks/$STACK_NAME"
a-novel core stacks new "$STACK_NAME"
a-novel core sync --root="$STACK_DIR"
```

Register it with the daemon, keeping your default stack first:

```bash
export A_NOVEL_STACKS="${A_NOVEL_STACKS:-default:$HOME/git-projects/a-novel},$STACK_NAME:$STACK_DIR"
a-novel core restart --preserve-targets
```

Add the export to your shell profile to keep the stack beyond this shell. Point commands at it with `--stack`:

```bash
a-novel run start service-json-keys/rest --stack="$STACK_NAME"
```

## List and remove stacks

```bash
a-novel core stacks list
a-novel core stacks prune "$STACK_NAME" --dry-run
a-novel core stacks prune "$STACK_NAME"
```

`prune` frees the stack's files, containers and volumes. It refuses the default stack and any stack holding unpushed work. Drop its entry from `A_NOVEL_STACKS` afterwards.

## Run one command in a throwaway stack

```bash
a-novel --sandbox test -y
```

`--sandbox` goes first, and works for commands that need no daemon.
