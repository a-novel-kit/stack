# Set up an agent

The team works with [Claude Code](https://code.claude.com/docs/en/overview) or [Codex](https://developers.openai.com/codex). Both run in the terminal and in IDEs, and both need a paid plan.

## Claude Code

### Install

Skip to [Configure](#configure) if this prints a version:

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

### Configure

Sign in on first launch. The team uses the most capable model and the Explanatory output style.

## Codex

### Install

Skip to [Configure](#configure-1) if this prints a version:

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

### Configure

Sign in on first launch.
