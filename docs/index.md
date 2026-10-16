---
layout: home

hero:
  name: A-Novel developer guide
  text: From a fresh machine to a running stack
  tagline: Install the toolchain, set up the a-novel CLI, then build, test and run every A-Novel project with it.
  actions:
    - theme: brand
      text: Get started
      link: /dependencies/
    - theme: alt
      text: The a-novel CLI
      link: /cli/

features:
  - title: One workspace
    details: The stack repository anchors local checkouts of every repository in both GitHub organizations, cloned and kept current by one command.
  - title: One CLI
    details: a-novel builds, tests and runs every project the same way, in place of per-repository scripts and Makefiles.
  - title: Local services
    details: A background daemon starts services with their databases and migrations, allocates their ports and streams their logs.
---

## What this guide covers

A-Novel spans two GitHub organizations: [`a-novel`](https://github.com/a-novel) holds the Agora writing studio, its services and platforms; [`a-novel-kit`](https://github.com/a-novel-kit) holds the shared libraries, CI actions and tooling. One setup covers both.

The work happens through the `a-novel` CLI, which lives in the [stack repository](https://github.com/a-novel-kit/stack). It clones the repositories you work on, runs their tests and builds, and starts their services on your machine. Repository settings are code too: maintainers apply them with the same CLI.

The guide follows the order you set a machine up in:

1. **[Dependencies](/dependencies/)**: the tools the CLI and the repositories rely on, one page each, with the accounts and access to request from the team.
2. **[The a-novel CLI](/cli/)**: install it, clone the workspace, then one runbook per everyday task.

Questions go to the team on [Discord](https://discord.gg/rp4Qr8cA). Report a security issue privately, following the [security policy](https://github.com/a-novel-kit/.github/blob/master/SECURITY.md).
