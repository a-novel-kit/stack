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
└── kit/                  a-novel-kit org checkouts (libraries) — gitignored
```

`app/` and `kit/` are not submodules — they are plain clones managed by
`a-novel core sync`, kept out of this repo's history.

## Install

Building the CLI needs Go; `a-novel core setup` then preflights your environment
and requires `git` and a running Podman. The rest of the toolchain (Node + pnpm,
`gh`) and the per-OS install commands live in the
[developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md).

```bash
go install github.com/a-novel-kit/stack/cli/cmd/a-novel@latest

a-novel core setup    # one-time bootstrap: env checks, state dirs, shell rc, daemon
a-novel core sync     # clone / fast-forward the workspace repos into app/ and kit/
```

`go install` pulls and builds the binary straight from the module path — no
upfront clone. `core setup` then clones the stack repo itself into
`~/git-projects/a-novel` (the default stack) when it's missing, so you don't
bootstrap the workspace by hand. It is otherwise idempotent, and installs a block
in your shell rc (zsh, bash and fish are supported) that auto-starts the
background daemon and loads tab-completion in every new shell.

Verify:

```bash
a-novel --version     # the binary resolves a build version
a-novel core status   # the daemon is up
a-novel run ps        # the daemon answers over its unix socket
```

After a `git pull` that touches `cli/`, run `a-novel install` to rebuild and
reinstall — it hands the daemon off without dropping running services.

If a service needs an API secret (e.g. `OPENAI_API_KEY`), provision it once,
encrypted and locally:

```bash
a-novel secrets init                 # one-time: local key + store
a-novel secrets set openai-key       # reads the value with no echo
```

Secrets are AES-256-GCM-encrypted at rest, never printed, and injected into the
child env of `test`/`run`/`ui` via a value-free `.a-novel/secrets.yaml` mapping
in the service repo. See
[`cli/README.md`](https://github.com/a-novel-kit/stack/blob/master/cli/README.md#secrets)
for the full model.

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

Two identities talk to GitHub: **you** (the operator, via `gh`) and the **org
bot** (a GitHub App). Anything that authors or merges — PR creation, edits,
ready/merge/close — runs as you. The bot is strictly for comments, so automated
notes attribute to `<app>[bot]` rather than a human account.

The bot's signing keys never touch a dev machine. To comment as the bot you
trigger your org's dispatcher workflow with your **own** `gh` token; the
workflow holds the key and posts:

```bash
gh auth login   # operator: pick GitHub.com + SSH
a-novel core bot-comment a-novel service-authentication 123 \
    --body "automated review note"
```

`bot-comment` dispatches the
[`bot-comment`](.github/workflows/bot-comment.yaml) workflow in the **target
org's dispatcher repo** and waits for the run. The workflow mints a short-lived
token scoped to the single target repo (issues + pull-request writes only — no
`contents`, so it cannot push, merge or release), then posts a comment and
exits — it never authors or edits a PR/issue. So an operator
(or the agent) only needs `gh` plus `actions: write` on that dispatcher repo;
**no `.pem` is ever stored locally**, and a compromised machine cannot do
anything as the bot beyond ask for a comment. It handles top-level comments on
PRs **or** issues, and `--reply-to` replies inside a PR review thread.

There is **one dispatcher per org**, because the App key is an org-level secret
and org secrets do not cross org boundaries:

| Org           | Dispatcher repo     |
| ------------- | ------------------- |
| `a-novel-kit` | `a-novel-kit/stack` |
| `a-novel`     | `a-novel/.github`   |

<details>
<summary>One-time GitHub App setup (per org) + dispatcher secrets</summary>

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

The **App ID** is public and inlined in
[`bot-comment.yaml`](.github/workflows/bot-comment.yaml); no Installation ID is
needed — `create-github-app-token` resolves it from the org owner.

**Dispatcher secret (once per org — never on a dev machine).** Generate a
private key from each App's settings page (downloads a `.pem`) and store it as
an **organization-level Actions secret** named `AGENT_BOT_PRIVATE_KEY` in that
org (one in `a-novel` for `anovelbot-agent`, one in `a-novel-kit` for
`anovelkitbot-agent`). Make sure its repository visibility includes that org's
dispatcher repo. The workflow derives which key/App/owner to use from
`github.repository_owner`, so the two copies are identical.

Then grant each operator/agent `actions: write` on the dispatcher repo so they
can dispatch. Verify end-to-end by commenting on a scratch PR or issue:

```bash
a-novel core bot-comment a-novel <repo> <number> --body "bot test"
```

</details>

## Security model

Releases, merges and pushes to `master` are human-only. `a-novel publish
version` refuses to run without a TTY, and the real boundary is server-side:
branch protection on `master`, tag protection on `v*`, and a comments-only bot
whose signing keys live only in CI (never on a dev machine — see
[GitHub access](#github-access)), reached through a per-org dispatcher workflow
that can do nothing but post a comment. The org-wide security policy lives in
[a-novel/.github](https://github.com/a-novel/.github/blob/master/SECURITY.md).
