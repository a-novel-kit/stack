# a-novel stack

The base-of-operations workspace for developing the Agora storyverse. This
repository hosts the [`a-novel` CLI](cli/README.md) — the single entrypoint
for building, testing, running and releasing every project across the
[`a-novel`](https://github.com/a-novel) and
[`a-novel-kit`](https://github.com/a-novel-kit) organizations — and anchors
local checkouts of those projects:

```
a-novel/                  this repo (github.com/a-novel-kit/stack)
├── cli/                  the a-novel CLI (Go) — see cli/README.md
├── app/                  a-novel org checkouts (services) — gitignored
├── kit/                  a-novel-kit org checkouts (libraries) — gitignored
└── .secrets/             GitHub App private keys — gitignored
```

`app/` and `kit/` are not submodules: they are plain clones managed by
`a-novel core sync`, kept out of this repo's history.

## Prerequisites

`git`, Go (per [`cli/go.mod`](cli/go.mod)), Podman + podman-compose,
Node.js + pnpm (per [`package.json`](package.json)), the GitHub CLI (`gh`),
and SSH access to GitHub.

Per-OS install commands (macOS / Ubuntu / Arch) live in the
[developer onboarding guide](https://github.com/a-novel-kit/.github/blob/master/README.md).

## Install the CLI

```bash
git clone git@github.com:a-novel-kit/stack.git ~/git-projects/a-novel
cd ~/git-projects/a-novel/cli
go install ./cmd/a-novel

a-novel core setup    # one-time bootstrap: env checks, state dirs, shell rc, daemon
a-novel core sync     # clone / fast-forward the workspace repos into app/ and kit/
```

`core setup` is idempotent — re-running it on a configured machine is a
no-op. It installs a marker-delimited block in your shell rc (zsh, bash and
fish supported) that auto-starts the daemon and loads tab-completion in
every new shell.

Verify:

```bash
a-novel --version     # binary resolves a build version
a-novel core status   # daemon is up, stacks are listed
a-novel run ps        # daemon answers over the unix socket
```

Update (after a `git pull` that touches `cli/`):

```bash
git -C ~/git-projects/a-novel pull
a-novel install       # rebuild + reinstall + state-preserving daemon restart
```

## Use the CLI

```
a-novel
├── test          run Go + pnpm tests discovered in the working tree
├── build         build Go binaries, pnpm bundles, Podman images
├── publish       cut a release (bump, commit, tag vX.Y.Z, push) — interactive-only
├── core          daemon lifecycle, workspace sync, bot tokens
└── run           operate services: start/kill, logs, env, volumes, TUI
```

Day-to-day:

```bash
a-novel test -y                          # run everything, no prompt
a-novel build --type=podman              # validate Dockerfile changes
a-novel run start <service>/<target>     # start a target (deps auto-cascade)
a-novel run logs <service>/<target> --follow
a-novel run ui                           # full-screen TUI
```

Every subcommand ships full help text: `a-novel <verb> --help`. The complete
reference — command tree, architecture, state directories, compose contract —
is in [`cli/README.md`](cli/README.md).

Lint, format and generate are deliberately **not** CLI verbs: each repo
exposes them as uniform pnpm scripts (`pnpm lint:go`, `pnpm format:go`,
`pnpm generate:go`, …).

## GitHub access

Two identities talk to GitHub from a dev machine: **you** (the operator,
via `gh`) and the **org bot** (a GitHub App, via `a-novel core bot-gh`).
Anything that authors or merges — PR creation, edits, ready/merge/close —
runs as you. The bot is strictly for comments, so automated review notes
attribute to `<app>[bot]` instead of a human account.

### Operator: the GitHub CLI

Install `gh` ([install docs](https://github.com/cli/cli#installation)),
then authenticate and verify:

```bash
gh auth login         # pick GitHub.com + SSH
gh auth status        # confirms the token and its scopes
```

### Bot: GitHub App setup

Each org has a dedicated GitHub App (`anovelbot-agent` for `a-novel`,
`anovelkitbot-agent` for `a-novel-kit`). The CLI mints short-lived (1h)
installation tokens from the App's private key — no long-lived bot token is
ever stored.

**One-time, per org** (already done for both orgs — kept here for new orgs
or App re-creation). [Register a GitHub App](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/registering-a-github-app)
on the org with:

- **Repository permissions**: `Issues: Read and write`,
  `Pull requests: Read and write`, `Metadata: Read-only` (mandatory).
  Nothing else — the bot must not be able to push, merge or release.
- **Webhook**: disabled. The App is outbound-only.
- Install the App on **all repositories** of the org.

Record the **App ID** (App settings page) and **Installation ID** (the
trailing number in the installation URL,
`https://github.com/organizations/<org>/settings/installations/<id>`) in
`botOrgs` in [`cli/internal/cli/bot_cmd.go`](cli/internal/cli/bot_cmd.go) —
both IDs are public; only the private key is sensitive.

**Per machine.** From the App's settings page, generate a private key (this
downloads a `.pem`), then move it into the gitignored `.secrets/` directory
under the stack root:

```bash
mkdir -p ~/git-projects/a-novel/.secrets
mv ~/Downloads/<downloaded-key>.pem \
   ~/git-projects/a-novel/.secrets/anovelbot-agent.private-key.pem      # a-novel
mv ~/Downloads/<downloaded-key>.pem \
   ~/git-projects/a-novel/.secrets/anovelkitbot-agent.private-key.pem   # a-novel-kit
chmod 600 ~/git-projects/a-novel/.secrets/*.pem
```

Set `BOT_KEY_DIR` to keep the keys somewhere else.

Verify (mints a real token, prints nothing on success):

```bash
a-novel core bot-token a-novel     > /dev/null && echo "a-novel bot OK"
a-novel core bot-token a-novel-kit > /dev/null && echo "a-novel-kit bot OK"
```

Use:

```bash
a-novel core bot-gh a-novel -- pr comment 123 --body "automated review note"
```

`bot-gh` hard-rejects every `gh` subcommand that authors or state-changes a
PR/issue (`pr create`, `pr merge`, `issue close`, …) — those must run as the
operator so CI triggers and attribution stay correct.

## Security model

Releases, merges and pushes to `master` are human-only. `a-novel publish
version` refuses to run without a TTY, and the real boundary is server-side:
branch protection on `master`, tag protection on `v*`, and a comments-only
bot App. The org-wide security policy lives in
[a-novel/.github](https://github.com/a-novel/.github/blob/master/SECURITY.md).
