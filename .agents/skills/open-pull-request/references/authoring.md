# PR authoring and tracking metadata

Read this reference when routed here by [open-pull-request](../SKILL.md). Its rules apply to the selected work.

Contents:

- [Phase 5: Create the PR](#phase-5-create-the-pr)
- [5.0 PR authoring runs as the operator — the bot can only comment](#50-pr-authoring-runs-as-the-operator--the-bot-can-only-comment)
- [5.1 Choose the base branch](#51-choose-the-base-branch)
- [5.2 Title](#52-title)
- [5.3 Body](#53-body)
- [5.4 Do NOT pass these flags](#54-do-not-pass-these-flags)
- [5.5 Tracking metadata — match the linked issue](#55-tracking-metadata--match-the-linked-issue)
- [5.6 Capture the PR URL](#56-capture-the-pr-url)
- [Phase 6: Updating an Existing PR](#phase-6-updating-an-existing-pr)
- [6.1 Flip a draft to ready the moment it qualifies (mandatory)](#61-flip-a-draft-to-ready-the-moment-it-qualifies-mandatory)

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
70 characters. No period.

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
EOF
)"
```

Rules:

- **Summary** is 1–3 bullets describing what changed _and why_. Readers see the diff; they
  need the intent.
- **Linked issues — close a planning issue with the FULL cross-repo ref.** A PR implementing
  a planning issue must close it in the body so merging advances the board. Planning issues
  (Epic / Feature / Task) live in the org **`.github`** repos (`a-novel-kit/.github`,
  `a-novel/.github`), so from another repo a bare `Closes #<n>` resolves to _this_ repo and
  links **nothing** — the issue then freezes on the board (a Feature stuck at Backlog though
  its PR merged). Use `Closes a-novel-kit/.github#<n>` (or `a-novel/.github#<n>`): only that
  form lands in the PR's `closingIssuesReferences`, the sole signal `derive-status` reads to
  move the issue's board **Status**. A Task filed in _this same_ repo keeps the bare
  `Closes #<n>`.
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

### 5.4 Do NOT pass these flags

- `--assignee` / `--reviewer` — the `auto-assign-author` workflow handles assignees; the
  repo decides reviewers via its `CODEOWNERS` file (at repo root) or team routing. Manual
  assignment duplicates or conflicts with that automation, so set reviewers only when the
  user asks for a specific person.
- `--label` — downstream automation derives labels from the title's Conventional-Commits
  type. Add one manually only when the user requests it.
- `--milestone` / project board / **Priority** / **Size** / tracking labels — **not** left to humans:
  a **ready** PR mirrors the milestone, project board, its board fields (Priority, Size), and labels
  of the issue it closes. See [Tracking metadata](#55-tracking-metadata--match-the-linked-issue).

### 5.5 Tracking metadata — match the linked issue

A PR that is **ready for review** should be as trackable as the planning issue it closes: add it
to the org **"Tasks"** board and give it the **same milestone**, the **same board fields
(Priority, Size)**, and the relevant **tracking labels** as that issue — so a glance at the board
shows the work whether you look at the issue or its PR. A draft skips this; apply it when opening
ready, or at the **draft → ready** flip (Phase 6).

```bash
gh pr edit <n> --repo <org>/<repo> --add-label <label> --milestone "<milestone-title>"
gh project item-add <project-number> --owner <org> --url <pr-url>
# then mirror the issue's Priority and Size (single-select board fields — discover ids with
# `gh project field-list <num> --owner <org>`):
gh project item-edit --id <pr-item-id> --project-id <proj-id> --field-id <priority-field> --single-select-option-id <opt>
gh project item-edit --id <pr-item-id> --project-id <proj-id> --field-id <size-field>     --single-select-option-id <opt>
```

This is **tracking** metadata mirroring the issue — distinct from the type label automation derives
from the title, and from assignee/reviewer (still automation's job, see 5.4).

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

# Flip from draft to ready — then apply tracking metadata (5.5): board + milestone + labels
gh pr ready

# Flip from ready back to draft
gh pr ready --undo
```

When the change is code, push new commits instead — the PR updates automatically. Never
close and re-open a PR to change its code; that loses review comments and CI history.

### 6.1 Flip a draft to ready the moment it qualifies (mandatory)

A draft exists for a Phase 3 reason. **Every time you push to a draft PR, re-check whether that
reason still holds** — status is not decided once at creation and forgotten. When the latest work
makes the branch review-ready — approved issue scope, resolved blocking decisions, completed
relevant tests and visual comparisons, final cleanup, and required layers/docs/dependencies — flip
it in the same turn:

```bash
gh pr ready   # then apply Phase 5.5 tracking metadata: board + milestone + labels
```

**Never end a turn with review-ready work sitting in a draft PR.** If the branch is done and green
and no Phase 3 reason still applies, the PR is ready — say so and run `gh pr ready`. Silently
leaving it draft withholds it from the reviewer and stalls the task; waiting for the user to say
"flip it" is the failure, not the courtesy.
