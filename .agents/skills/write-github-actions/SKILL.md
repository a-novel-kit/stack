---
name: write-github-actions
description: >
  Write and maintain GitHub Actions workflows, composite actions (action.yaml), and repo CI config
  across the a-novel / a-novel-kit orgs. Use whenever adding or editing a workflow, a shared action
  in a-novel-kit/workflows, a CI job, a required check, or a ruleset.
---

# Writing GitHub Actions

CI is written across two surfaces. The shared building blocks are **composite actions** in
`a-novel-kit/workflows`, each at `<group>/<name>/action.yaml` under `build-actions`,
`generic-actions`, `github-pages-actions`, `go-actions`, `node-actions`, or `publish-actions`. Every
repo's `.github/workflows/*.yaml` then calls them, pinned to a release tag. Read the neighbours
before writing either — the patterns below are already in every file.

**Scope.** This skill owns authoring: workflow files, action manifests, and the repo CI config that
turns a job into a required check. `monitor-ci` owns watching a run and diagnosing a failure.
`coordinate-landing` owns the cross-repo landing saga and merge-queue semantics. `manage-versions`
owns releasing the workflows repo and re-pinning consumers. Point at them; do not restate them.

---

## Choosing a surface

A **composite action** packages a step sequence that runs inside someone else's job. It cannot
declare `permissions`, cannot fan out across jobs, and has no early return — a guard that must stop
the work sets an output and every later step carries an `if:` on it (`generic-actions/derive-status`
threads a `halted` output that way).

A **reusable workflow** (`.github/workflows/<name>-run.yaml`, triggered by `workflow_call`) packages
whole jobs with their own `permissions` and `secrets:` block. `merge-gate-run.yaml` is one: each
governed repo ships a thin caller so the engine lives in one place. Inside a reusable workflow, `./`
resolves against the **caller's** checkout, so its nested actions must be remote-pinned.

A **repo-local composite action** (`.github/actions/<name>/action.yml`) holds a step sequence used by
several jobs in one repo and by no other repo — `service-json-keys`' `run-migrations` is the model.

---

## Composite actions cannot reference vars, secrets, needs, or matrix

GitHub expression-evaluates the **entire manifest** when it loads a composite action: input
`description` fields, `run:` strings, bash comments inside them. Those contexts do not exist for a
composite action, so a literal `${{ vars.X }}`, `${{ secrets.X }}`, `${{ needs.* }}`, `${{ matrix.* }}`
or `${{ strategy.* }}` anywhere in the file makes it fail to load with
`Unrecognized named-value: 'vars'` — including where it appears purely as documentation of the
caller's syntax. The available contexts are `inputs`, `github`, `steps`, `runner`, `env`,
`job.container`, plus `toJSON` and `always()` / `success()` / `failure()`.

To document caller syntax inside an action, write the context name as plain text. `merge-gate` and
`board-write` describe their halt input as "threaded from the caller as
`kill_switch: vars.AGENT_KILL_SWITCH`" with no `${{ }}` around it, and load fine.

The failure is invisible on the PR that introduces it. The workflows repo's own governance callers
stay pinned to the previous release tag while the PR is open, and its `main.yaml` exercises only the
node lane through `./node-actions/lint-node`, so nothing loads the edited manifest. The breakage
surfaces the moment a consumer pins the new tag — which is how a `vars` reference in an input
description took `merge-gate` offline across both orgs until the next patch. The `lint-action-manifests`
job in the workflows repo's `main.yaml` now greps every composite manifest for these contexts and is
a required check; keep it passing rather than working around it.

Caller **workflows** carry `${{ vars.* }}` and `${{ secrets.* }}` legally. Thread the value into the
action as an ordinary input.

---

## Writing a composite action

```yaml
name: board-write
description: >
  One-line purpose, then the rationale a caller needs. This text is the README catalog entry.

inputs:
  client_id:
    required: true
    description: What it is and why the action needs it.
  dry_run:
    required: false
    default: "false"
    description: When "true", log the change that would be made without writing it.

outputs:
  changed:
    description: '"true" if the field was mutated.'
    value: ${{ steps.write.outputs.changed }}

runs:
  using: composite
  steps:
    - name: Write the field
      id: write
      shell: bash
      env:
        PROJECT_ID: ${{ inputs.project_id }}
      run: |
        set -euo pipefail
        ...
```

- Input names are `snake_case`, and every input carries a `description` — GitHub prints it and the
  README catalog is transcribed from it by hand, so an inaccurate one propagates. (`go-actions/lint-go`
  keeps a kebab-case `working-directory` from before the convention settled.)
- Booleans travel as the strings `"true"` / `"false"`; there is no boolean input type for actions.
- Every `run:` step declares `shell: bash` explicitly — a composite action has no default shell.
- Pass values into a script through `env:` and read them as shell variables, rather than
  interpolating `${{ inputs.x }}` into the script body, so a value containing shell metacharacters is
  data instead of code.
- Start each script with `set -euo pipefail`. Report failures with `::error::` so they surface as
  annotations, and write operator-facing results to `$GITHUB_STEP_SUMMARY`.
- Reference a sibling action in the same repo by its pinned tag
  (`a-novel-kit/workflows/generic-actions/pull-bot@v1.20.1`). A relative `./` path would bind a
  released action to whatever sits on `master`, so a release could not be internally consistent.
- Mint the narrowest App token the step needs: `actions/create-github-app-token` takes
  `permission-*` inputs (`permission-organization-projects: write`), which bounds a leak of that
  token to the one operation.
- Update the README catalog in the same change whenever a `name` or `description` moves.
- Non-trivial bash in an action belongs under `tests/`. The suites there extract the functions
  verbatim out of the manifest and run them against stubbed network leaves, so the shipped code is
  what executes and there is no fixture to drift.

