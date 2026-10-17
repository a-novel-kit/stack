# The a-novel CLI

The `a-novel` CLI is the everyday tool: it syncs the workspace, tests and builds any repository, and runs services on your machine. A background daemon keeps those services running after you close the terminal.

It needs the [dependencies](/dependencies/), on Linux, macOS or WSL2.

## Install

Skip to [Set up](#set-up) if `a-novel --version` answers.

```bash
go install github.com/a-novel-kit/stack/cli/cmd/a-novel@latest
```

## Set up

```bash
a-novel core setup
a-novel core sync
```

Setup clones the stack into `~/git-projects/a-novel`, starts the daemon, and makes every new shell start it too. Sync clones the repositories, and brings them up to date whenever you rerun it.

To put the workspace elsewhere, run setup this way instead:

```bash
STACK_DIR="$HOME/code/a-novel"
A_NOVEL_STACKS="default:$STACK_DIR" a-novel core setup
```

```text
~/git-projects/a-novel/   the stack: a-novel-kit/stack
├── cli/                  the a-novel CLI
├── app/                  a-novel repositories: services, platforms, infra
└── kit/                  a-novel-kit repositories: libraries, CI actions
```

Check that the daemon runs and sees the workspace:

```bash
a-novel core status
a-novel run ps
```

## Update

Pull the stack and rebuild; running services stay up:

```bash
git -C ~/git-projects/a-novel pull --ff-only
a-novel install
```

## Next

Next, [set up an AI agent](/ai/setup), or work by hand with the [runbooks](/cli/ui).
