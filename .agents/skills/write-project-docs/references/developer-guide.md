# The developer guide

Read this reference when routed here by [write-project-docs](../SKILL.md): it governs the developer
guide, the VitePress site in the stack's `docs/`, published at <https://a-novel-kit.github.io/stack/>.
The [editorial principles](editorial.md) apply to its prose, as to any guide.

## Shape

The site runs in setup order: the home page says what the stack does, **Dependencies** has one page
per tool in install order, and **a-novel CLI** opens with its installation, then one runbook per task.
The sidebar in `docs/.vitepress/config.ts` lists every page, so a new page needs its entry there: a
new runbook goes in the CLI group's Runbooks sub-group, which the top bar's Runbooks entry also covers.

A dependency page opens with what the tool does here and its version floor, then holds two sections.
Describe a tool's role in the team's work, never its current feature set: a list of what Discord
posts, which plans an agent accepts or which keys the UI binds goes stale, and the tool's own help
shows it anyway. The same holds in the runbooks, which name the everyday flags and leave the rest to
`--help`.
`## Install` opens with the tool's version check and a link that skips to `## Configure`, then holds
only the install commands, so a reader who already has the tool runs one command. Every step inside
Configure that a reader may have done before opens with its own check and skip link, and a new tool
adds a row to the overview's "Check what you already have" table. Access the team grants gets its own section naming what to ask for, whom
to ask and what to send, and a row in the overview's "What to ask the team for" table.

## Commands

- Give a command for every step, and say so when a step needs the browser or an app instead (creating
  a token, accepting an invitation, a Discord setting).
- Set a block's inputs as variables on its first lines, so the reader edits those and runs the rest as
  it is. Read a secret with `read -rs` or `Read-Host -MaskInput`, never as an argument, so it stays out
  of the terminal and the shell history.
- Shell profile edits target `$HOME/.$(basename "$SHELL")rc`, which covers zsh and bash.
- Use the distribution's package when its version meets the floor, checked on
  [Repology](https://repology.org); otherwise the vendor's own channel, with one line saying why.
- A pinned version in a command needs a Renovate manager in the stack's `renovate.json`, as the nvm
  installer URL has.
- Keep a code line within about 78 characters where the command allows: the content column shows no
  more, and a trailing comment past it hides behind a scroll. Shorten the comment or move it into
  the prose; a long URL or command can stay, since the copy button takes the whole line.

## Tabs

`vitepress-plugin-tabs` remembers a reader's choice per key in the browser, across pages, matching
tabs by label. A label spelled differently in one group breaks that group's sync, and a group lacking
the stored label shows its first tab, so reuse these sets exactly:

| Key      | Labels                                          | Use                                       |
| -------- | ----------------------------------------------- | ----------------------------------------- |
| `os`     | `Linux`, `macOS`, `Windows`                     | Commands that differ per system           |
| `distro` | `Debian / Ubuntu`, `Fedora`, `openSUSE`, `Arch` | Inside `Linux`, when distributions differ |
| `shell`  | `Linux / macOS`, `Windows`                      | Commands that differ only by shell        |
| `sshkey` | `Security key`, `Key file`                      | The two SSH key kinds                     |

A nested group's parent takes one more colon and one more equals sign:

```md
::::tabs key:os
=== Linux
:::tabs key:distro
== Fedora
...
:::
=== macOS
...
::::
```

Keep VitePress containers such as `::: tip` outside tab groups, since their closing `:::` can close
a group instead. Duplicate a block across two tabs rather than merge their labels.

## Verify

`pnpm docs:dev` serves the site with live reload. `pnpm docs:build` fails on a dead internal link,
and CI's `build-docs` job runs it on every branch; `publish-docs` deploys `master`'s build.
