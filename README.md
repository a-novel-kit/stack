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

A-Novel spans many repositories across two GitHub organizations: the brand's applications and business code, Agora among them, and their shared tooling. The **stack** brings them into one workspace, and its `a-novel` CLI works on all of them the same way: it keeps every repository in sync, tests and builds any of them, and runs services on your machine. Day to day, an AI agent that knows the team's conventions drives it.

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
- An AI agent: [Claude Code](https://code.claude.com/docs/en/overview) or [Codex](https://developers.openai.com/codex)
- For sysadmins: [Google Cloud CLI](https://cloud.google.com/sdk/docs/install) and [Bitwarden Secrets Manager CLI](https://bitwarden.com/help/secrets-manager-cli/)

Install the CLI:

```bash
go install github.com/a-novel-kit/stack/cli/cmd/a-novel@latest
```

Then set up your workspace:

```bash
a-novel core setup
a-novel core sync
```

`a-novel claude` then opens Claude Code at the stack root, and `a-novel run ui` shows every service.

## Documentation

New to A-Novel? Start with the [developer guide](https://a-novel-kit.github.io/stack/). [`cli/README.md`](cli/README.md) is the CLI reference.

## Contributing

To work on this repository, [CONTRIBUTING.md](CONTRIBUTING.md) covers its layout and how the CLI works.
