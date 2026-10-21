---
layout: home

hero:
  name: A-Novel developer guide
  text: From a fresh machine to a running stack
  tagline: Set up your machine, then work on any A-Novel project, by hand or with an AI agent.
  actions:
    - theme: brand
      text: Get started
      link: /dependencies/
    - theme: alt
      text: Working with AI
      link: /ai/

features:
  - title: One workspace
    details: Every A-Novel repository, across both GitHub organizations, in one checkout that a single command keeps in sync.
    link: /cli/#set-up
    linkText: Set it up
  - title: Agent-ready
    details: Agents start with the team's conventions loaded and drive the CLI for you. Every tool stays usable by hand.
    link: /ai/
    linkText: Work with an agent
  - title: Local services
    details: Start any service with everything it depends on, then follow it live from one terminal view.
    link: /cli/services
    linkText: Run services
---

## Start here

A-Novel spans two GitHub organizations. [`a-novel`](https://github.com/a-novel) holds the applications and business code of the A-Novel brand. [`a-novel-kit`](https://github.com/a-novel-kit) holds the shared libraries and tooling. The [stack repository](https://github.com/a-novel-kit/stack) gathers them into one workspace, with the `a-novel` CLI to work across it.

[Start your contributor setup](/dependencies/).

Ask questions on [Discord](https://discord.gg/rp4Qr8cA). Report security issues privately, through the [security policy](https://github.com/a-novel-kit/.github/blob/master/SECURITY.md).

**Sysadmins** add [their own setup](/sysadmins/) on top.
