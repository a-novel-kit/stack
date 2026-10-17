# The developer guide

Read this reference when routed here by [write-project-docs](../SKILL.md): it governs the developer
guide, the VitePress site in the stack's `docs/`, published at <https://a-novel-kit.github.io/stack/>.
The [editorial principles](editorial.md) apply to its prose, as to any guide.

Contents: [Shape](#shape), [Prose](#prose), [Commands](#commands), [Tabs](#tabs), [Verify](#verify).

## Shape

The site runs in the order a contributor works: **Dependencies** (one page per tool, Discord first),
**a-novel CLI** (installation), **Working with AI** (agent setup, then working through an agent),
**Runbooks** (the same work by hand) and **Sysadmins**. AI comes before the runbooks because the
workspace is optimized for agents. Agents are a tool, not a requirement: never claim most work goes
through them, and give each supported agent its own way to open the stack root rather than one
provider's launcher, taken from that vendor's docs for each surface it offers. Leave to the skills what they already teach agents, such as running sessions
side by side: the guide covers what the human operator does. The sidebar in
`docs/.vitepress/config.ts` lists every page and the top bar has one entry per group, so a new page
needs its sidebar entry under the right group.

Access has two levels, and the structure carries them. Everything outside **Sysadmins** is for
contributors and is theirs to do; the one thing the team grants them, enrollment, lives on the
Discord page with what it brings (the member role for the private working channels, GitHub access).
**Sysadmins** holds the admin side: stricter security requirements, extra tools (Google Cloud CLI,
Bitwarden), and duties such as enrolling contributors and applying repository settings. Maintainers
get no section: their access widens the contributor setup without changing it. Write the
contributor path without exceptions: a sysadmin difference gets at most a one-line note linking to
the Sysadmins section, which holds the stricter setup (the security key, for one).

A tool's page opens with its role in the team's work, never its current feature set: a list of what
Discord posts, which plans an agent accepts or which keys the UI binds goes stale, and the tool's own
help shows it. `## Install` opens with the version check and a link that skips to `## Configure`;
every step inside Configure a reader may have done before opens with its own check. Never repeat
what the sidebar or a page already shows, such as a table of pages or of their checks.

## Prose

Write assertive, short sentences, one idea each, and trust the reader with what the page shows:

- State the step: "Accept the server invite", not "Joining needs the browser or the app: open the
  invite and accept it". A sentence that chains clauses with colons and semicolons splits in two.
- Never describe the page's own structure ("every page has an Install and a Configure section"); a
  reader sees it by opening one. A call to action names the reader's goal ("Start your contributor
  setup"), not the route through the site.
- Say a fact once. A code block shows what a command does; the prose adds only what the block
  cannot, such as why or when.
- Guide where the reader cannot guess: where to look and whom to ask, then what comes out of it.
  Name the exact menus and labels where a value hides, in bold and joined with →, as for a Discord
  user ID or GitHub's private email address.

## Commands

- Give a command for every step, and say so when a step needs the browser or an app instead (creating
  a token, accepting an invitation, a Discord setting).
- Set a block's inputs as variables on its first lines, so the reader edits those and runs the rest as
  it is. Read a secret with `read -rs` or `Read-Host -MaskInput`, never as an argument, so it stays out
  of the terminal and the shell history. A procedure repeated per item, such as each security key,
  sets its variables in a step of its own and ends by saying where to repeat from.
- Give each step one action: creating a key and registering it on GitHub are two steps, each with its
  own check.
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

Before publishing, read every changed page top to bottom as a newcomer would. Each heading matches
its section, each tip agrees with the step beside it, each pronoun points back to something already
shown, and each step a reader may have done opens with its check.
