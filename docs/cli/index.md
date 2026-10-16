# The a-novel CLI

The `a-novel` CLI builds, tests and runs every A-Novel project, and configures their GitHub repositories. A background daemon, one per user, supervises the services you start, allocates their ports and keeps their logs. The terminal UI and the commands are two views of that same daemon: what one starts, the other sees.

It needs every tool from the [dependencies](/dependencies/), on Linux, macOS, or WSL2 on Windows.

## Install

`go install` builds the CLI straight from its module path, with no clone:

```bash
go install github.com/a-novel-kit/stack/cli/cmd/a-novel@latest
a-novel --version
```

## Configure

### Set up the workspace

```bash
a-novel core setup
```

Setup checks Podman, Git and your SSH access to GitHub, then:

1. creates the CLI's state directories;
2. clones the stack repository into `~/git-projects/a-novel`, after asking;
3. adds a block to your shell profile that starts the daemon and loads tab completion in every new shell;
4. starts the daemon.

Run it again whenever you like: on a machine already set up, it changes nothing.

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
| [Configure repositories](./repositories) | Maintainers: apply the repository settings              |

Every command documents its flags: `a-novel <command> --help`.
