# AI agents (optional)

The stack repository ships the team's conventions as agent skills, under `.agents/skills/`. An agent started at the root of the stack picks them up with no setup, and every checkout under `app/` and `kit/` is in its reach, which makes it a strong pair programmer on this codebase. The team uses two agents; both need a paid plan.

## Claude Code

[Claude Code](https://code.claude.com/docs/en/overview) is Anthropic's agent. It reads the skills through the stack's `.claude/skills` link. It runs in the terminal and has [IDE extensions](https://code.claude.com/docs/en/ide-integrations) for VS Code and JetBrains. It needs a Claude Pro, Max, Team or Enterprise plan, or a Claude Console account.

### Install

Skip to [Configure](#configure) if this already prints a version:

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

On Linux and macOS, the installer puts `claude` in `~/.local/bin`. If the command is not found, add that directory to your `PATH`:

```bash
echo 'export PATH="$PATH:$HOME/.local/bin"' >> "$HOME/.$(basename "$SHELL")rc"
exec "$SHELL"
```

### Configure

Run `claude` and sign in on first launch. Three settings sharpen it on this codebase, set from inside a session:

| Setting      | Command                          | Value                                   |
| ------------ | -------------------------------- | --------------------------------------- |
| Model        | `/model`                         | The latest and most capable             |
| Output style | `/config`, then **Output style** | **Explanatory**                         |
| Effort       | `/effort xhigh`                  | `xhigh` by default, `max` for hard work |

Once the [CLI](/cli/) is set up, start it with `a-novel claude`: from any directory, it opens Claude Code at the root of the stack, where the skills load.

## Codex

[Codex](https://developers.openai.com/codex) is OpenAI's agent. It reads `.agents/skills/` directly, so start it from the stack root. It runs in the terminal and has [IDE extensions](https://developers.openai.com/codex/ide) for VS Code and its forks, JetBrains and Xcode. It needs a paid ChatGPT plan or an OpenAI API key.

### Install

Skip to [Configure](#configure-1) if this already prints a version:

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

Run `codex` and sign in on first launch.
