# Sweeping dependency PRs

Read this reference when routed here by [monitor-ci](../SKILL.md): checking, unsticking or landing
Renovate and Dependabot PRs across both organizations. Its rules apply to the sweep.

Contents:

- [Find the open PRs](#find-the-open-prs)
- [Classify each stuck PR](#classify-each-stuck-pr)
- [Land them without saturating CI](#land-them-without-saturating-ci)
- [Verify what landed](#verify-what-landed)

## Find the open PRs

Renovate runs as one self-hosted App per org: `app/anovelbot-dependencies` in `a-novel` and
`app/anovelkitbot-dependencies` in `a-novel-kit`. Dependabot is `app/dependabot`. Searching for
`app/renovate` finds nothing.

Query each org once with GraphQL `search`, reading `mergeStateStatus`, `reviewDecision`,
`isInMergeQueue`, `autoMergeRequest`, `locked` and the head commit's `statusCheckRollup`. A personal
token's 5,000 calls an hour are shared by every session on the account, so poll with one query per
cycle, at most every two minutes, and never loop REST calls over every repo.

## Classify each stuck PR

Read the PR's timeline (`RemovedFromMergeQueueEvent.reason`) before acting.

| Symptom                                                                                  | Cause                                                                                  | Action                                                                                                                                 |
| ---------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `isInMergeQueue` true, auto-merge shown off                                              | Queued; GitHub reports queued PRs as not armed                                         | Wait. It is not stuck.                                                                                                                 |
| Green, approved, not queued; last removal `checks_timed_out`                             | The queue's checks did not report in time, usually runner saturation                   | `recover-prs.yaml` re-arms it, up to three times. By hand: `gh pr merge <n> --repo <r>`, with no method flag, since the queue owns it. |
| `DIRTY`                                                                                  | A sibling PR on the same lines merged first, often two `[security]` PRs on one modfile | `recover-prs.yaml` dispatches Renovate. By hand: `gh workflow run renovate.yaml --repo <r>`.                                           |
| `auto-approve` failed: `Issue is locked`                                                 | The conversation was locked while open                                                 | Unlock it; `lock-closed.yaml` locks only on close.                                                                                     |
| Approval withheld: `renovate/stability-days` pending                                     | The update is younger than its release age                                             | Wait for Renovate to mark it green; that status approves it.                                                                           |
| Red                                                                                      | See [failure diagnosis](failures.md) and the classes below                             | Fix on master; Renovate's rebase carries it.                                                                                           |
| Red and stale, every member pending the age gate, in a repo whose only JS update is pnpm | Zombie: Renovate skips the branch while every update is pending                        | Close it with `--delete-branch`; a fresh PR follows.                                                                                   |

Never push to a bot branch. The push withholds the auto-approval, Renovate force-pushes over it on
the next rebase, and the operator who pushed cannot approve it.

Recurring red classes:

- **`generated-go` fails after a Go toolchain bump**, with `export data version N is greater than
maximum supported`. A tool's indirect `golang.org/x/tools` cannot read the new compiler output; see
  the Renovate preset rules in `write-github-actions`.
- **One member of a lockstep group moved alone.** An age gate held its pair; for example, the
  Playwright image ran ahead of npm `playwright` and the browser jobs could not find their executable.
- **`build-*` fails with `go.mod requires go >= X (running go Y; GOTOOLCHAIN=local)`.** A dependency
  raised the `go` directive before the go-toolchain PR moved the images. Land the toolchain PR first;
  the other heals on rebase.
- **Docker Hub `429` or `504`, or a Sigstore `tlog` 404.** External and transient: rerun within the
  flake budget.
- **infra `scan-infrastructure` (Trivy) reports a CVE in an indirect Go module.** Renovate's
  `[security]` PR fixes it once the repo's preset enables OSV alerts; otherwise bump the module by hand.
- **infra `plan-*` needs `allow-resource-deletion`.** A database image replaces its instance
  templates. The label is a human decision.

## Land them without saturating CI

Each merge-queue entry builds its own merge group, and a service pipeline is about 30 jobs. Several
queued entries across repos can exceed the runner allowance, the groups time out, and the work is
lost. When CI is backlogged:

- Keep one entry in the queue at a time, org-wide, and enqueue the next once the queue empties.
- Do not dispatch Renovate across the fleet. Every run rebases every behind PR, which reruns its CI.
- Gauge load from runs created in the last few hours; `a-novel/.github` holds a run queued since
  2026-08-19 that never starts.
- Wait on background tasks with `TaskStop` or `[b]racket` patterns, never a bare `pkill -f`, which
  matches the calling shell.

## Verify what landed

A merged title is not evidence: Renovate retitles a PR whose branch it skipped.

- For each merged PR, grep `gh pr diff` for the title's version on a `+` line.
- Check the head SHA's statuses for a failed `renovate/artifacts`, which no ruleset requires.
- Confirm the default branch's next `main` run is green; for infra, also its `deploy` run.
