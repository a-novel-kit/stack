# Set up an agent

The workspace is built for AI agents. Its root holds the team's conventions as skills, which an agent loads when it starts there, and every repository sits below it. Open your agent with the stack root, `~/git-projects/a-novel` by default, as its workspace, not a single repository.

The team works with [Claude Code](https://code.claude.com/docs/en/overview) or [Codex](https://developers.openai.com/codex). Both need a paid plan.

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

For the desktop app, download it from [claude.ai/download](https://claude.ai/download).

### Open the workspace

In a terminal:

```bash
cd ~/git-projects/a-novel
claude
```

In the desktop app, open the **Code** tab and choose `~/git-projects/a-novel` as the folder.

Sign in on first launch. The team uses the most capable model and the Explanatory output style.

## Codex

### Install

Skip to [Open the workspace](#open-the-workspace-1) if this prints a version:

```bash
codex --version
```

:::tabs key:os
== Linux

```bash
curl -fsSL https://chatgpt.com/codex/install.sh | sh
```

== macOS

```bash
curl -fsSL https://chatgpt.com/codex/install.sh | sh
```

== Windows

```powershell
winget install --id OpenAI.Codex -e
```

:::

### Open the workspace

```bash
cd ~/git-projects/a-novel
codex
```

Sign in on first launch.

## Other apps and editors

Any other agent app or editor extension works the same way: open `~/git-projects/a-novel` as its project folder.
