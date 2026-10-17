# Set up an agent

The workspace is built for AI agents. Its root holds the team's conventions: `AGENTS.md`, and the skills in `.agents/skills`. An agent loads them when the stack root, `~/git-projects/a-novel` by default, is its workspace. Each repository below it is a separate checkout, so an agent started inside one misses them.

The team works mostly with Claude Code and Codex. Any agent that reads `AGENTS.md` and Agent Skills works too, such as the others below.

## Claude Code

### Install

For the terminal, skip to [Open the workspace](#open-the-workspace) if this prints a version:

```bash
claude --version
```

:::tabs key:os
== Linux

```bash
curl -fsSL https://claude.ai/install.sh | bash
```

== macOS

```bash
curl -fsSL https://claude.ai/install.sh | bash
```

== Windows

```powershell
winget install --id Anthropic.ClaudeCode -e
```

:::

The [desktop app](https://claude.ai/download) includes Claude Code. The [VS Code](https://code.claude.com/docs/en/vs-code) and [JetBrains](https://code.claude.com/docs/en/jetbrains) extensions install from their marketplaces.

### Open the workspace

In a terminal, start it from the stack root:

```bash
cd ~/git-projects/a-novel
claude
```

In the desktop app's **Code** tab, select **Local**, then **Select folder**, and choose the stack root. In VS Code or JetBrains, open the stack root as the project.

Sign in on first launch. The team uses the most capable model and the Explanatory output style.

## Codex

### Install

For the terminal, skip to [Open the workspace](#open-the-workspace-1) if this prints a version:

```bash
codex --version
```

:::tabs key:shell
== Linux / macOS

```bash
curl -fsSL https://chatgpt.com/codex/install.sh | sh
```

== Windows

```powershell
irm https://chatgpt.com/codex/install.ps1 | iex
```

:::

The [Codex app](https://learn.chatgpt.com/docs/app) and the [IDE extension](https://learn.chatgpt.com/docs/codex/ide) install from their own pages.

### Open the workspace

In a terminal:

```bash
codex --cd ~/git-projects/a-novel
```

In the app, open the stack root as a project. Codex reads `AGENTS.md` and skills from the project's primary folder only. If you add folders, keep the stack root primary with **Edit project** → **Make primary**. In the IDE extension, open the stack root as the project.

Sign in on first launch.

## GitHub Copilot

In VS Code, open the stack root with **File** → **Open Folder**. Copilot's agent reads `AGENTS.md` and the skills from there.

For the terminal, install the [Copilot CLI](https://docs.github.com/en/copilot/how-tos/set-up/install-copilot-cli), start it from the stack root, and trust the folder when it asks:

```bash
npm install -g @github/copilot
cd ~/git-projects/a-novel
copilot
```

## Cursor

[Download Cursor](https://cursor.com/download), then open the stack root with **File** → **Open Folder**. Its agent reads `AGENTS.md` and the skills from there.

## Antigravity CLI

Google's terminal agent, which replaced Gemini CLI for individual accounts.

:::tabs key:shell
== Linux / macOS

```bash
curl -fsSL https://antigravity.google/cli/install.sh | bash
```

== Windows

```powershell
irm https://antigravity.google/cli/install.ps1 | iex
```

:::

Start it from the stack root, and confirm the workspace trust prompt:

```bash
cd ~/git-projects/a-novel
agy
```

## OpenCode

An open-source terminal agent that works with your own model provider:

```bash
npm install -g opencode-ai
opencode ~/git-projects/a-novel
```

## Other agents

Any other agent that reads `AGENTS.md` and Agent Skills works the same way: open the stack root as its workspace.
