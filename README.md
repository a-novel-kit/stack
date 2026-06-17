# a-novel stack

`a-novel` is the single command-line tool for the Agora storyverse: it builds,
tests, runs and releases every project across the
[`a-novel`](https://github.com/a-novel) and
[`a-novel-kit`](https://github.com/a-novel-kit) organizations — one CLI in place
of the per-repo Makefiles and bash scripts.

This repository — the **stack** — hosts that CLI and anchors local checkouts of
the projects it operates on:

```
a-novel/                  this repo (github.com/a-novel-kit/stack)
├── cli/                  the a-novel CLI (Go)
├── app/                  a-novel org checkouts (services)   — gitignored
├── kit/                  a-novel-kit org checkouts (libraries) — gitignored
└── .secrets/             GitHub App private keys            — gitignored
```

`app/` and `kit/` are not submodules — they are plain clones managed by
`a-novel core sync`, kept out of this repo's history.

## Install

You need `git` and Go. The rest of the toolchain (Podman, Node + pnpm, `gh`) and
the per-OS install commands live in the
[developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md).

```bash
git clone git@github.com:a-novel-kit/stack.git ~/git-projects/a-novel
cd ~/git-projects/a-novel/cli
go install ./cmd/a-novel

a-novel core setup    # one-time bootstrap: env checks, state dirs, shell rc, daemon
a-novel core sync     # clone / fast-forward the workspace repos into app/ and kit/
```

`core setup` is idempotent. It installs a block in your shell rc (zsh, bash and
fish are supported) that auto-starts the background daemon and loads
tab-completion in every new shell.

Verify:

```bash
a-novel --version     # the binary resolves a build version
a-novel core status   # the daemon is up
a-novel run ps        # the daemon answers over its unix socket
```

After a `git pull` that touches `cli/`, run `a-novel install` to rebuild and
reinstall — it hands the daemon off without dropping running services.

## The UI — start here

For day-to-day work, open the terminal UI:

```bash
a-novel run ui
```

It is a full-screen dashboard: a services list on the left, a tabbed detail and
live log viewer on the right, and a command palette (press **Esc**) covering the
entire CLI surface — `:start`, `:kill`, `:infra-start`, and so on. Press **?**
for a searchable key and command reference; **q** quits.

The UI is a thin client over the same daemon as the command line, so anything
you do in it is visible to the CLI and vice-versa. If you learn one command,
learn this one.

## Non-interactive commands

Everything the UI does is also a discrete command — what you reach for in
scripts, in CI, or when you already know exactly what you want.

**Build and test** discover their targets in the working tree and show an
interactive picker by default; `-y` skips it and runs everything sequentially
(also the default when there is no TTY):

```bash
a-novel test -y                       # run every Go + pnpm test in the tree
a-novel build -y --type=podman        # build only the Podman images
```

**Run services.** The daemon auto-starts a target's dependencies — Postgres,
migrations — before the target itself:

```bash
a-novel run start service-json-keys/rest      # <service>/<target>
a-novel run logs  service-json-keys/rest --follow
a-novel run ps                                # what is running
a-novel run kill  service-json-keys/rest
```

**Release.** Cut a tag locally; CI's release workflow publishes from the pushed
tag:

```bash
a-novel publish version 0.21.0        # or: patch / minor / major
```

Run `a-novel <verb> --help` for any command's full flags. The complete
reference — command tree, daemon architecture, state directories, the compose
contract — is in [`cli/README.md`](cli/README.md).

> Lint, format and generate are deliberately **not** CLI verbs: each repo
> exposes them as uniform pnpm scripts (`pnpm lint:go`, `pnpm format:go`,
> `pnpm generate:go`, …).

## GitHub access

Two identities talk to GitHub from a dev machine: **you** (the operator, via
`gh`) and the **org bot** (a GitHub App, via `a-novel core bot-gh`). Anything
that authors or merges — PR creation, edits, ready/merge/close — runs as you.
The bot is strictly for comments, so automated notes attribute to `<app>[bot]`
rather than a human account.

```bash
gh auth login         # operator: pick GitHub.com + SSH
a-novel core bot-gh a-novel -- pr comment 123 --body "automated review note"
```

The CLI mints short-lived (1h) installation tokens from each org's App private
key — no long-lived bot token is ever stored. `bot-gh` hard-rejects every `gh`
subcommand that authors or state-changes a PR or issue, so those always run as
the operator and CI triggers and attribution stay correct.

<details>
<summary>One-time GitHub App setup (per org, then per machine)</summary>

Each org has a dedicated GitHub App (`anovelbot-agent` for `a-novel`,
`anovelkitbot-agent` for `a-novel-kit`). Both are already registered — these
steps are kept for new orgs or App re-creation.

**Per org.**
[Register a GitHub App](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/registering-a-github-app)
on the org with:

- **Repository permissions**: `Issues: Read and write`,
  `Pull requests: Read and write`, `Metadata: Read-only` — and nothing else, so
  the bot cannot push, merge or release.
- **Webhook**: disabled. The App is outbound-only.
- Install the App on **all repositories** of the org.

Record the **App ID** and **Installation ID** (the trailing number in
`https://github.com/organizations/<org>/settings/installations/<id>`) in
`botOrgs` in [`cli/internal/cli/bot_cmd.go`](cli/internal/cli/bot_cmd.go) — both
IDs are public; only the private key is sensitive.

**Per machine.** Generate a private key from the App's settings page (downloads
a `.pem`), then move it into the gitignored `.secrets/` directory under the
stack root:

```bash
mkdir -p ~/git-projects/a-novel/.secrets
mv ~/Downloads/<key>.pem ~/git-projects/a-novel/.secrets/anovelbot-agent.private-key.pem      # a-novel
mv ~/Downloads/<key>.pem ~/git-projects/a-novel/.secrets/anovelkitbot-agent.private-key.pem   # a-novel-kit
chmod 600 ~/git-projects/a-novel/.secrets/*.pem
```

Set `BOT_KEY_DIR` to keep the keys elsewhere. Verify (mints a real token, prints
nothing on success):

```bash
a-novel core bot-token a-novel     > /dev/null && echo "a-novel bot OK"
a-novel core bot-token a-novel-kit > /dev/null && echo "a-novel-kit bot OK"
```

</details>

## Security model

Releases, merges and pushes to `master` are human-only. `a-novel publish
version` refuses to run without a TTY, and the real boundary is server-side:
branch protection on `master`, tag protection on `v*`, and a comments-only bot
App. The org-wide security policy lives in
[a-novel/.github](https://github.com/a-novel/.github/blob/master/SECURITY.md).
