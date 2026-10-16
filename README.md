# a-novel stack

The `a-novel` CLI — one tool to build, test, run, and configure every project across the [`a-novel`](https://github.com/a-novel) and [`a-novel-kit`](https://github.com/a-novel-kit) organizations, in place of the per-repo Makefiles and scripts.

[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/agorastoryverse)](https://twitter.com/agorastoryverse)
[![Discord](https://img.shields.io/discord/1315240114691248138?logo=discord)](https://discord.gg/rp4Qr8cA)

<hr />

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/a-novel-kit/stack?filename=cli/go.mod)
![GitHub repo file or directory count](https://img.shields.io/github/directory-file-count/a-novel-kit/stack)
![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/a-novel-kit/stack)

![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/a-novel-kit/stack/main.yaml)

## What this is

This repository, the **stack**, hosts the `a-novel` CLI and anchors local checkouts of the projects it operates on. A background daemon started by the CLI runs services with their databases on your machine, and the same CLI applies every repository's GitHub settings from the templates kept here.

```
a-novel/                  this repo (github.com/a-novel-kit/stack)
├── cli/                  the a-novel CLI (Go)
├── docs/                 the developer guide
├── app/                  a-novel org checkouts (services, platforms, infra) — gitignored
└── kit/                  a-novel-kit org checkouts (libraries, CI actions)  — gitignored
```

## Install

The CLI needs these tools, on Linux, macOS, or WSL2 on Windows:

- [Git](https://git-scm.com)
- [GitHub CLI](https://cli.github.com)
- [Go](https://go.dev)
- [Node.js](https://nodejs.org) and [pnpm](https://pnpm.io)
- [Podman](https://podman.io) and [podman-compose](https://github.com/containers/podman-compose) 1.6.0 or newer
- [Bitwarden Secrets Manager CLI](https://bitwarden.com/help/secrets-manager-cli/), for maintainers

```bash
go install github.com/a-novel-kit/stack/cli/cmd/a-novel@latest
a-novel core setup    # one-time bootstrap: checks, state dirs, the stack clone, shell rc, daemon
a-novel core sync     # clone or fast-forward the workspace repos into app/ and kit/
```

## Documentation

The [developer guide](https://a-novel-kit.github.io/stack/) takes a fresh machine through each tool, per operating system, with the access to request from the team, then covers the CLI one runbook per task. Its source is in [`docs/`](docs/).

[`cli/README.md`](cli/README.md) is the CLI's design reference: the command tree, the daemon, its state directories, and the compose contract services follow.

## Contributing

New to the platform? Start with the [developer guide](https://a-novel-kit.github.io/stack/). To work on this repository, [CONTRIBUTING.md](CONTRIBUTING.md) covers its layout and how to add a command.
