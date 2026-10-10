# Workspace hygiene

Read this reference when routed here by [git-conventions](../SKILL.md). Its rules apply to the selected work.

## Workspace Hygiene

### Before you start

Load [develop-feature](../../develop-feature/SKILL.md) for stage timing. The clean-tree pre-flight below
applies when starting new work; an owned, ongoing local draft may be dirty and must be preserved.
Resume it without resetting, stashing, or discarding the draft to satisfy a new-task pre-flight.

**Start new work from _freshly-pulled_ `master`, with a clean tree, in a checkout that is
yours.** Check this before the first edit — in the stack root and in each `app/` or `kit/` checkout
the task will touch, since those are independent repos with independent states:

```bash
git -C <checkout> status --porcelain            # empty
git -C <checkout> checkout master               # be on master before pulling
git -C <checkout> pull --ff-only                # fast-forward to origin/master, no merge commit
git -C <checkout> rev-parse --abbrev-ref HEAD   # master
```

**Always pull `master` before cutting the branch — never branch from a stale local `master`.** A
branch cut from a `master` that is days behind starts life already diverged: it re-runs work that
landed since, collides in review with changes it never saw, and forces a rebase later that a
`pull --ff-only` now would have avoided. `--ff-only` refuses to invent a merge commit — if local
`master` has drifted (someone committed to it directly, which should not happen), it stops so you
can look, rather than silently tangling histories. A branch whose parent is already merged (as a
completed task's branch is, once its PR lands) is finished work; leave it and branch from master.

Unrecognized uncommitted changes, or a checkout on another task's branch,
mean **someone else may be working in this checkout** — the operator in another terminal, or a parallel
agent session. The mere _existence_ of stale unmerged branches is not that signal: a repo that has
shipped hundreds of PRs carries dozens of finished branches nobody deleted, and none of them blocks
cutting a fresh one from `master` — which is why the pre-flight keys on the current branch and the
tree, not on `branch --no-merged`. When someone _is_ working here, their work-in-progress is
invisible to you, and `stash`, `reset`, `checkout -f`, or branching on top of it can destroy hours
of work that exists nowhere else. Leave it untouched and take a checkout of your own.

**A clean pre-flight expires immediately.** It proves the checkout was free at that instant, and a
checkout has one HEAD that nothing holds: a parallel session running `git checkout` between your
`checkout -b` and your `commit` lands your commit on whatever branch it moved you to, and its own
`reset` can then unlink it. For anything longer than a couple of commands, work in a worktree of
your own — git refuses to check out a branch already checked out elsewhere, so the branch cannot be
taken from you mid-task, and the shared object store keeps every commit you have made.

```bash
git worktree add <path-outside-the-repo> <branch>
```

After a collision nothing is lost: the commit is unreferenced, not deleted. Find it in `git reflog`,
point your branch at it with `git branch -f <branch> <sha>`, and put local `master` back with
`git branch -f master origin/master`. Clear your own stray files out of the shared tree so the other
session does not commit them, and leave everything of theirs alone.

The daemon manages as many stacks as the machine supports. `A_NOVEL_STACKS` is its source of truth,
formatted `name:/path,name:/path` with the first entry as the default; unset means a single
`default` stack at `~/git-projects/a-novel`.

```bash
a-novel core stacks list             # which stacks exist, and what they hold
a-novel core stacks new <name>       # clone the workspace into a fresh root
a-novel core sync --root=<new-root>  # populate it with the whitelisted repos
```

`stacks new` puts the checkout under the OS temp directory unless `--root` says otherwise, so a
stack nobody prunes expires instead of accumulating. Add the printed entry to `A_NOVEL_STACKS` and
`a-novel core restart` so daemon-backed verbs (`a-novel run …`) reach it, then work from there —
see `use-a-novel-cli`.

Resuming a branch **you** created earlier in the same session is your own work; carry on with it.

### When you are done

**Touch only what this session created**, unless the developer explicitly says otherwise. A request
to clean up or clear worktrees, branches, stacks or issues means yours: the worktrees and branches
you added, your subagents' worktrees, your scratch stacks and containers, and the issues and pull
requests you opened. List them as you create them, so cleanup removes exactly that list. A global
cleanup that owns the whole workspace is always requested explicitly; a bare "clean up" never is one.

Everything else stays, however finished it looks. A clean worktree on a merged branch can still be
the working directory of a live parallel session, and removing it, deleting its branch, or
fast-forwarding a shared checkout pulls that session's ground from under it. Clean, merged and idle
are not ownership. When you cannot tell whether something is yours, leave it and ask.

A stack synced for one task is scratch space. Left behind it becomes a stale checkout the next
session mistakes for real work, plus containers and volumes that outlive the machine's reboot.

**Prune it when development ends: every change reviewed and approved.** Not at push, and not at
green CI — review turns up work, and rebuilding a stack to answer one comment costs more than
keeping it a few hours longer. Approval is the first moment the checkout is no longer needed.

```bash
a-novel core stacks list           # what each stack is still holding
a-novel core stacks prune <name>   # kill its targets + infra, clear its volumes, remove its files
```

Prune covers all three of a stack's allocations. Deleting the root by hand covers one: the
containers keep running on their host ports and the volumes stay in the container store, because
neither ever lived in the stack directory.

`prune` refuses the default stack, and refuses any stack still holding work that exists nowhere
else — so run it without rehearsing the state first. It reports the `A_NOVEL_STACKS` entry to drop
rather than editing your shell config.

**Never reach for `a-novel core kill --force` as cleanup.** It tears down every service's infra
across every registered stack, the operator's included — the exact harm the pre-flight above
exists to prevent.
