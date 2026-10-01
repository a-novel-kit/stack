# Daemon and workspace operations

Read this reference when routed here by [use-a-novel-cli](../SKILL.md). Its rules apply to the selected work.

## `a-novel core` — daemon lifecycle + workspace tooling

```bash
a-novel core setup            # one-time interactive bootstrap (run once after install)
a-novel core start            # idempotent + silent if already running (lives in .zshrc)
a-novel core restart          # stop then start (use --preserve-targets for checkpoint replay)
a-novel core status           # is it running? what stacks? checkpoint pending?
a-novel core kill [--force]   # graceful shutdown (--force also tears down infra)
a-novel core prepare-reinstall  # used by `a-novel install` — checkpoints + exits

# Workspace tooling (ported from the old sync / bot-token bash scripts, now deleted).
a-novel core sync                          # clone/ff-pull the curated workspace whitelist
a-novel core sync --allow=a-novel-kit/golib  # subset to specific repos
a-novel core sync --ignore=<org>/<repo>      # skip specific repos
a-novel core bot-comment <org> <repo> <number> --body <text> [--reply-to <id>]
                                           # comment as the org App bot (see below)

# Stack lifecycle — allocate, audit, give back.
a-novel core stacks new <name>        # clone a fresh stack under the OS temp dir
a-novel core stacks new <name> --root=<path>  # ...or somewhere durable
a-novel core stacks list              # every stack: path, targets up, infra up, volumes
a-novel core stacks prune <name>      # kill its targets + infra, clear its volumes, remove its files
a-novel core stacks prune <name> --dry-run    # report what would be reclaimed
a-novel core stacks prune <name> --purge-backups  # also delete its volume backups
a-novel core stacks prune --all -y    # sweep every stack but the default
```

**Pruning a scratch stack.** A stack allocates three things and only one is a
file, so deleting the root reclaims the checkout but leaves containers holding
host ports and volumes in the container store. `stacks prune` releases all three,
in that order.

It refuses the default stack — that is the workspace, not scratch space — and
`--all` sweeps every _other_ registered stack, the pass to run after a batch of
agent sessions. It also refuses a stack whose checkouts hold work that exists
nowhere else (dirty tree, a non-default branch, unpushed commits) unless `--force`.
`$A_NOVEL_STACKS` lives in your shell config, so prune prints the entry to drop
instead of editing the file under you.

Volume backups survive: `ClearVolume` takes one on the way past, so the artefact
that undoes a prune outlives it. `--purge-backups` deletes them too.

**Where a new stack lives.** `stacks new` defaults to `<os temp dir>/a-novel-stacks/<name>`
via Go's `os.TempDir()`, which honours `$TMPDIR` — a per-user `/var/folders/…/T`
on macOS, `/tmp` on Linux. The OS reclaims both, so a stack nobody prunes expires
instead of accumulating. Pass `--root` for somewhere durable.

Because that home is swept, a registration can outlive its files. The daemon
skips such a stack rather than refusing to start over it, and `stacks list`
flags it (`files are gone — drop it from A_NOVEL_STACKS`) so the stale entry
stays visible.

`bot-comment` is the **only** way to post a PR/issue/review comment as
`<app-slug>[bot]`. It mints no local token: it triggers the centralized
`bot-comment` workflow in `a-novel-kit/stack` with your own `gh` token, and
that workflow (which alone holds the App keys) posts the comment and is watched
to completion. No `.pem` ever lives on a dev machine; you need only `gh` +
`actions:write` on the dispatcher repo. The bot can only comment — PR
authoring/merge/close are impossible through it.

`core setup` is interactive; everything else is non-interactive and `.zshrc`-safe.

**Sub-agents spawning fresh stacks**: run `a-novel core sync --root=<new-stack-root>`
as the first action in the new workspace, so later test/build/run commands have
something to operate on.

**`workspace-repos.yaml` at the workspace root is the whitelist** — the single
source of truth for which repos exist locally, read at runtime by both
`core sync` and `repo update --all`. Add a repo by editing that file; no rebuild,
no code change. Do not restate its contents anywhere (this doc used to name six
repos and went stale as the list grew); read the file.