Consumer-visible changes (a renamed input, a new preferred path) need a migration guide under
`docs/migrations/`; `prepare-release` owns sizing the release and writing it.

---

## Writing a caller workflow

`main.yaml` is a repo's CI. It runs on every branch push, ignoring tags:

```yaml
on:
  push:
    tags-ignore: ["**"]
    branches: ["**"]
```

Each job declares its own `permissions` block. Read-only is the floor (`contents: read`); image
publishing adds `packages: write`, `attestations: write`, `id-token: write`; a job whose action mints
its own App token declares `permissions: {}`, because the default token is never used.

Pin every `uses:` to a release tag. Renovate groups the whole workflows repo under one
`a-novel-kit workflows` update, so a repo's references move together and must sit at one version.
Third-party actions are pinned by tag too (`actions/checkout@v7`).

`vars.*` and `secrets.*` belong here. The bot credentials are org-level: `AGENT_BOT_CLIENT_ID` /
`AGENT_BOT_PRIVATE_KEY` for governance work, `DEPENDENCY_BOT_*` for the node lane, `PUBLISH_BOT_*`
for publishing.

Add a `concurrency` group with `cancel-in-progress: true` only where a superseded run's result is
worthless, such as a lint-only workflow. Anything that releases or deploys must run to completion.

---

## Job names are check contexts

A job's ID **is** its required-check context, so name it by lane: `test-go`, `lint-go`, `lint-node`,
`lint-proto`, `generated-go`, and the `build-*` / `report-*` families. A bare verb like `test` breaks
down in a repo holding both Go and JS, where two jobs would claim the name, and the discovery map in
`cli/internal/repocfg/templates/checks.yaml` cannot tell which lane it belongs to. Some repos still
emit a legacy bare `test`, and the node lane is split between `lint-node` and an older `lint-js`;
new jobs use the lane form.

A job must also not take the name of a check-run the [Agent] App posts. `merge-gate.yaml` and
`epic-freeze.yaml` both name their job `evaluate` for that reason — the job is only the runner, and a
same-named job would collide with the App's check.

---

## Required checks and rulesets

`cli/internal/repocfg/templates/checks.yaml` decides which jobs gate the default branch. The
required set is the `always` list plus every job declared in the repo's `.github/workflows/main.yaml`,
minus two exclusions: IDs starting with `report-` (reporting and post-merge jobs), and jobs whose
`if:` restricts them to master, since a job that never runs on a PR cannot gate one.

Adding a job to `main.yaml` therefore adds a required check on the next `a-novel repo update`.
Renaming one silently drops the old context and adds a new one, so rename and reconcile in the same
landing. Codecov's `codecov/patch` and `codecov/project` are posted by Codecov rather than by a job,
so they live in their own ruleset.

The rulesets themselves are static YAML under `templates/rulesets/`, with the required-check list and
the bypass actors injected by the CLI. `a-novel repo update` applies them; `use-a-novel-cli` covers
the command.

The governance workflows (`merge-gate.yaml`, `epic-freeze.yaml`, `derive-status.yaml`,
`release-train.yaml`, `hotfix.yaml`, `approve-pr.yaml`, `epic-rollback.yaml`,
`auto-approve-dependabot.yaml`) are rendered from `cli/internal/repocfg/templates/governance/` and
carry a "Managed by `a-novel repo update`" banner. Edit the template in the stack repo; a change to
the copy in a repo is overwritten. What those workflows mean is `coordinate-landing`'s subject.

---

## Verifying a change

There is no local runner, so verification is reading plus CI.

- Run `pnpm lint:stylecheck` — workflow YAML is prettier-formatted like any other file, and an
  unformatted one fails the node lane.
- For a composite action, grep the manifest for `${{ vars.`, `${{ secrets.`, `${{ needs.`,
  `${{ matrix.` and `${{ strategy.` before pushing. The workflows repo has a job for this; a repo-local
  action under `.github/actions/` does not.
- A change to a shared action is not exercised by the workflows repo's own PR CI. It is proven when a
  consumer re-pins to the released tag, which `manage-versions` sequences.
- Never trigger `release.yaml` to test a change — it cuts a real release. Use its `dry_run` input.
- Hand off to `monitor-ci` once the branch is pushed.

---

## Common pitfalls

- **A `${{ vars.* }}` or `${{ secrets.* }}` expression inside `action.yaml`.** The action fails to
  load for every consumer, and the PR that introduces it is green. Write the context as plain text and
  thread the value in as an input.
- **`uses: ./<group>/<action>` inside a composite action.** It resolves against whatever the consumer
  checked out. Reference the sibling by its pinned tag.
- **A bare-verb job ID.** `test` collides across lanes and cannot be mapped to one; use `test-go` or
  `test-node`.
- **A job named after an App-posted check.** `merge-gate` and `epic-freeze` are check-run names; a job
  of that name duplicates the context.
- **A missing `permissions` block.** The job then inherits the repository default, which is wider than
  it needs. Declare the block on every job, `{}` included.
- **`@master` or a floating ref in `uses:`.** CI stops being reproducible and a workflows release
  reaches consumers unannounced.
- **Mixed workflows-repo tags in one repo.** The actions ship as a unit; move every reference in the
  repo to the same tag.
- **A `run:` step in a composite action without `shell: bash`.** The step fails to load; composite
  actions have no default shell.
- **Editing a governance workflow in a service repo.** It carries a managed-by banner and is
  regenerated. Edit `cli/internal/repocfg/templates/governance/`.
- **Adding a `main.yaml` job without reconciling the ruleset.** The job runs but gates nothing until
  `a-novel repo update` runs.
- **Interpolating an input straight into a `run:` script.** Bind it through `env:` so the value cannot
  be read as shell syntax.
