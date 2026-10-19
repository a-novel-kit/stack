# a-novel

The command-line tool for developing A-Novel: one workspace, and the same commands to test, build and run every repository.

## Install

```bash
go install github.com/a-novel-kit/stack/cli/cmd/a-novel@latest
```

## Set up

```bash
a-novel core setup    # once: checks your tools, clones the stack, starts the background service
a-novel core sync     # clone every repository; run it again to bring them all up to date
```

The [developer guide](https://a-novel-kit.github.io/stack/) covers the tools the CLI needs, and its [runbooks](https://a-novel-kit.github.io/stack/cli/ui) walk through the everyday work by hand: running services, testing and building, secrets, backups and parallel stacks. An AI agent started at the stack root does that work for you.

## Usage

```text
a-novel <command> [flags]
a-novel <command> --help
```

`a-novel run ui` is the everyday view: every service, its logs, and a palette to start and stop them. A background service, the daemon, keeps your services running between commands.

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

Lint, format and code generation are pnpm scripts in every repository: `pnpm lint`, `pnpm format`, `pnpm generate:go`.

## Configuration

| Setting                 | Where               | Effect                                                                                                                                |
| ----------------------- | ------------------- | ------------------------------------------------------------------------------------------------------------------------------------- |
| `A_NOVEL_STACKS`        | Your shell profile  | The registered stacks, as comma-separated `name:path` pairs, the first one the default. Unset, one stack at `~/git-projects/a-novel`. |
| `workspace-repos.yaml`  | The stack's root    | The repositories `core sync` clones.                                                                                                  |
| `.a-novel/secrets.yaml` | A repository's root | The secrets the repository needs, as environment variables mapped to secret IDs. It holds no values.                                  |

## Files

| Path                              | Holds                                                |
| --------------------------------- | ---------------------------------------------------- |
| `~/.local/state/a-novel/logs/`    | Each target's current log and those of its last runs |
| `~/.local/share/a-novel/backups/` | Recent volume backups, the oldest pruned first       |
| `~/.local/share/a-novel/secrets/` | The encrypted secrets store and its key              |

The logs follow `XDG_STATE_HOME` and the other two `XDG_DATA_HOME` when those are set.

Every command documents its flags and examples in `a-novel <command> --help`. How the CLI works inside, and the conventions a service follows for it, are in [CONTRIBUTING.md](../CONTRIBUTING.md#how-the-cli-works).
