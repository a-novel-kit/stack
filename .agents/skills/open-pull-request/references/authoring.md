# PR authoring and tracking metadata

Read this reference when routed here by [open-pull-request](../SKILL.md). Its rules apply to the selected work.

Contents:

- [Phase 5: Create the PR](#phase-5-create-the-pr)
- [5.0 PR authoring runs as the operator — the bot can only comment](#50-pr-authoring-runs-as-the-operator--the-bot-can-only-comment)
- [5.1 Choose the base branch](#51-choose-the-base-branch)
- [5.2 Title](#52-title)
- [5.3 Body](#53-body)
- [5.4 Flags](#54-flags)
- [5.5 Trace the PR to its planning issues](#55-trace-the-pr-to-its-planning-issues)
- [5.6 Capture the PR URL](#56-capture-the-pr-url)
- [Phase 6: Updating an Existing PR](#phase-6-updating-an-existing-pr)
- [6.1 Mark ready the moment you want review (mandatory)](#61-mark-ready-the-moment-you-want-review-mandatory)

## Phase 5: Create the PR

### 5.0 PR authoring runs as the operator — the bot can only comment

`gh pr create` (and every `gh pr edit` / `gh pr ready` in this skill) runs with the plain
`gh` credential, the operator's **user token**. The operator authors the PR, so the
`auto-assign-author` workflow can assign them and `CODEOWNERS` routing works.

Authoring a PR as the bot is impossible by construction. There is no local bot token, and
the only bot entry point — `a-novel core bot-comment <org> <repo> <number> --body …` —
does one thing: trigger the centralized dispatcher workflow, which _posts a comment_. So
`pr create|edit|ready|merge|close` are always operator actions; commenting (top-level
PR/issue comments and review-thread replies in `resolve-pr-feedback`) is the only thing
that attributes to `<app-slug>[bot]`.

If a `gh pr create`/`edit`/`ready` call fails with an auth/permission error, surface it
to the user — there is no bot fallback to route around it.

### 5.1 Choose the base branch

- Default: `master`
- Stacked: the parent feature branch (e.g., `feat/dao/jwk-revoke`)

Pass the base explicitly with `--base` when it is not `master`:

```bash
gh pr create --base feat/dao/jwk-revoke ...
```

### 5.2 Title

The title is a Conventional-Commits line matching the primary commit on the branch. Under
70 characters. No period. It becomes the subject of the commit that lands on the default branch,
which the `commit-messages` ruleset checks; the branch's own commit subjects never land.

```
feat(dao): add soft-delete repository for key revocation
```

With multiple commits touching one scope, use the scope that best describes the branch's
goal. When the commits are genuinely cross-cutting (rename across layers), omit the scope.

### 5.3 Body

Use this template, passed via HEREDOC to preserve formatting. Skip sections that do not
apply — do not write "no changes" placeholders.

```bash
gh pr create --title "feat(dao): add soft-delete repository for key revocation" --body "$(cat <<'EOF'
## Summary

- Adds `PgJwkRevoke` DAO for marking keys as revoked.
- Returns `ErrJwkRevokeNotFound` when the target is already revoked or expired.

## Layers changed

- **DAO**: new `pg.jwkRevoke.go` + test; sentinel error added.

## Breaking changes

None.

## Test plan

- [x] `a-novel test --type=go -y` passes
- [ ] CI green

## Tracking

Closes a-novel/.github#412 · Epic a-novel/.github#405 · milestone "JSON keys: key revocation".
EOF
)"
```

Rules:

- **Summary** is 1–3 bullets describing what changed _and why_. Readers see the diff; they
  need the intent.
- **Tracking** names the Task the PR implements, its Epic and their milestone, as
  [5.5](#55-trace-the-pr-to-its-planning-issues) describes. Only a PR with no planning issue
  omits it.
- **Layers changed** lists only the layers actually touched. Omit the section entirely if
  only one layer is affected and the title already conveys it.
- **Breaking changes** is either `None.` or an itemized list with migration steps. Never
  leave it "TBD" or blank — reviewers should not have to hunt.
- **Test plan** is a checklist. Check the boxes you have already verified locally; leave
  `CI green` unchecked (monitor-ci will mark it).
- **Local review surfaces stay out of the PR body.** Never put localhost or another local-only URL in
  durable PR metadata. For rendered UI, `write-frontend` requires the freshly verified direct
  Storybook link in the completion report instead.
- **Read the body back after create/edit.** Run
  `gh pr view <n> --json body --jq .body` and verify headings, lists, and line breaks render as
  intended. Literal `\n` text is a quoting defect; fix it before handoff.

**Writing style — rationale-dense, zero filler.** The body's job is what the diff cannot say:
why the change, what tradeoff was taken, what a reviewer should scrutinize. Never narrate the
diff — file lists, mechanical renames, and "updated X to Y" bullets restate what review tooling
already shows. Exhaustive on decisions, silent on mechanics. The same bar applies to PR thread
comments (`resolve-pr-feedback`), where prose may lean more technical.

### 5.4 Flags

Pass `--milestone` with the Task's milestone ([5.5](#55-trace-the-pr-to-its-planning-issues)).
Leave out these flags:

- `--assignee` / `--reviewer` — the `auto-assign-author` workflow handles assignees; the
  repo decides reviewers via its `CODEOWNERS` file (at repo root) or team routing. Manual
  assignment duplicates or conflicts with that automation, so set reviewers only when the
  user asks for a specific person.
- `--label` — downstream automation derives labels from the title's Conventional-Commits
  type. Add one manually only when the user requests it.
- `--project` — a PR is never a board item (5.5).

### 5.5 Trace the PR to its planning issues

A PR carries its Task's milestone, and its Tracking section names the Task, the Epic and that
milestone. A reader then traces the PR to its planning issues from the PR itself. Read all three
from the Task:

```bash
gh issue view <n> --repo <org>/<repo> --json milestone,parent \
  --jq '{milestone: .milestone.title, due: .milestone.dueOn, parent: .parent.url}'
```

The parent is the Epic, or a Feature whose own parent is the Epic.

- **Milestone.** Pass `--milestone "<milestone>"` to `gh pr create`, or to `gh pr edit` on an open
  PR. Each repository holds its own milestones. When the PR's repository has none of that name,
  create one with the identical name and due date, as
  [plan-feature](../../plan-feature/references/planning-issues.md) groups cross-repo work:
  `gh api repos/<org>/<repo>/milestones -f title="<milestone>" -f due_on=<due>`. GitHub silently
  drops a milestone set without Write on the repository. Read it back with
  `gh pr view <n> --json milestone`.
- **Full refs.** Write each issue as `<org>/<repo>#<n>`, even within one repository. A bare `#<n>`
  resolves to the PR's own repository, so a planning issue in a `.github` repository goes unlinked
  and freezes on the board.
- **Closing keyword.** On a PR into the default branch, the Task ref reads `Closes <ref>`. The
  keyword puts the Task in the PR's `closingIssuesReferences`, the sole signal `derive-status` reads
  to move the Task's board Status: In review when the PR is ready, Awaiting release when it merges.
  GitHub ignores closing keywords on a PR into any other base, such as a long-lived `v1` branch.
  Such a PR writes `Task <ref>`, and its Tracking section is its only link to the Task.
- **Board.** A PR is never a board item. Do not add it to a board, and do not copy the Task's
  Priority, Size or tracking labels onto it. A second card per task drifts from the first, and
  nothing keeps the two in sync.
- **`epic:<N>` label.** Leave it off. A maintainer applies it to switch on the Epic's atomic
  landing, which `coordinate-landing` owns. The Epic ref in the Tracking section is the PR's link to
  its Epic.

### 5.6 Capture the PR URL

`gh pr create` prints the PR URL on success. Surface it in the final message so the user can
jump to it. When an active layer skill requires a companion review surface, include its direct link
in the same completion report as the PR and linked task or issue URLs; for rendered UI, this is the
live local Storybook route required by `write-frontend`. Do not add that local URL to the PR body.

---

## Phase 6: Updating an Existing PR

When a PR is already open for this branch and you need to change its metadata (not code):

```bash
# Change the title
gh pr edit --title "feat(dao): add revoke repository with soft-delete"

# Replace the body (use HEREDOC as in Phase 5.3)
gh pr edit --body "$(cat <<'EOF'
...
EOF
)"

# Flip from draft to ready
gh pr ready

# Flip from ready back to draft
gh pr ready --undo
```

When the change is code, push new commits instead — the PR updates automatically. Never
close and re-open a PR to change its code; that loses review comments and CI history.

### 6.1 Mark ready the moment you want review (mandatory)

A draft only backs up dirty, unfinished work
([Phase 3](../SKILL.md#phase-3-decide-pr-status-ready-vs-draft)).
**Every time you push to a draft PR, re-check whether anyone other than you should look at it
now** — status is not decided once at creation and forgotten. When you want feedback, an audit or
an approval, flip it in the same turn and list in the body what still gates the merge:

```bash
gh pr ready
```

**Never ask for a review of a draft, and never end a turn with reviewable work in one.** GitHub
refuses approvals on drafts, so the reviewer cannot act on it; waiting for the user to say "flip it"
is the failure, not the courtesy. Ready does not mean mergeable: approval, the full relevant tests
and the final cleanup still gate the merge.
