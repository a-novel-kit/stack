# Change safety

## Prove the plan before review

- **Pure refactors must plan to zero changes.** Locally, run
  `tofu plan -refresh=false -lock=false` against the real backend. It compares code with state
  without calling the APIs, so it works under a human account that cannot read the workload
  projects.
- **Data sources the account cannot read** go in a scratch copy, stubbed with the literal value
  from state. Never stub them in the committed code.
- **Within one state,** move addresses with `moved` blocks.
- **Across states,** adopt with `import` blocks in the new root. Generate them from the old state's
  attributes; never hand-guess an ID:
  - IAM members are `"<resource> <role> <member>"`, plus the condition title when there is one;
  - logging metrics are `"<project> <name>"`;
  - alert policies and channels use their server-generated `name`.
- **Before the imports apply,** check the new root locally: rename addresses into a merged scratch
  state, point a `backend "local"` override at it, and plan with the import file removed. The CI
  plan with the read-only identity then proves the import IDs.
- **After the import applies,** delete the import blocks. Leave the old state objects in place:
  nothing reads them, and versioning keeps them recoverable.
- **Forget, don't destroy,** what still holds data, using `removed { lifecycle { destroy = false } }`.
  GCS refuses to delete a non-empty managed folder.
- **Grants that served retired tooling** are better adopted and then deleted under the label than
  left behind as unmanaged orphans.
- **`prevent_destroy` blocks destroying a `for_each` instance removed from the map.** Retiring one
  needs a deliberate, reviewed lifting of that protection.

## Read the live state before trusting the code

Production can differ from `master` even when drift is clean. One example: a VM's boot metadata was
rendered by an older template, and the current code would push a flag its pinned loader does not
understand. Compare the planned diff with the image or binary that will consume it, and fix the
pins in the same change.

## Release contract

1. Migrations run during apply through the job's `run_execution_token`, which hashes the migration
   image. The Cloud Run service `depends_on` that job.
2. Traffic is `TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST`. Cloud Run routes to a revision only after its
   startup probe passes.
3. Post-deploy checks:
   - the JSON Keys smoke job runs on every new gRPC image;
   - deploy calls `/v2/healthcheck`.
4. Rollback is a revert pull request. Migrations stay backward compatible; data rollback is a
   recovery, never part of a deploy.

## Database and backup hosts

- **The host groups use an `OPPORTUNISTIC` update policy.** Merging a template, image or TLS change
  restarts nothing.
- **Rolling is a manual `deploy.yaml` dispatch** with `roll_database=<service>`. It restarts the
  host, or replaces it when the template changed; the stateful disk and IP stay. Then it restarts
  the repository VM and checks health. Ask for a full backup before rolling.
- **Database images are pinned** by `tag@digest` in foundation and in the service root. Both
  Artifact Registry copies (`agora-production`, `agora-<service>-private-production`) derive from
  that one pin.
- **A database password rotation** bumps foundation's `password_version` and the service's
  `postgres-password` in one pull request, then rolls the host. It costs a few minutes of errors.
- **pgBackRest owns backup expiry.** Never add age-based deletion to a backup bucket. Never delete
  WAL to free space.

## Trust boundaries

- **Pull-request plans run the candidate's code and providers with the read-only identity.**
  - GitHub issues no OIDC token to fork pull requests.
  - Renovate delays OpenTofu and provider updates by 7 days (6 hours for patches) to bound a
    compromised release.
  - State holds no secret values, because those are added outside OpenTofu.
- **The writer's federation condition** pins `deploy.yaml` and `recovery.yaml` on `master` behind
  the `production` environment. Changing it is a bootstrap change, applied by the last trusted
  workflow before a new one depends on it.
- **Inventory IAM by principal and resource:**
  - runtime accounts read only their own secrets;
  - database and repository hosts pull only their service's images;
  - humans get no write access in workload projects.

## Recovery

Restores go into a disposable `a-novel-recovery-<id>` project, never production. `recovery.yaml`
plans or applies the drill host from one JSON input; the runbook covers project creation, the
temporary grants, the restore units and cleanup. A successful backup proves nothing until a drill
restores it and its SQL checks pass.
