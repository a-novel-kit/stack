# Planning commands and board operations

Read this reference when routed here by [plan-feature](../SKILL.md). Its rules apply to the selected work.

## gh quick reference

IDs (project, field, single-select option) are discovered with `gh project field-list <num> --owner
<org>` and `gh project item-list`; field values are then set with `gh project item-edit`.

| Action                             | Command                                                                                                                                                                                                                                                                                                                                              |
| ---------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Create an Initiative (umbrella)    | `gh issue create --repo <org>/<repo-or-.github> --type Initiative --assignee "@me" --project "Tasks" --milestone "<goal-name>" --title "..." --body-file <file>` — `.github` when cross-repo, the repo itself when single-repo; then set Status → **Tracking** via `gh project item-edit` (a board field, not a `create` flag — see the ⚠ row below) |
| Create an Epic (cross-repo)        | `gh issue create --repo <org>/.github --type Epic --assignee "@me" --project "Tasks" --milestone "<goal-name>" --title "..." --body-file <file>` — add `--parent <initiative-#>` when under an Initiative                                                                                                                                            |
| Create a Task sub-issue            | `gh issue create --repo <org>/<repo> --type Task --parent <epic-#-or-url> --assignee "@me" --project "Tasks" --milestone "<goal-name>" --title "..." --body-file <file>`                                                                                                                                                                             |
| Re-parent an issue                 | `gh issue edit <n> --repo <org>/<repo> --parent <new-parent-#>` (or `--remove-parent`; `--add-sub-issue`/`--remove-sub-issue` on the parent)                                                                                                                                                                                                         |
| ⚠ Board ≠ milestone ≠ fields       | `--project` only **boards** the item; it does **not** set the Milestone or any field. Pass `--milestone` at create-time, then set Priority/Size/Stage via `gh project item-edit`.                                                                                                                                                                    |
| Verify a batch create              | `gh project item-list <7\|1> --owner <org>` → scan for a Stage-tagged item with an empty Milestone (the tell that `--milestone` was forgotten)                                                                                                                                                                                                       |
| Add an existing issue to the board | `gh project item-add <num> --owner <org> --url <issue-url>`                                                                                                                                                                                                                                                                                          |
| Sequence stages                    | `gh issue edit <m> --repo <org>/<repo> --add-blocked-by <n>`                                                                                                                                                                                                                                                                                         |
| Set Priority / Size / Status       | `gh project item-edit --id <item-id> --field-id <field-id> --project-id <proj-id> --single-select-option-id <opt-id>`                                                                                                                                                                                                                                |
| Iterate the plan body              | `gh issue edit <n> --repo <org>/<repo> --body-file <file>`                                                                                                                                                                                                                                                                                           |
| Discuss / open question (bot)      | `a-novel core bot-comment <org> <repo> <n> --body "..."`                                                                                                                                                                                                                                                                                             |
| Delete a not-yet-started draft     | `gh issue delete <n> --repo <org>/<repo> --yes`                                                                                                                                                                                                                                                                                                      |

(Board numbers: `a-novel` → project **#7** "Tasks"; `a-novel-kit` → project **#1** "Tasks".)

### Token scopes & permissions

Issue work is core to this workflow, so the `gh` session **should always be able to manage the full
issue lifecycle** — create, read, update, delete, plus sub-issues, dependencies, labels, and
milestones. That rides on the **`repo`** scope (deleting an issue additionally needs an owner/admin
role on the repo, which org owners have). Reading and writing **board fields** (Priority / Size /
Status / Stage) needs the **`project`** scope. Managing the org-level **issue types** themselves
(adding / editing / removing a type such as `Epic`) needs **`admin:org`** — a one-time admin act.

**If any `gh` / `gh api` command fails with an authorization or `INSUFFICIENT_SCOPES` error, do not
work around it** (don't fall back to a label, a comment, or a local file). Stop and **ask the human
to grant the missing scope**, naming it explicitly:

```bash
gh auth refresh -h github.com -s <missing-scope>   # e.g. -s project, -s admin:org
```

Then retry the command. Higher privilege is granted on request for exactly this reason; never
silently degrade the plan to fit a missing scope.

---

## Operating the board

**One board per org, scaled by _views_ not boards.** Each org has exactly one "Tasks" project, and
GitHub's own model is **one project, many saved views**. Orgs that run many boards (Kubernetes
per-SIG, Node.js per-team, Prometheus per-release) have many parallel _teams_, a scale driver we don't
have; comparable focused projects (Astro, Vite, Excalidraw) anchor on a single board. Do **not** add
per-area or per-release boards. A project **cannot span two orgs**, which is the other reason the two
orgs keep separate boards (cross-org epics are tracked by reference — see "Where the issue lives").

**Status is single-writer: the board's bot owns it, not GitHub's built-in workflows.** The bot derives
every Pull-Request-backed status (draft → _In progress_, ready → _In review_, approved → _Done_,
merged → _Awaiting release_) and a sweep re-asserts it, so editing one of those by hand will not
stick. The statuses no Pull Request can produce stay yours to set: `Triage` → `Ready`, an
Initiative's `Tracking`, and a meta task's final `Applied`. Leave the built-in workflows that derive status **from Pull Request state** off: they cannot
see the board's own statuses (_Awaiting release_, _Tracking_, _Applied_), and GitHub's _Pull request
merged → Done_ directly contradicts the bot's _merged → Awaiting release_. The one built-in that stays
**on** is _Item added to project → Triage_: at add-time there is no Pull Request to read from, so the
bot has no opinion yet, and anything boarded outside the skills lands in the Triage queue instead of
sitting status-less and unseen — including an item whose field edits were forgotten (see the footgun
above). Planned work never lingers there, because the skills set its real status in the same breath.
**Keep the _auto-add_ workflows OFF** too — both _Auto-add to project_ (repo) and _Auto-add
sub-issues to project_. **The bot sets Status, the skills add the items:** every issue and sub-issue
joins the board explicitly via `--project` on `gh issue create` (or `gh project item-add`), so board
membership stays deliberate. **Backlog** is the landing for **planned** work; **Triage** is a
_deliberate_ status a maintainer moves an un-assessed issue into — and where the deferred
external-user intake will file incoming reports — so the _Triage_ view surfaces only work still
needing assessment, not every newly added item.

**Saved views worth having** (also UI-only): _Board by Status_; _Triage_ (`is:open status:Triage`);
_My/agent items_ (`assignee:@me is:open`); _Roadmap_ grouped by Milestone; _Epics_ grouped by
**Parent issue** (or filtered `type:Epic`).

**Recurring triage is its own skill.** Grooming the open-issue set — prioritising, assigning weights,
setting due dates, refining drafts about to go active — is the `triage-issues` skill, run manually
during a planning pass. `plan-feature` sets a ticket's fields at _creation_; `triage-issues` keeps
them honest over time.
