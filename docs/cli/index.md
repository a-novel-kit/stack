# The a-novel CLI

The `a-novel` CLI is how you work on Agora day to day. It keeps your workspace of repositories in sync, runs any repository's tests and builds, and starts services on your machine with what they depend on. The services run under a background service, the daemon, so they outlive your terminal and the UI and the commands always agree on what runs.

It needs every tool from the [dependencies](/dependencies/), on Linux, macOS, or WSL2 on Windows.

## Install

Skip to [Set up](#set-up) if `a-novel --version` already answers; [Update](#update) brings an older build current.

`go install` builds the CLI straight from its module path, with no clone:

```bash
go install github.com/a-novel-kit/stack/cli/cmd/a-novel@latest
a-novel --version
```

## Set up

### Create the workspace

```bash
a-novel core setup
```

Setup checks your tools, clones the stack into `~/git-projects/a-novel` after asking, and starts the daemon. It also adds a block to your shell profile, so every new shell starts the daemon and completes `a-novel` commands.

Run it again whenever you like: on a machine already set up, it changes nothing, which also makes it a health check.

To put the workspace elsewhere, name the directory in `A_NOVEL_STACKS` for setup, which then records it in your shell profile:

```bash
STACK_DIR="$HOME/code/a-novel"
A_NOVEL_STACKS="default:$STACK_DIR" a-novel core setup
```

### Clone the repositories

```bash
a-novel core sync
```

Sync clones every repository listed in the stack's [`workspace-repos.yaml`](https://github.com/a-novel-kit/stack/blob/master/workspace-repos.yaml), in parallel, and fast-forwards the ones already there. It never switches branches or touches uncommitted work. Run it whenever you want the latest `master` everywhere.

```text
~/git-projects/a-novel/   the stack: a-novel-kit/stack
├── cli/                  the a-novel CLI
├── app/                  a-novel repositories: services, platforms, infra
└── kit/                  a-novel-kit repositories: libraries, CI actions
```

`app/` and `kit/` hold plain clones, ignored by the stack's own Git history.

### Check the installation

```bash
a-novel core status
a-novel run ps
```

`core status` reports the daemon running, with your stack; `run ps` lists the services of the workspace. If the daemon is down, `a-novel core start` brings it up; new shells start it on their own.

## Update

Pull the stack, then rebuild the CLI from it. The new binary takes over from the running daemon, and the services you started stay up:

```bash
git -C ~/git-projects/a-novel pull --ff-only
a-novel install
```

## Runbooks

| Runbook                                  | When you need it                                        |
| ---------------------------------------- | ------------------------------------------------------- |
| [Use the terminal UI](./ui)              | Every day: services, logs and commands on one screen    |
| [Run services](./services)               | Start a service with its database, read its logs        |
| [Test and build](./test-build)           | Run a repository's tests, build its binaries and images |
| [Manage secrets](./secrets)              | Give a service an API key without exposing it           |
| [Back up service data](./volumes)        | Save, restore or reset a service's database             |
| [Work in parallel stacks](./stacks)      | Keep a second workspace for a separate task             |
| [Comment as the bot](./bot-comments)     | Post automated notes on pull requests and issues        |
| [Configure repositories](./repositories) | Administrators: apply the repository settings           |

Every command documents its flags: `a-novel <command> --help`.
