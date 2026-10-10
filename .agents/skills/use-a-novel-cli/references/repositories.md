# Repository configuration

Read this reference when routed here by [use-a-novel-cli](../SKILL.md). Its rules apply to the selected work.

## `a-novel repo` — repository config and governance

`create` scaffolds a repository from its class template; `update` reconciles an existing one. This is
how the governance workflows, the branch rulesets, and the required-check list reach every repo — so
after adding or renaming a job in a repo's `.github/workflows/main.yaml`, its ruleset stays stale
until `update` runs.

The **class** is inferred from the repo name: `service-*` → a Go backend service, `platform-*` → a
SvelteKit frontend platform (a _terminal_ app — it ships a container image and a healthcheck route but
exports no package), `workflows` / `.github` → the shared-CI and meta repos, everything else → a shared
library (`golib`, `nodelib`, `jwt`, `stack`). A repo needing a different class carries a
`repos/<org>_<repo>.yaml` override, which wins over the name-based guess.

```bash
a-novel repo update --dry-run    # print the API operations, no writes — the agent-safe form
a-novel repo update              # interactive, human-only: a human must run this
a-novel repo update --all        # every whitelisted checkout present under app/ or kit/, in parallel
```

Four behaviours to know before running it:

- **Required checks are derived, not configured.** They are the jobs in the repo's `main.yaml` (minus
  `report-*` and master-only jobs) plus the always-required set. A new job becomes a required check on
  the next `update`, and not before.
- **Config comes from the working tree**, not from GitHub — and only `--all` guards that. The
  batch sweep skips a checkout carrying ongoing work (off its default branch, or a dirty tree) and
  reports each one as `⏸ <org>/<repo> — on <branch>, skipped`, so a partial run is visible in the
  output rather than silent. The **single-repo** form has no such guard: run from a feature branch,
  it reconciles from that branch's `main.yaml`. Be on an up-to-date default branch before running it.
- **`--all` shares `core sync`'s whitelist.** Both read `workspace-repos.yaml` at the workspace root
  through the same loader, so the batch covers every whitelisted repo actually cloned under `app/` or
  `kit/`, plus the stack repo itself. A whitelisted repo not yet cloned is simply absent. (`repo create`
  takes its `<org> <name>` explicitly — the repo does not exist yet, so no whitelist applies.)
- **`--all` runs in parallel.** It plans every repo at once, then applies `--jobs` repos at a time
  (default 6): the cap is GitHub's secondary rate limit on writes, not CPU, and a rate-limited call
  is retried after a minute. Each repo prints one status line as it finishes; the closing report
  carries every repo's per-operation log.
- **A newer deployed pin survives.** For files pinning `a-novel-kit/workflows` actions, a version
  already ahead of the template's is kept, so `update` never rolls back a bump Renovate landed.

A failed operation prints GitHub's JSON error after the HTTP status; its `errors` list names the
rejected field.

Agents stop at `--dry-run`: the write path refuses a non-TTY.
