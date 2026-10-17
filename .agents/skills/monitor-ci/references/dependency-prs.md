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
`app/renovate` finds nothing. The same App opens the `refresh-apko-locks` action's weekly PRs too;
they carry no label, so search by author, not label. It also opens infra's release pins on
`chore/deps/<repository>`: each service or platform release dispatches `pin-release.yaml` there.

The Discord dependencies feed posts each bot PR that fails CI, and each one still green and out of
the merge queue 2 hours after its last commit, so a sweep can start from its recent posts.

Query each org once with GraphQL `search`, reading `mergeStateStatus`, `reviewDecision`,
`isInMergeQueue`, `autoMergeRequest`, `locked` and the head commit's `statusCheckRollup`. A personal
token's 5,000 calls an hour are shared by every session on the account, so poll with one query per
cycle, at most every two minutes, and never loop REST calls over every repo.

## Classify each stuck PR

Read the PR's timeline (`RemovedFromMergeQueueEvent.reason`) before acting.

| Symptom                                                                                  | Cause                                                                                   | Action                                                                                                                                 |
| ---------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `isInMergeQueue` true, auto-merge shown off                                              | Queued; GitHub reports queued PRs as not armed                                          | Wait. It is not stuck.                                                                                                                 |
| Green, approved, not queued; last removal `checks_timed_out`                             | The queue's checks did not report in time, usually runner saturation                    | `recover-prs.yaml` re-arms it, up to three times. By hand: `gh pr merge <n> --repo <r>`, with no method flag, since the queue owns it. |
| Green, approved, armed, never queued; head commit's `verification.reason` is `unsigned`  | The bot built the commit with git, and `master` and `release/**` require signed commits | Fix the producer to commit through the API (`write-github-actions` pitfalls); its next run replaces the head.                          |
| `DIRTY`                                                                                  | A sibling PR on the same lines merged first, often two `[security]` PRs on one modfile  | `recover-prs.yaml` dispatches Renovate. By hand: `gh workflow run renovate.yaml --repo <r>`.                                           |
| `auto-approve` failed: `Issue is locked`                                                 | The conversation was locked while open                                                  | Unlock it; `lock-closed.yaml` locks only on close.                                                                                     |
| Approval withheld: `renovate/stability-days` pending                                     | The update is younger than its release age                                              | Wait for Renovate to mark it green; that status approves it.                                                                           |
| infra release pin `DIRTY`                                                                | Another release merged first on a neighbouring pin                                      | The next push to `master` rebuilds it. To rebuild it now, send its dispatch again (infra `docs/runbooks/deployments.md`).              |
| A release has no infra release pin                                                       | Its `deploy` job failed, or the release predates the job                                | Re-run the release's `deploy` job, or send the dispatch by hand.                                                                       |
| Red                                                                                      | See [failure diagnosis](failures.md) and the classes below                              | Fix on master; Renovate's rebase carries it.                                                                                           |
| Red and stale, every member pending the age gate, in a repo whose only JS update is pnpm | Zombie: Renovate skips the branch while every update is pending                         | Close it with `--delete-branch`; a fresh PR follows.                                                                                   |

Never push to a bot branch. The push withholds the auto-approval, Renovate force-pushes over it on
the next rebase, and the operator who pushed cannot approve it.

Recurring red classes:

- **`generated-go` fails after a Go toolchain bump**, with `export data version N is greater than
maximum supported`. A tool's indirect `golang.org/x/tools` cannot read the new compiler output; see
  the Renovate preset rules in `write-github-actions`.
- **One member of a lockstep group moved alone.** An age gate held its pair; for example, the
  Playwright image ran ahead of npm `playwright` and the browser jobs could not find their executable.
  Lockfile maintenance does the same to a ranged member whose siblings are exact: `vitest: ^5.0.0`
  reached 5.0.5 beside `@vitest/browser-playwright` 5.0.3, and the browser jobs failed on
  `closeBrowserProvider is not a function`. Fix it on master: give the whole family one specifier
  form, all exact or the same range (see `write-github-actions` Renovate presets).
- **`build-*` fails with `go.mod requires go >= X (running go Y; GOTOOLCHAIN=local)`.** A dependency
  raised the `go` directive before the go-toolchain PR moved the images. Land the toolchain PR first;
  the other heals on rebase.
- **Docker Hub `429` or `504`, or a Sigstore `tlog` 404.** External and transient: rerun within the
  flake budget.
- **infra `scan-infrastructure` (Trivy) reports a CVE in an indirect Go module.** Renovate's
  `[security]` PR fixes it once the repo's preset enables OSV alerts; otherwise bump the module by hand.
- **infra `plan-*` asks for `allow-downtime`.** A service release with a new database digest changes
  its database host's template, so it merges only during that service's planned downtime, with the
  label. A release without a database change keeps the digest and plans nothing for the hosts. Both
  the downtime and the label are human decisions.

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
