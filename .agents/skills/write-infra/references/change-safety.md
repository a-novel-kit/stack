# Change safety

- [Prove the plan before review](#prove-the-plan-before-review): zero-diff proofs, moves, imports
- [Retire protected resources](#retire-protected-resources): unlock, retire, forget
- [Read the live state](#read-the-live-state-before-trusting-the-code)
- [Release contract](#release-contract), [Capacity caps](#capacity-caps),
  [Database and backup hosts](#database-and-backup-hosts), [Trust boundaries](#trust-boundaries),
  [Recovery](#recovery)

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
- **Forget, don't destroy,** what still holds data you keep, using
  `removed { lifecycle { destroy = false } }`. A managed folder with `force_destroy = true` is
  deleted while its objects stay.
- **Grants that served retired tooling** are better adopted and then deleted under the label than
  left behind as unmanaged orphans.
- **Service roots read foundation's applied outputs.** A new foundation output must deploy before a
  service root reads it, or the service plans fail on a missing attribute. Ship the output in its
  own pull request first, then the change that uses it.
- **A plan shows the state it read.** When pull requests merge and deploy close together, re-run a
  plan that shows an unexpected change before judging it: a deploy may have applied it meanwhile.

## Retire protected resources

- **Unlock in one apply, retire in the next.** The provider reads `deletion_policy` and
  `force_destroy` from state, so a resource whose state says `PREVENT` refuses to be destroyed
  even after its block is gone. Both pull requests need the deletion label.
- **One instance of a protected `for_each`:** `moved` it to an address outside the configuration,
  plus a `removed` block (`destroy = true`, or `false` to forget). `prevent_destroy` stays on the
  other instances.
- **That move drops the instance's dependency edges.** Its grants no longer wait for it, so a
  service account can be deleted before the grants that name it. Remove an identity's grants in
  the apply before the identity.
- **IAM writes fail for a few minutes after a service account is deleted** with
  `Deleted member '…?uid=…' is not deleted`. The deletion is still propagating: wait about ten
  minutes, then re-run the failed jobs.
- **Forget, don't revoke, the deploy identity's own grant on a resource it deletes in the same
  apply.** Revoking could run first and turn the deletion into a 403. The grant goes with the
  resource.
- **A green plan does not prove the writer's permissions.** Plans run as the read-only identity,
  and an apply also polls each long-running operation. Operations carry no tags, so the writer's
  tag-conditioned roles never cover `run.operations.get`; it lives on an unconditional read role.
  When the writer starts managing a new resource type, add its write and operation permissions in
  foundation first.
- **A newly enabled API's service agent may not exist yet.** Google creates it lazily, and Workflows
  refused the first workflow with `service agent does not exist`. Create it with
  `google_project_service_identity`, the one resource on `google-beta`, and grant its `serviceAgent`
  role before the resource that needs it, as `modules/project-shell` does.

## Read the live state before trusting the code

Production can differ from `master` even when drift is clean. One example: a VM's boot metadata was
rendered by an older template, and the current code would push a flag its pinned loader does not
understand. Compare the planned diff with the image or binary that will consume it, and fix the
pins in the same change.

## Release contract

1. Migrations run during apply through the job's `run_execution_token`. The Cloud Run service
   `depends_on` that job.
   - **A token must never repeat.** Cloud Run names an execution after its job and token, and
     refuses a name it has used: the apply waits on "already exists" until it times out.
   - **Derive it from a `terraform_data` its inputs replace,** never from the inputs alone. A hash
     repeats when they come back, as on a revert or a cleared downtime. The plan policy exempts
     that replacement.
2. Traffic is `TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST`. Cloud Run routes to a revision only after its
   startup probe passes.
3. Post-deploy checks:
   - the JSON Keys smoke job runs on every new gRPC image, and again once a downtime clears;
   - deploy calls `/v2/healthcheck`;
   - each check skips once a planned downtime has started, and re-tests the start when it fails,
     since a check begun just before `start` fails when the services stop.
4. Rollback is a revert pull request. Migrations stay backward compatible; data rollback is a
   recovery, never part of a deploy.

## Capacity caps

- **Every Cloud Run service sets a service-level `scaling.max_instance_count`.** The revision-level
  one caps each revision, so a rollout can exceed it.
- **Every project that runs Cloud Run has a CPU and a memory quota** in foundation's `cost_cap`. The
  quota is the hard ceiling.
- **A service's postcondition checks its cap against the quota:** maximum instances plus one
  rollout instance, times its CPU, must fit. A plan that raises the cap fails until foundation
  raises the quota.
- **A service with in-memory rate limits** keeps the instance count its limits assume. Raising it
  is a design change, not a tuning one.

## Database and backup hosts

- **The host groups use an `OPPORTUNISTIC` update policy.** Merging a template, image or TLS change
  restarts nothing.
- **Database host changes wait for a planned downtime.** `policy.jq` flags a database host's
  template replacement or group update, which is not a deletion. Its pull request needs
  `allow-downtime`, and `DOWNTIME` must list `service-<service>.database` with a `start` already
  passed. Ask for a full backup before the downtime starts.
- **Planned downtimes come only from the admin-only `downtime.yaml`,** which writes `DOWNTIME` and
  deploys. Listed services refuse work from `start` until someone clears it; `end` only informs
  users.
- **Users see a downtime whenever a deploy can enforce one.** Any deploy applies `DOWNTIME`, so a
  schedule publishes `downtime.json` before writing it, and a clear deletes it before publishing.
- **The deploy's `rollout` job rolls hosts from observed state, not the diff,** so a cancelled or
  force-merged deploy loses nothing:
  - a database host off its group's target rolls only during its service's downtime, once the
    service's serving revision carries the downtime's start, after a snapshot of its data disk.
    Until then the deploy warns "rollout pending", or fails when the service missed the downtime.
    It restarts in place or is replaced; the stateful disk and IP stay;
  - a backup repository VM restarts when its `config` label differs from `running-config`, after
    its database host;
  - `roll-database.yaml` forces both, whatever the downtime.
- **Each rollout's snapshot is kept 14 days,** labelled `role=pre-rollout`. Undo is a revert, plus a
  same-name disk swap from the snapshot when the data is damaged (`docs/runbooks/database.md`). The
  data disk ignores its source snapshot, so the swapped disk needs no state change.
- **Database images are pinned** by `tag@digest` in foundation and in the service root. Both
  Artifact Registry copies (`agora-production`, `agora-<service>-private-production`) derive from
  that one pin. The hosts use the digest alone, so the pin rides with the service's release pull
  request: a release without a database change keeps the reproducible digest and plans nothing. A
  new digest replaces the host template, so that release merges during a planned downtime.
- **`host-credentials` is pinned once,** in foundation's `host_credentials_image`, as the
  `tag@digest` `publish-rollout-verifier.yaml` published. Deploy verifies it against that workflow
  and copies it into each service's tooling repository; the hosts and the service roots derive the
  digest reference. A new pin is a database host change.
- **Each backup repository VM keeps a reserved address,** taken from foundation's
  `native_backups.<service>.repository_ip`, the same value its database host trusts.
- **A database password rotation** bumps foundation's `password_version` and the service's
  `postgres-password` in one pull request, merged during a planned downtime, which rolls the host.
- **pgBackRest owns backup expiry.** Never add age-based deletion to a backup bucket. Never delete
  WAL to free space.

## Trust boundaries

- **Pull-request plans run the candidate's code and providers with the read-only identity.**
  - GitHub issues no OIDC token to fork pull requests.
  - Renovate delays OpenTofu and provider updates by 7 days (6 hours for patches) to bound a
    compromised release.
  - State holds no secret values, because those are added outside OpenTofu.
- **Only admins bypass the rulesets.** A merge to `master` deploys production, and every bot
  reaches it through a pull request. The bot Apps' keys are org secrets that any repository's
  workflows can read, so no bot may push `master` directly; the `infra` class sets
  `rulesets.admins_only`. This closes the direct push only: a bot's pull request that the
  auto-approver approves still deploys, tracked in a-novel/.github#414.
- **The writer's federation condition** pins `deploy.yaml`, `roll-database.yaml` and
  `recovery.yaml` on `master` behind the `production` environment. Changing it is a bootstrap change, applied by the last trusted
  workflow before a new one depends on it.
- **`infra-downtime` trusts only `downtime.yaml` on `master`.** It can start, list and cancel the
  `downtime-announcements` executions in the management project, and nothing else.
- **The read-only identity trusts only pull requests into `master`.** A stacked pull request's plans
  fail with `rejected by the attribute condition`. Once its parent merges, GitHub retargets it to
  `master`; rebase it onto `master` to drop the parent's squashed commit, and its plans run.
- **Cloud Workflows can record what a workflow reads.** Detailed execution history keeps variables
  and call logs keep call results, so a workflow that reads a secret pins
  `call_log_level = "LOG_NONE"` and `execution_history_level = "EXECUTION_HISTORY_BASIC"`.
  Bootstrap's test checks both for `downtime-announcements`.
- **A service that trusts a forwarded visitor header requires IAM.** Its invokers are only the
  platform and the deploy health check, which sends an ID token for the service's custom audience.
- **Inventory IAM by principal and resource:**
  - runtime accounts read only their own secrets;
  - database and repository hosts pull only their service's images;
  - humans get no write access in workload projects.

## Recovery

Restores go into a disposable `a-novel-recovery-<id>` project, never production. `recovery.yaml`
plans or applies the drill host from one JSON input; the runbook covers project creation, the
temporary grants, the restore units and cleanup. A successful backup proves nothing until a drill
restores it and its SQL checks pass.
