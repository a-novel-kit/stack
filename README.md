# a-novel stack

The backbone of Agora development: one workspace and one CLI for every project across the [`a-novel`](https://github.com/a-novel) and [`a-novel-kit`](https://github.com/a-novel-kit) organizations.

[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/agorastoryverse)](https://twitter.com/agorastoryverse)
[![Discord](https://img.shields.io/discord/1315240114691248138?logo=discord)](https://discord.gg/rp4Qr8cA)

<hr />

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/a-novel-kit/stack?filename=cli/go.mod)
![GitHub repo file or directory count](https://img.shields.io/github/directory-file-count/a-novel-kit/stack)
![GitHub code size in bytes](https://img.shields.io/github/languages/code-size/a-novel-kit/stack)

![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/a-novel-kit/stack/main.yaml)

## What this is

Agora is built from many repositories spread over two GitHub organizations. The **stack** is the central place they are developed and kept in sync from: one checkout holds all of them, and one tool, the `a-novel` CLI, works on every one the same way.

The CLI clones the whole project into your workspace and keeps it current. It runs any repository's tests and builds, and starts its services on your machine with everything they depend on, so the same few commands cover a backend service, a frontend platform or a shared library.

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
- [Bitwarden Secrets Manager CLI](https://bitwarden.com/help/secrets-manager-cli/), for maintainers

Install the CLI:

```bash
go install github.com/a-novel-kit/stack/cli/cmd/a-novel@latest
```

Then set up your workspace, once: `core setup` clones the stack and starts the CLI's background service, and `core sync` clones every repository into it.

```bash
a-novel core setup
a-novel core sync
```

From there, `a-novel run ui` shows every service, ready to start.

## Documentation

New to Agora? The [developer guide](https://a-novel-kit.github.io/stack/) sets up your machine as a contributor, from the tools to the access you need from the team, then walks through the everyday work.

[`cli/README.md`](cli/README.md) is the CLI reference: its commands and the workflows they serve.

## Contributing

New to the platform? Start with the [developer guide](https://a-novel-kit.github.io/stack/). To work on this repository, [CONTRIBUTING.md](CONTRIBUTING.md) covers its layout and how the CLI works.
