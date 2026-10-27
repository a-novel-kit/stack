# Change safety

- [Prove the plan before review](#prove-the-plan-before-review): zero-diff proofs, moves, imports
- [Retire protected resources](#retire-protected-resources): unlock, retire, forget, tainted
  replacements
- [Read the live state](#read-the-live-state-before-trusting-the-code)
- [Release contract](#release-contract), [New log-based metrics](#new-log-based-metrics),
  [Capacity caps](#capacity-caps),
  [Database and backup hosts](#database-and-backup-hosts), [Trust boundaries](#trust-boundaries),
  [Recovery](#recovery)
- [Add a service](#add-a-service): the pull-request order, the first plan, native backups
- [Report faults to Sentry](#report-faults-to-sentry): the DSN, the release name, the egress

## Prove the plan before review

- **Pure refactors must plan to zero changes.** Locally, run
  `tofu plan -refresh=false -lock=false` against the real backend. It compares code with state
  without calling the APIs, so it works under a human account that cannot read the workload
  projects.
- **Data sources the account cannot read** go in a scratch copy, stubbed with the literal value
  from state. Never stub them in the committed code.
- **Without backend access,** diff `tofu test -verbose` between an `origin/master` export
  (`git archive origin/master | tar -x`) and the branch. It prints every run's planned values under
  the mocked providers, rendered templates included, and its output is deterministic.
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
- **Changing an IAM binding's condition replaces the binding.** Its expression, title and
  description are all force-new, so the plan deletes the binding and needs the label. To grant a
  role on one more invocation class, add a second binding conditioned on that class: it plans as an
  addition, as `foundation_maintenance_updates` did in a-novel/infra#962.
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
  When the writer starts managing a new resource type, or takes an action it never took before,
  add the permissions in foundation first.
- **A resource whose creation failed halfway is tainted, and its replacement destroys it first.**
  OpenTofu orders that destroy before an update to any resource its state recorded as a dependency
  at creation, whatever the configuration says now. A permission granted in the same change through
  one of those resources arrives too late: the destroy fails, and so does every re-run. Grant it
  through a role outside the recorded dependencies. Never make a resource `depends_on` an IAM role
  or member to order one creation: the edge then binds every later replacement.
- **A newly enabled API's service agent may not exist yet.** Google creates it lazily, and Workflows
  refused the first workflow with `service agent does not exist`. Create it with
  `google_project_service_identity`, on `google-beta`, and grant its `serviceAgent` role before the
  resource that needs it, as `modules/project-shell` does.

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
   - **A failed execution still records its token,** so the next plan shows nothing to do. Every
     plan runs `.github/scripts/rerun-failed-jobs.mjs`, which reads each token job's last
     execution from the Cloud Run v2 API and re-plans with `-replace` on the token source of one
     that failed. The provider does not expose the execution's status.
2. Traffic is `TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST`. Cloud Run routes to a revision only after its
   startup probe passes.
3. Post-deploy checks run one script, `modules/smoke-check/smoke.sh`:
   - each private service's `smoke-check` module runs it in `grpc` mode, as the service's smoke
     identity, on every new gRPC image, script or identity, which are triggers, and again once a
     downtime clears;
   - the `health` action runs it in `http` mode on Authentication's `/v2/healthcheck`, after each
     deploy and from `drift.yaml`, with the ID token its caller passes as `id_token`;
   - it retries every five seconds for two minutes, since a fresh invoker grant takes that long to
     apply, then prints the last exit code and answer. A deadline, not an attempt count, bounds it,
     so slow attempts cannot outlast the job;
   - it skips once a planned downtime has started, and re-tests the start when it fails, since a
     check begun just before `start` fails when the services stop.
4. Nothing rolls a release back. A pull request that lowers a pinned version fails its plan, so a
   bad release is fixed forward, through a hotfix when the service's `master` carries unreleased
   work (`manage-versions`). Migrations stay backward compatible; data rollback is a recovery,
   never part of a deploy.
5. The pins the last apply deployed come from the plan's `output_changes.<output>.before`. The
   plan's `prior_state` already holds the new outputs, so it cannot tell a changed pin apart.

## New log-based metrics

Cloud Monitoring accepts a new log-based metric's type up to about ten minutes after the metric is
created. An alert policy created in the same apply fails: a threshold condition answers 404, and a
PromQL condition answers `400 … PromQL metric(s) are invalid`. `depends_on` cannot help, because the
metric resource itself succeeds at once.

- **A new log-based metric lands in its own pull request,** deployed before the one that adds the
  alerts that query it. A renamed metric is a new metric.
- **When the two must ship together,** expect the apply to fail on the alert policies. Ten minutes
  after the metric was created, re-run the failed job:
  `gh run rerun <run ID> --repo a-novel/infra --failed`. Everything else in that apply has landed.

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
- **Database host changes wait for a planned downtime.** Any byte change under
  `modules/database-runtime/files/`, and any reordering of its `local.files`, replaces the host
  template. `policy.jq`
  flags a database host's template replacement or group update, which is not a deletion. Its pull request needs
  `allow-downtime`, and `DOWNTIME` must list `service-<service>.database` with a `start` already
  passed. Ask for a full backup before the downtime starts.
- **Planned downtimes come only from the admin-only `downtime.yaml`,** which writes `DOWNTIME`. A
  full deploy applies it once the run ends, but only when the run's `downtime` job succeeded: a run
  that failed before writing deploys nothing. Listed services refuse work from `start` until
  someone clears it; `end` only informs users.
- **A downtime post's color follows its headline.** Once `start` has passed, every post that reads
  "Expected back" shows the in-progress color, whatever change it announces.
- **Users see a downtime whenever a deploy can enforce one.** Any deploy applies `DOWNTIME`, so a
  schedule publishes `downtime.json` before writing it, and a clear deletes it before publishing.
- **The deploy's `rollout` job rolls hosts from observed state, not the diff,** so a cancelled or
  force-merged deploy loses nothing:
  - a database host off its group's target rolls only during its service's downtime, once the
    service's serving revision carries the downtime's start, after a snapshot of its data disk.
    Until then the deploy warns "rollout pending", or fails when the service missed the downtime.
    It restarts in place or is replaced; the stateful disk and IP stay. The rollout then waits up
    to 10 minutes for the new boot's `agora/database-release` guest attribute to read `healthy`.
    A stable group proves only that the VM runs: a startup that failed closed leaves PostgreSQL
    stopped;
  - a backup repository VM restarts when its `config` label differs from `running-config`, after
    its database host;
  - `roll-database.yaml` forces both, whatever the downtime.
- **Each rollout's snapshot is kept 14 days,** labelled `role=pre-rollout`. It is recovery data,
  never an undo: a faulty host change is fixed forward by a new pull request
  (`docs/runbooks/database.md`). The data disk still ignores its source snapshot, so a disk ever
  recreated from one plans no replacement.
- **Database images are pinned** by `tag@digest` in foundation and in the service root. Both pins
  copy the image into the service's `agora-<service>-private-production`, the only repository its
  host pulls PostgreSQL from. The hosts use the digest alone, so the pins ride with the service's
  release pull request: a release without a database change keeps the reproducible digest and plans
  nothing. A new digest replaces the host template, so that release merges during a planned
  downtime.
- **`host-credentials` is pinned once,** in foundation's `host_credentials_image`, as the
  `tag@digest` `publish-rollout-verifier.yaml` published. Deploy verifies it against that workflow
  and copies it into each service's tooling repository; the hosts and the service roots derive the
  digest reference. A new pin is a database host change.
- **Each backup repository VM keeps a reserved address,** taken from foundation's
  `native_backups.<service>.repository_ip`, the same value its database host trusts.
- **Services connect as `<database>_app`,** which owns schema `public` without superuser rights.
  Each boot of the host converges that role and hands it what a superuser created. The owner
  `<database>` has no password and logs in only through the host's local socket, so a leaked
  service password reaches no shell.
- **A database password rotation** bumps foundation's `app_password_version` and the service's
  `app-password` in one pull request, merged during a planned downtime, which rolls the host.
- **pgBackRest owns backup expiry.** Never add age-based deletion to a backup bucket. Never delete
  WAL to free space.

## Trust boundaries

- **Pull-request plans run the candidate's code and providers with the read-only identity.**
  - GitHub issues no OIDC token to fork pull requests.
  - Renovate delays OpenTofu and provider updates by 7 days (6 hours for patches) to bound a
    compromised release.
  - State holds no secret values, because those are added outside OpenTofu.
- **Only admins bypass the rulesets.** A merge to `master` deploys production, and every bot
  reaches it through a pull request. The Agent and Dependencies keys live only in each repository's
  `governance` environment, filled from Bitwarden by `a-novel repo update`. That environment admits
  the default branch, plus guarded release lines and tags. Every repository's default-branch
  workflows can read the keys, so no bot may push `master` directly. The `infra` class sets
  `rulesets.admins_only`. A bot's pull request merges alone only as a release pin, a version update
  Renovate armed, or a refresh of Google's service ranges to exactly what Google publishes. A pin
  deploys only an image of an immutable Release the release App published, built by that tag's own
  run.
- **Internet egress leaves through Cloud NAT, past the egress allowlist.** A rule names a host by
  FQDN only when every client resolves it alike, as Cloudflare's anycast does for `api.openai.com`.
  The firewall admits only the addresses its own lookup returned, and Google's domains answer each
  client location with another frontend: an FQDN rule for `smtp-relay.gmail.com` dropped most of
  Authentication's mail (a-novel/infra#1033). Match Google's hosts with the `google-services` address
  group, Google's ranges minus its Cloud customers', which `refresh-google-ranges.yaml` keeps
  current. A refused connection times out without a log, and a health probe can pass by luck, so
  prove an allowlist change with a real call from the workload, such as an email.
- **A fresh Direct VPC egress instance waits for Cloud NAT.** Its first connection out can wait 30
  seconds or more, as Google documents, while internal addresses answer at once. A warm service
  never meets the wait, and a job always does. Give a job's outbound budgets room for it, inside its
  task timeout: Authentication's maintenance job sends its mail within `SMTP_TIMEOUT = 300s`
  (a-novel/infra#1123).
- **DNS resolves only the allowlist.** The response policy `a-novel-dns-allowlist` resolves the
  private zones, Compute Engine's internal names and the egress allowlist's hosts. Every other name
  answers with a CNAME to `blocked.internal.` and no address, so no outside name server receives the
  lookup (a-novel/infra#1016).
  - A host joins both allowlists through its one `egress_allowlist` entry. Removing it deletes
    rules, so that pull request needs the deletion label.
  - A wildcard stops at any level that another rule names, as RFC 4592 wildcards do, so foundation
    refuses every level above an allowed name. The bypass rules need `google-beta`.
  - The `dns_query` logs show each lookup and its answer. A Cloud Run entry carries no source
    address, so the logs name what was resolved, not which workload resolved it.
- **Google API calls stay inside the `agora_production` perimeter,** a VPC Service Controls
  perimeter around the private and public-api projects, in foundation's `service-perimeter.tf`. Its
  spec runs in dry run until a-novel/infra#1042 enforces it.
  - Its restricted services are every API either project enables, plus Secret Manager, and their VPC
    reaches only those. A pull request that enables an API adds it to the list, or foundation's
    test fails.
  - CI's three identities and the operators enter from any source. Any identity inside may call
    Secret Manager and Cloud Storage in management, and only there. A new call that crosses the
    perimeter needs a rule in the same pull request.
  - IAP's tunnel and Cloud Run's `run.app` invocations reach the perimeter without the caller's
    identity, so no identity rule admits them. The tunnel stays unrestricted, and a service with
    ingress `all` skips the invocation check.
  - Each project's `cloudaudit.googleapis.com/policy` log holds the violations, and in dry run the
    calls the spec would refuse. Prove a perimeter change with a real call: from a database host, a
    read of a public bucket outside logs one.
  - Management stays outside, since its Workflows call Discord and GitHub, which Workflows inside a
    perimeter cannot reach.
- **Organization guardrails are set by hand,** from the setup runbook: the private project's Cloud
  Run policies, `run.allowedVPCEgress` on both workload projects, the plan identity's deny policy,
  and the access policy with its two grants. Organization Administrator holds none of their roles,
  so an admin first grants themselves Access Context Manager Admin, Organization Policy
  Administrator or Deny Admin. `gcloud` bills a user's calls to their configured project, which
  needs each API it calls enabled.
- **The writer's federation condition** pins `deploy.yaml`, `roll-database.yaml` and
  `recovery.yaml` on `master` behind the `production` environment. Changing it is a bootstrap change, applied by the last trusted
  workflow before a new one depends on it.
- **`infra-downtime` trusts only `downtime.yaml` on `master`.** It starts, lists and cancels the
  `downtime-announcements` executions. Workflows grants that project-wide, so it could start
  `github-dispatch` too, which therefore refuses any workflow its schedules do not list. Both
  `downtime.yaml` jobs gate on the admin role, since "Re-run this job" skips the first one.
- **`infra-health` trusts only `drift.yaml` on `master`, never a pull request.** It can read and
  invoke Authentication's REST service, for the scheduled health check, and nothing else. Pull
  requests can mint the read-only identity, so that identity never calls a service.
- **The `github-dispatch` clock holds the only GitHub credentials in Google Cloud:** one
  fine-grained token per org, limited to Actions on that org's `.github` repo, which only
  `github-dispatcher` reads. `github-dispatch-clock` holds `workflows.invoker`, which Workflows grants
  project-wide, so it could start any management-project execution. Rotate a token by adding a
  version and bumping `github_dispatch_token_versions`.
- **The read-only identity trusts only pull requests into `master`.** A stacked pull request's plans
  fail with `rejected by the attribute condition`. Once its parent merges, GitHub retargets it to
  `master`; rebase it onto `master` to drop the parent's squashed commit, and its plans run.
- **Cloud Workflows can record what a workflow reads.** Detailed execution history keeps variables
  and call logs keep call results, so a workflow that reads a secret pins
  `call_log_level = "LOG_NONE"` and `execution_history_level = "EXECUTION_HISTORY_BASIC"`.
  Bootstrap's test checks both for every workflow it deploys.
- **A service that trusts a forwarded visitor header requires IAM.** Its invokers are only the
  platform and the health checks after each deploy and from drift, which send an ID token for the
  service's custom audience.
- **Only named humans run a maintenance job.** Authentication's `agora-authentication-maintenance`
  is the model (a-novel/infra#961, #962):
  - the service root defines it with a pinned image and no `run_execution_token`. Its stored
    definition names no operation, since the image's default only prints help, and it never
    retries, so a failed run returns to the operator;
  - foundation tags it `maintenance`. The deploy identity may update jobs of that class, so a
    release moves the image, but never runs them;
  - the principals in `authentication_maintenance_principals` hold
    `roles/run.jobsExecutorWithOverrides` on that class alone, because only an execution's argument
    overrides name the operation. `run.viewer` and `logging.viewer` let them follow the run;
  - `docs/runbooks/setup.md` gives the `gcloud run jobs execute … --args=…` command.
- **Inventory IAM by principal and resource:**
  - each workload runs as its own identity and reads only the secrets it mounts: the runtime
    account is the main server's, and `service-runtime`'s `workloads` map creates one account per
    job, sweep or smoke check;
  - database and repository hosts pull only their service's images. `service-runtime` grants the
    database host's pull, so a host without native backups pulls too. Genai lost its database for
    over an hour on 2026-10-18 when that grant lived in the backup module;
  - humans get no write access in workload projects; the named operators only run the maintenance
    job.

## Recovery

Restores go into a disposable `a-novel-recovery-<id>` project, never production. `recovery.yaml`
plans or applies the drill host from one JSON input; the runbook covers project creation, the
temporary grants, the restore units and cleanup. A successful backup proves nothing until a drill
restores it and its SQL checks pass.

## Add a service

A private service with a database ships in six pull requests, each merged once the previous one has
deployed. a-novel/infra#785 is the worked example.

1. **Bootstrap** creates the secret containers. Add version 1 of each value before a later step pins
   it: the database host reads the application role's `app-password` at boot.
2. **The service root starts with its `runtime` module alone,** its workloads' identities
   included, plus its `plan-<root>` job and `roll-database.yaml` option, and foundation's
   `service_release_zones` entry and quotas. `deploy.yaml` and `drift.yaml` find the root on disk;
   `lint-repository` names what is missing. The deploy copies a root's `image_copies` into Artifact
   Registry before it applies, into the repositories `service-runtime` creates. Foundation copies
   the database image into the service's `agora-<service>-private-production`, so it pins that
   image only once this step has deployed.
3. **Foundation adds the database host.** A new host starts at its target template, so it needs no
   downtime.
4. **The root creates its jobs without a `run_execution_token` and its services without invokers.**
   Name every Cloud Run service that writes to its database in the roll action: the host rolls only
   once all of them have taken the downtime. The rollout skips a service until its host exists.
5. **Foundation binds the workloads' invocation tags,** grants the scheduler and creates the
   schedules.
6. **The root sets the execution tokens and grants its invokers.** A private gRPC service adds its
   `smoke-check` module here: the module grants its invoker and runs its job at once.

- **In the private project, the deploy identity can create a workload but not run it.**
  `infraFoundationPrivateProvisioning` grants creating Cloud Run services and jobs and Cloud
  Scheduler jobs, binding tags to them and reading both back, without a condition: neither API
  supports resource-name conditions, and no tag exists before creation. Updating, running and
  invoking need the invocation tag foundation binds in step 5.
- **Step 5 binds tags and creates schedules,** which no pull-request plan can prove:
  - a binding also takes `tagUser` on the tag value, which `foundation_application_tags` grants;
    the project's `tagAdmin` does not include it;
  - the provider resumes every Cloud Scheduler job it creates, which takes
    `cloudscheduler.jobs.enable`. `infraFoundationScheduler` holds it, with the `delete` that
    replaces a schedule whose creation failed.
- **The new root's first plan fails.** OpenTofu's GCS backend creates a missing state under a lock
  even with `-lock=false`, and the plan identity only reads the state bucket, so
  `storage.objects.create` is denied on `<root>/default.tflock`. The deploy creates the state on
  merge. `plan-<root>` is not a required check until `a-novel repo update`, and plan jobs skip the
  merge queue, so the failure blocks nothing. List `a-novel repo update` in the handoff.
- **The database host scripts are shared.** `modules/database-runtime`'s `startup.sh` and
  `shutdown.sh` read the service, port and database name from foundation's template metadata
  (`agora-database-service`, `agora-database-port`, `agora-database-name`), so a new service changes
  neither. Any change to them is a [database host change](#database-and-backup-hosts) for every
  service.
- **Native backups enroll after the first deploy.** Until then, the roll action skips the missing
  repository VM. Each step needs the one before:
  1. `host-credentials`, `native-restore` and the recovery SQL accept the service. Publish them,
     then pin `host-credentials`, a database host change for every service.
  2. The root's `backup-repository` module creates `agora-pgbr-<service>` and the repository VM.
  3. Bootstrap adds the pgBackRest bucket, roles and TLS secrets, which bind that account. Then add
     the TLS certificates by hand.
  4. Foundation adds `native_backups.<service>` during the service's planned downtime. Then take a
     full backup and run a recovery drill.

## Report faults to Sentry

A workload reports its faults to Sentry once it holds three variables. Migrations and smoke checks
hold none and report nothing. The change takes three pull requests, each merged once the one before
has deployed.

1. **Bootstrap creates the DSN container,** `production-<service>-sentry-dsn` with the contract
   `SENTRY_DSN`. The same pull request grants each reporting workload's identity read on it, through
   `service-runtime`'s `secrets` or the workload's `workloads` entry, and pins
   `secret_versions.sentry-dsn`. Cloud Run checks a revision's secret access when it creates the
   revision, and a fresh grant takes minutes to apply, so the grant deploys before anything mounts
   the secret. Ops then add the project's DSN as version 1, as `docs/runbooks/secrets.md` shows.
2. **Foundation admits the workload's identity** in `egress_allowlist["sentry"]`, the rule for
   Sentry's ingest host.
3. **The root wires each reporting workload:**
   - `SENTRY_DSN` mounts the pinned `sentry-dsn` version;
   - `SENTRY_ENVIRONMENT` is `production`;
   - `SENTRY_RELEASE` is `<repository>@${trimprefix(local.images.<image>.tag, "v")}`, the name
     release CI gives the release in Sentry, so it moves with every release pin. The root's
     `tofu test` asserts that each reporting workload's value matches
     `^<repository>@[0-9]+\.[0-9]+\.[0-9]+$`;
   - its `vpc_access` sends `ALL_TRAFFIC` from the egress subnet,
     `local.foundation.network.egress_subnet_id`, where Cloud NAT and the allowlist decide what
     leaves.
   - a server a consumer waits on, directly or through another service, sets `cpu_idle = false`,
     and the root's `tofu test` asserts it. That covers a public API and every server it calls.
     sentry-go and OpenTelemetry send from background goroutines after the response, and
     request-based billing gives an instance CPU only while it serves a request, starts or stops.
     Under sparse traffic, a send can stall past its deadline, and the event is then lost.
   - a server no consumer waits on, such as one a scheduler calls, keeps request-based billing and
     sends its telemetry before each answer through golib's `httpf.SendTelemetry`. CPU between its
     calls would only keep it billed, and frequent calls keep its instance alive around the clock.

Cloud Run reserves a workload's egress-subnet addresses 16 at a time, so check that foundation's
`egress_subnet_cidr` still fits every workload there. A subnet's range grows in place but never
shrinks. A job there can start without a network for a minute or more. golib gives each event
`sentryf.SendTimeout`, 2 minutes, to leave, and the job's flush outlasts it, so an event captured
before the network came up still arrives.

### Send traces and logs to Sentry

The workloads that report to Sentry also export their traces and logs to the same project over
OTLP/HTTP. Each root adds four variables to the reporting workloads' Sentry env:

- `OTEL_EXPORTER_OTLP_ENDPOINT` is
  `https://${local.foundation.sentry_ingest_host}/api/${var.sentry_project_id}/integration/otlp`,
  from foundation's output and the root's `sentry_project_id` input. The SDK appends `/v1/traces`
  and `/v1/logs`.
- `OTEL_EXPORTER_OTLP_HEADERS` mounts `production-<service>-sentry-otlp-headers` at a pinned
  version. It holds `x-sentry-auth=sentry sentry_key=<public key>`, with the DSN's public key.
- `OTEL_EXPORTER_OTLP_COMPRESSION` is `gzip`, and `OTEL_SERVICE_NAME` is the repository name.
- `OTEL` is `true`. A service exports only when both `OTEL` and the endpoint are set.

The header container follows the DSN's steps: its grant deploys before anything mounts it, and ops
add version 1 before the pin merges. Egress needs no change, since the OTLP endpoints share the
ingest host. The perimeter keeps restricting the Telemetry and Cloud Trace APIs, so no workload can
export to another project's.

Foundation's single `sentry_ingest_host` input names the ingest host that every DSN and OTLP
endpoint points to. A move to another Sentry organization changes that input, each root's
`sentry_project_id`, and the DSN and OTLP header versions, and nothing else here.
