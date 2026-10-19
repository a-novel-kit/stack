# a-novel stack

The backbone of A-Novel development: one workspace and one CLI for every project across the [`a-novel`](https://github.com/a-novel) and [`a-novel-kit`](https://github.com/a-novel-kit) organizations.

[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/agorastoryverse)](https://twitter.com/agorastoryverse)
[![Discord](https://img.shields.io/discord/1315240114691248138?logo=discord)](https://discord.gg/rp4Qr8cA)

<hr />

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/a-novel-kit/stack?filename=cli/go.mod)
![GitHub repo file or directory count](https://img.shields.io/github/directory-file-count/a-novel-kit/stack)
![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/a-novel-kit/stack)

![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/a-novel-kit/stack/main.yaml)

## What this is

A-Novel spans many repositories across two GitHub organizations: the brand's applications and business code, and their shared tooling. The **stack** brings them into one workspace, and its `a-novel` CLI works on all of them the same way: it keeps every repository in sync, tests and builds any of them, and runs services on your machine. The workspace is optimized for AI agents that know the team's conventions, and every tool stays usable by hand.

```
~/git-projects/a-novel/   the stack (github.com/a-novel-kit/stack)
├── cli/                  the a-novel CLI
├── app/                  a-novel repositories: services, platforms, infra
└── kit/                  a-novel-kit repositories: libraries, CI actions
```

## Install

The CLI runs on Linux, macOS, or WSL2 on Windows, and needs:

- [Git](https://git-scm.com)
- [GitHub CLI](https://cli.github.com)
- [Go](https://go.dev)
- [Node.js](https://nodejs.org) and [pnpm](https://pnpm.io)
- [Podman](https://podman.io) and [podman-compose](https://github.com/containers/podman-compose) 1.6.0 or newer
- An AI agent that reads `AGENTS.md`, such as [Claude Code](https://code.claude.com/docs/en/overview) or [Codex](https://learn.chatgpt.com/docs)
- For sysadmins: [Google Cloud CLI](https://cloud.google.com/sdk/docs/install), [OpenTofu](https://opentofu.org/docs/intro/install/) and [Bitwarden Secrets Manager CLI](https://bitwarden.com/help/secrets-manager-cli/)

Install the CLI:

```bash
go install github.com/a-novel-kit/stack/cli/cmd/a-novel@latest
```

Then set up your workspace:

```bash
a-novel core setup
a-novel core sync
```

Open your AI agent with the stack root as its workspace, and run `a-novel run ui` to see every service. The [developer guide](https://a-novel-kit.github.io/stack/) walks through each tool and the everyday work.

## Documentation

[`cli/README.md`](cli/README.md) is the CLI reference.

## Contributing

Set up your machine with the [developer guide](https://a-novel-kit.github.io/stack/), then read [CONTRIBUTING.md](./CONTRIBUTING.md).
