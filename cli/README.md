# a-novel

The command-line tool for developing Agora. It keeps one workspace holding every repository of the project in sync, and tests, builds and runs any of them with the same commands.

## Install

```bash
go install github.com/a-novel-kit/stack/cli/cmd/a-novel@latest
```

## Set up

```bash
a-novel core setup    # once: checks your tools, clones the stack, starts the background service
a-novel core sync     # clone every repository; run it again to bring them all up to date
```

Setup also adds a block to your shell profile, so every new shell starts the background service and completes `a-novel` commands. The [developer guide](https://a-novel-kit.github.io/stack/) lists the tools the CLI needs and how to install them.

## Usage

```text
a-novel <command> [flags]
a-novel <command> --help
```

| Command         | What it is for                                                                 |
| --------------- | ------------------------------------------------------------------------------ |
| `run ui`        | The everyday view: every service, its logs, and a palette to start and stop it |
| `run ...`       | Start, stop, restart and inspect services, read their logs, reach their data   |
| `test`          | Run a repository's tests, or the whole workspace's                             |
| `build`         | Build binaries, JavaScript bundles and container images                        |
| `secrets`       | Keep API keys encrypted on your machine and pass them to what needs them       |
| `core ...`      | The workspace and the background service: setup, sync, stacks, bot comments    |
| `repo ...`      | Apply repositories' GitHub settings, for administrators                        |
| `install`       | Rebuild the CLI from your stack checkout, keeping running services up          |
| `claude`        | Open Claude Code at the root of the stack, where the team's skills load        |
| `publish stamp` | Refresh version references in documentation, called by release scripts         |
| `version`       | Print the CLI's version                                                        |

The `run` commands go through a background service, which keeps your services running between commands and after you close the terminal. The other commands work on their own. Lint, format and code generation are not CLI commands: every repository exposes them as pnpm scripts, `pnpm lint`, `pnpm format` and `pnpm generate:go`.

## Workflows

The examples name a service and one of its targets, the programs under its `cmd/` directory. Set them once in your shell:

```bash
SERVICE=service-json-keys
TARGET=rest
```

### Start the day

Bring the stack and every repository up to date, rebuild the CLI when the stack's pull changed it, then open the UI:

```bash
git -C ~/git-projects/a-novel pull --ff-only
a-novel install       # only when that pull changed cli/
a-novel core sync
a-novel run ui
```

### Work on a service

Start a target: its database comes up and its migrations run first.

```bash
a-novel run start "$SERVICE/$TARGET"
```

Follow its logs while you edit, in the UI or in a second terminal with the same two variables set. The stream holds that terminal until Ctrl+C, which stops the stream, not the target:

```bash
a-novel run logs "$SERVICE/$TARGET" --follow
```

Restart the target to pick up a code change:

```bash
a-novel run restart "$SERVICE/$TARGET"
```

Ports change on every run, so ask the CLI for them:

```bash
eval "$(a-novel run env "$SERVICE")"
curl "$REST_URL/ping"
```

When you are done, stop the target; its database keeps running for next time.

```bash
a-novel run kill "$SERVICE/$TARGET"
```

### Check your work before pushing

From the repository you changed:

```bash
pnpm format                   # lint checks formatting, so fix it first
a-novel test -y
pnpm lint
a-novel build -y -t podman    # when the change touches what goes into an image
```

To try the target from its image rather than from source, restart it in container mode:

```bash
a-novel run restart "$SERVICE/$TARGET" --mode=container
```

### Reset a service's data

Clearing stops the service, backs its data up, then empties it; the next start recreates the database and reruns the migrations.

```bash
a-novel run volume clear "$SERVICE" --force
a-novel run start "$SERVICE/$TARGET"
```

`a-novel run volume restore "$SERVICE"` brings the backup back.

### Give a service an API key

A repository that needs a secret names it, by an id, in its `.a-novel/secrets.yaml`, and `test` or `run` warn with that id while it is missing. Store the value once, typed with no echo; from then on `test` and `run` pass it to the service, and no command prints it.

```bash
a-novel secrets init                  # once per machine
SECRET_ID=openai-key                  # the id the manifest names
a-novel secrets set "$SECRET_ID"
```

### Debug a target

For a target running from source, print the `dlv attach` command, then run it in another terminal:

```bash
a-novel run debug "$SERVICE/$TARGET"
```

### Work on two tasks at once

A second stack is a separate workspace with its own branches and services. Create and fill it:

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

The export lasts as long as this shell, which suits a scratch task; to keep the stack, put the same value in your shell profile. Then point commands at it:

```bash
a-novel run start "$SERVICE/$TARGET" --stack="$STACK_NAME"
```

Remove it, with its services and data, when the task is done:

```bash
a-novel core stacks prune "$STACK_NAME"
```

### Comment as the bot

Automated notes, such as an agent's review replies, post as the organization's bot rather than as you:

```bash
a-novel core bot-comment a-novel service-authentication 123 --body "Automated review note."
```

## Configuration

| Setting                 | Where               | Effect                                                                                                                                |
| ----------------------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `A_NOVEL_STACKS`        | Your shell profile  | The registered stacks, as comma-separated `name:path` pairs, the first one the default. Unset, one stack at `~/git-projects/a-novel`. |
| `workspace-repos.yaml`  | The stack's root    | The repositories `core sync` clones.                                                                                                  |
| `.a-novel/secrets.yaml` | A repository's root | The secrets the repository needs, as environment variables mapped to secret IDs. It holds no values.                                  |

## Files

| Path                              | Holds                                                    |
| --------------------------------- | -------------------------------------------------------- |
| `~/.local/state/a-novel/logs/`    | Each target's current log and its five previous runs     |
| `~/.local/share/a-novel/backups/` | Volume backups, five per volume, the oldest pruned first |
| `~/.local/share/a-novel/secrets/` | The encrypted secrets store and its key                  |

The logs follow `XDG_STATE_HOME` and the other two `XDG_DATA_HOME` when those are set.

## Command reference

```text
a-novel
├── test                              run Go and pnpm tests in the working tree
├── build                             build Go binaries, pnpm bundles, Podman images
├── secrets
│   ├── init                          create the local key and store
│   ├── set <id>                      store a value, typed with no echo
│   ├── ls / rm <id>                  list ids / delete a secret
│   └── exec --env NAME=<id> -- <cmd> run a command with secrets in its environment
├── run
│   ├── ui                            the terminal UI
│   ├── ps / stacks / topology        what runs, the stacks, the dependency graph
│   ├── service (status / infra)      one service, its infrastructure
│   ├── start / kill / restart        a target's lifecycle (go-exec or container)
│   ├── logs / env / watch            logs, environment, a stream of state changes
│   ├── volume (list / backup / restore / clear)   a service's data
│   └── exec / debug                  a command beside a target, a debugger
├── core
│   ├── setup / start / kill / restart / status    the background service
│   ├── sync                          clone or fast-forward the workspace repositories
│   ├── stacks (new / list / prune)   extra workspaces
│   └── bot-comment <org> <repo> <n>  comment as the organization bot
├── repo
│   ├── create <org> <name>           create a repository and apply its class settings
│   └── update                        reconcile a repository's settings (--all: every one)
├── install                           rebuild and reinstall the CLI, keeping services up
├── claude                            launch Claude Code from the stack root
├── publish stamp <prefix> <file>     refresh vX.Y.Z references in documentation
└── version                           print the CLI version
```

`--sandbox`, placed before a command that needs no background service, runs it in a freshly cloned temporary stack that is removed afterwards: `a-novel --sandbox test -y`.

Every command documents its flags and examples in `a-novel <command> --help`. How the CLI works inside, and the conventions a service follows for it, are in [CONTRIBUTING.md](../CONTRIBUTING.md#how-the-cli-works).
