# Subscribe a repository to Drive visual tests

Read this reference when routed here by [write-frontend-tests](../SKILL.md): before a repository
compares its screenshots against Drive reference batches for the first time, or when its master
reference freezes. Run the subscription steps in order.

The adoption guides in a-novel-kit/workflows cover the repository's wiring: `docs/migrations/v1.33.0.md`
for Playwright journeys, plus `v1.64.0.md` for a Vitest browser project. This runbook provides the
identities, folders and variables those guides consume, and the order that creates the first reference.

Most steps need more than a developer holds: an infra pull request and its `visual_tests` output
(ops), the Drive shares (the Workspace owner) and repository variables (core). Check each with
[check-access](../../check-access/SKILL.md) before its step, and hand the steps the developer cannot
run to their holder.

1. **Admit the repository in a-novel/infra.** Add an entry to `visual_test_repositories` in
   `bootstrap/visual-tests.tf`: a slug of 2 to 11 characters, the repository, and its numeric ID from
   `gh api repos/<owner>/<repo> --jq .id`. The validation admits `a-novel/platform-*` and
   `a-novel-kit/uikit`, whose organization IDs `local.visual_owner_ids` already holds. On merge, the
   deploy run's `bootstrap` job adds two service accounts, two workload-identity providers and two IAM
   members. Its `visual_tests` output then lists the `<slug>-visual-ci` and
   `<slug>-visual-maintenance` provider and account under `repositories.<slug>.identities`.
2. **Have the Workspace owner prepare Drive.** The owner creates `<slug>/ci/references` and
   `<slug>/ci/results` in a Shared Drive, then grants:
   - `<slug>-visual-ci`: Viewer on `references` and Contributor on `results`, on the folders alone.
   - `<slug>-visual-maintenance`: Manager on the Shared Drive. Maintenance deletes batches
     permanently, and the Drive API refuses that organizer authority on a folder.

   A failed Drive call adds its operation, HTTP status and Drive reason to the generic
   `Drive operation failed; check API access and quota in the private consoles`. At the
   `Download the current master reference` step, `(files.get, HTTP 404, notFound)` means the CI
   identity cannot see a folder. Either a share is missing, or a new Shared Drive refuses outside
   and non-member access: a service account counts as outside the organization. Once the folders
   are visible, the helper's own messages name the missing capability, such as
   `The identity cannot upload platform results`.

3. **Merge the wiring pull request first.** Keep `VISUAL_CI_PROVIDER` unset until the pull request
   that passes `test-playwright`'s Drive inputs and adds `visual-tests.yaml` has merged. Drive turns on
   once that variable is non-empty. From then on, every run fails until a reference exists, the wiring
   pull request's own run included. A wiring pull request that folds a required job into
   `test-browser`, as uikit's did with `test-storybook`, follows the
   [required-checks rule](../../write-github-actions/SKILL.md#required-checks-and-rulesets) for a
   removed job. The GitHub root already provisions the `allow-screenshot-change` label.
4. **Set the repository variables.** The GitHub root leaves repository variables alone, so set
   each one with `gh variable set <name> --repo <owner>/<repo> --body '<value>'`. The identity values
   come from step 1's `visual_tests` output, under `repositories.<slug>.identities`.

   | Variable                      | Value                                              |
   | ----------------------------- | -------------------------------------------------- |
   | `VISUAL_CI_PROVIDER`          | `ci.identity_provider`                             |
   | `VISUAL_CI_ACCOUNT`           | `ci.service_account`                               |
   | `VISUAL_MAINTENANCE_PROVIDER` | `maintenance.identity_provider`                    |
   | `VISUAL_MAINTENANCE_ACCOUNT`  | `maintenance.service_account`                      |
   | `VISUAL_REFERENCES_FOLDER`    | The folder ID of `references`, from its Drive URL  |
   | `VISUAL_RESULTS_FOLDER`       | The folder ID of `results`                         |
   | `VISUAL_SEED_SHA`             | Master's head commit, at or after the wiring merge |

5. **Seed the first reference.** Every Drive run fails with
   `No reference exists; explicit master seeding is required` until a first reference exists. Only the
   latest attempt of the master `main` run at the commit named by `VISUAL_SEED_SHA` may create one.
   1. Re-run the `test-browser` job of the master `main` run at that commit. The re-run stages a
      pending batch.
   2. Wait for the `cleanup` job of `visual-tests.yaml` to promote that batch.
   3. Delete the seed with `gh variable delete VISUAL_SEED_SHA --repo <owner>/<repo>`.
   4. Re-run `test-browser` once more. The first reference is live once its `visual-comparison`
      status reads `matched:<seed sha>`.

## Recover a frozen reference

A master reference is frozen when master's `test-browser` fails on screenshot changes that merged
pull requests already approved, and every re-run fails the same way. A commit without a pull request
between the reference and master's head causes it, such as an admin's direct push. A workflows
release older than the fix for a-novel-kit/workflows#685 also freezes it when a release is cut during
an approved change's master run.

1. Have the Workspace owner trash the current reference, the `playwright-<run>-<attempt>.tar` batch in
   `<slug>/ci/references`.
2. Set `VISUAL_SEED_SHA` to master's head commit, as in step 4.
3. Seed from that commit as step 5 describes. Drive can list a trashed batch for about ten minutes, and
   a re-run that still finds it fails on the same drift. Re-run `test-browser` until one passes.
