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
    details: Every repository of Agora, across both GitHub organizations, in one checkout that a single command keeps in sync.
  - title: One CLI
    details: The same commands test, build and run a backend service, a frontend platform or a shared library.
  - title: Agora on your machine
    details: Start any service with everything it depends on, then follow it live from one terminal view.
---

## What this guide covers

Agora is built from many repositories across two GitHub organizations: [`a-novel`](https://github.com/a-novel) holds the writing studio, its services and platforms, and [`a-novel-kit`](https://github.com/a-novel-kit) the shared libraries, CI actions and tooling. The [stack repository](https://github.com/a-novel-kit/stack) is the central place they are all developed and kept in sync from, through one tool, the `a-novel` CLI.

This guide sets your machine up for that work, then shows the work itself.

The guide follows the order you set a machine up in:

1. **[Dependencies](/dependencies/)**: the tools the CLI and the repositories rely on, one page each, with the accounts and access to request from the team.
2. **[The a-novel CLI](/cli/)**: install it, clone the workspace, then one runbook per everyday task.

Questions go to the team on [Discord](https://discord.gg/rp4Qr8cA). Report a security issue privately, following the [security policy](https://github.com/a-novel-kit/.github/blob/master/SECURITY.md).
