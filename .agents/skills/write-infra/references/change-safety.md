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
- [Report faults to Sentry](#report-faults-to-sentry): the DSN, the release name, the egress, the
  OTLP export, [the Sentry root](#the-sentry-root) and its tokens and organization moves, and
  [each release's deploy](#sentry-deploys)

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
- **A failed Cloud Run update still records its configuration in state.** The provider saves the
  planned values when the wait fails, so a rerun plans no changes and passes, while the service
  keeps serving its previous revision. After such a failure, fix the cause, then make sure the next
  apply changes that resource, as a release pin does. A green rerun alone proves nothing.
- **A `kubernetes_manifest` whose wait hits a request deadline reaches no state.** When `wait` ends
  on `context deadline exceeded` instead of `timed out waiting on conditions`, the object exists in
  the cluster but not in state, and the resources that `depends_on` it are never created. The next
  apply then fails creating it. Adopt it through an `import` block whose `for_each` reads a set
  input, as the cluster root's `adopted_databases` does, and empty the input once the import has
  applied. The root's tests leave it empty, since
  [mocked providers cannot import](tooling-and-tests.md#tests-that-earn-their-place)
  (a-novel/infra#1229, #1247, #1248).
- **A `kubernetes_manifest` lists in `computed_fields` every map that its operator's webhook
  fills.** The provider plans a map with exactly the keys the manifest sets. A key the webhook adds
  fails the apply with `Provider produced inconsistent result after apply`. The list replaces the
  provider's default, so keep `metadata.labels` and `metadata.annotations` in it: the databases add
  CloudNativePG's `spec.postgresql.parameters` beside them (a-novel/infra#1249).
- **A Kubernetes Job's pod template is immutable, and an updated Job never runs again.** An input
  that must re-run a Job goes in the Job's name, so that a change replaces the Job: the cluster
  root's migrations Job ends its name with a hash of the database lineage, after its image digest
  (a-novel/infra#1376). A template annotation, as the servers carry for the lineage, plans as an
  in-place update that Kubernetes refuses.

## Retire protected resources

- **Unlock in one apply, retire in the next.** The provider reads `deletion_policy` and
  `force_destroy` from state, so a resource whose state says `PREVENT` refuses to be destroyed
  even after its block is gone. Both pull requests need the deletion label.
- **One instance of a protected `for_each`:** `moved` it to an address outside the configuration,
  plus a `removed` block (`destroy = true`, or `false` to forget). `prevent_destroy` stays on the
  other instances.
- **That move drops the instance's dependency edges.** The resources that reference it no longer
  wait for it: a service account can be deleted before the grants that name it, and an Artifact
  Registry repository before its IAM members (a-novel/infra#1391). Destroy the dependents first, in
  an earlier pull request.
- **An Artifact Registry repository deleted before its grants blocks the root.** The provider drops
  a resource from state on a 404, but Artifact Registry answers 403 for a missing repository.
  Every refresh of those grants then fails, under a `removed` block too, and only a `tofu state rm`
  of each grant, by ops, frees the root (a-novel/infra#1394).
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
- **An `ignore_changes` entry can hide a missing permission.** Removing it makes OpenTofu call the
  APIs that set that attribute, which it never called before. a-novel/infra#1370 stopped ignoring a
  Scheduler job's `paused`, and the next deploy failed on `cloudscheduler.jobs.pause`
  (a-novel/infra#1377). Before removing one, check the writer's role for those calls.
- **A permission granted in an apply may not hold yet later in that apply.** IAM takes seconds to
  minutes to propagate, and OpenTofu calls the next API at once. a-novel/infra#1377 added
  `cloudscheduler.jobs.pause` to the writer's role, and the deploy's pause, 1.5 seconds later, was
  refused. A new `google_project_iam_custom_role` likewise fails a grant in the apply that creates
  it, with `does not exist in the resource's hierarchy` (a-novel/infra#1226). Expect the deploy to
  fail once, and hand the developer the re-run of its failed jobs. Add no `depends_on` for this:
  it orders the calls, and waits for no propagation.
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
- **Artifact Registry's VPC-SC setting `depends_on` a repository in its location.** A location has
  no setting until the project holds a repository there, so the first apply in a new region
  answered 404 (a-novel/infra#1230, #1233).
- **Cloud Scheduler's first jobs in a new region can fail with
  `Error 409: Failed to perform tenant project creation`.** Google is still creating its tenant
  project for that region. Re-run the failed deploy job (a-novel/infra#1230).

## Read the live state before trusting the code

Production can differ from `master` even when drift is clean. One example: a VM's boot metadata was
rendered by an older template, and the current code would push a flag its pinned loader does not
understand. Compare the planned diff with the image or binary that will consume it, and fix the
pins in the same change.

Foundation's cluster sends only its system components' logs to Cloud Logging, so workload logs stay
in the cluster. A failed cluster deploy's `Describe the databases` step prints each database's unmet conditions, stuck pods
and last warnings. To read further, open a draft pull request whose `plan-cluster` job reads the
pods, their logs and the custom resources from the API server as the plan identity. Close it
unmerged once read (a-novel/infra#1246, #1251).

## Release contract

1. Migrations run during apply through the job's `run_execution_token`. The Cloud Run service
   `depends_on` that job.
   - **A token must never repeat.** Cloud Run names an execution after its job and token, and
     refuses a name it has used: the apply waits on "already exists" until it times out.
   - **Derive it from a `terraform_data` its inputs replace,** never from the inputs alone. A hash
     repeats when they come back, as on a revert or a cleared downtime. The plan policy exempts
     that replacement.
   - **A failed execution still records its token,** so the next plan shows nothing to do. Every
     plan runs `.github/scripts/rerun-failed-workloads.mjs`, which reads each token job's last
     execution from the Cloud Run v2 API and re-plans with `-replace` on the token source of one
     that failed. The provider does not expose the execution's status.
2. Traffic is `TRAFFIC_TARGET_ALLOCATION_TYPE_LATEST`. Cloud Run routes to a revision only after its
   startup probe passes.
   - **A failed revision still records its template,** and the previous revision keeps serving. So
     every service's template carries a `revision-token` annotation set from its own
     `terraform_data`, a new service's included. The same script reads each service's
     `terminal_condition` from the refreshed plan, and replaces the token source of one in
     `CONDITION_FAILED`, so the deploy rolls the template again and fails until it serves.
   - **A successful apply therefore means every Cloud Run service of the root serves its
     template.** The apply waits for each service it changes, and a failed unchanged one would
     have been rolled again.
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
   - **A release pin that merges while a branch is open turns the branch's pins into downgrades.**
     The branch's plans then fail with `… lowers <image> from <version>`. Rebase the branch onto
     `master`.
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
- **GKE node boot disks and the databases' volumes share one SSD quota.** The nodes' pd-balanced
  boot disks and the PersistentVolumes all count against the region's `SSD_TOTAL_GB`, 500 GB by
  default. Size them together. A replica whose volume cannot be provisioned leaves its database
  never `Ready`. Foundation's `cost_cap` raises the quota through a Cloud Quotas preference, which
  takes effect only once Google grants it.
- **A service's postcondition checks its cap against the quota:** maximum instances plus one
  rollout instance, times its CPU, must fit. A plan that raises the cap fails until foundation
  raises the quota.
- **A service with in-memory rate limits** keeps the instance count its limits assume. Raising it
  is a design change, not a tuning one.
- **A gRPC workload's call cap** (`GRPC_MAX_CONCURRENT_CALLS`) sits beside its `GOMEMLIMIT` in the
  cluster root, so a change to one is reviewed with the other: the cap times one call's peak memory
  fits the memory limit. `cluster-workload` makes a gRPC workload's Service headless, so callers
  balance their calls across its pods; an HTTP workload keeps its ClusterIP.
- **A service whose calls mostly wait scales on its calls in flight, beside CPU.** CPU stays low
  on its full pods. `cluster-workload`'s `scaling_gauge` reads the pods' `grpc_server_calls_in_flight`
  on their metrics port through an `AutoscalingMetric`. The autoscaler keeps it at 60% of
  `GRPC_MAX_CONCURRENT_CALLS` per pod, Cloud Run's default target, and CPU at 70%. It absorbs
  bursts only: `replicas.max` times the call cap bounds the calls the service runs at once, and so
  the load it sends downstream.

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
- **A downtime's GKE maintenance exclusion lands only when foundation applies.** The exclusion
  holds node upgrades through the window. When a merge lands while the downtime's full deploy waits
  behind another deploy, the merge's deploy replaces it, and applies foundation only when
  foundation changed. Check that foundation applied after the downtime was written. Otherwise an
  admin dispatches `deploy.yaml`, which applies every root, before `start`.
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
- **A backup bucket keeps Object Versioning beside its retention policy.** Retention refuses to
  replace a live object, and versioning lets the replaced version turn noncurrent and stay retained.
  pgBackRest rewrites its info files, and Barman rewrites `backup.info` from `STARTED` to `DONE`
  (a-novel/infra#1252).
- **CloudNativePG's Barman Cloud plugin needs three permissions beyond object access,** which
  bootstrap's `barmanWriter` grants per bucket:
  - `storage.buckets.get`: `barman-cloud-wal-archive --test` reads the bucket before archiving. It
    counts any API error as lost connectivity, so a 403 exits 2;
  - `storage.multipartUploads.create` and `.abort`: base backups upload through Cloud Storage's XML
    multipart API, and WAL as single objects. Without them, WAL archives while every base backup
    fails with a 403 (a-novel/infra#1248, #1252).
- **Check the cluster's DNS first when Barman reports `Project was not passed` or exits with
  status 4.** Its sidecar takes the Workload Identity project and token from
  `metadata.google.internal`, so a [DNS policy fault](#trust-boundaries) shows here first. A
  `GOOGLE_CLOUD_PROJECT` variable moves the failure to the token without curing it
  (a-novel/infra#1247).

## Trust boundaries

- **Pull-request plans run the candidate's code and providers with the read-only identity.**
  - GitHub issues no OIDC token to fork pull requests.
  - Renovate delays OpenTofu and provider updates by 7 days (6 hours for patches) to bound a
    compromised release.
  - State holds no secret values, because those are added outside OpenTofu.
- **Only admins bypass the rulesets.** A merge to `master` deploys production, and every bot
  reaches it through a pull request. The Agent and Dependencies keys live only in each repository's
  `governance` environment, which the GitHub root's secret sync fills from Secret Manager. That
  environment admits the default branch, plus guarded release lines and tags. Every repository's
  default-branch workflows can read the keys, so no bot may push `master` directly. The `infra`
  class sets `rulesets.admins_only`. A bot's pull request merges alone only as a release pin, a
  version update Renovate armed, or a refresh of Google's service ranges to exactly what Google
  publishes. A pin deploys only an image of an immutable Release the release App published, built by
  that tag's own run.
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
- **The cluster's default-deny policies admit DNS to the `kube-system` pods whose `k8s-app` is
  `kube-dns` or `node-local-dns`.** GKE Standard turns NodeLocal DNSCache on by default since
  1.34.1-gke.3720000: each node's `node-local-dns` pod answers the queries sent to kube-dns'
  address. A peer that admits only `kube-dns` times out every lookup with `Errno -3`. The cluster
  root's test requires both values (a-novel/infra#1247).
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
  `master` through an `edited` event, which `main.yaml` ignores. Rebase it onto `master` to drop
  the parent's squashed commit, then push: a push runs its plans, and so does a reopen.
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
  merge. `plan-<root>` becomes a required check only once the GitHub root reads the merged
  `main.yaml`, and plan jobs skip the merge queue, so the failure blocks nothing.
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
hold none and report nothing. The service's Sentry project comes first: add its repository to the
Sentry root's `projects`, as [The Sentry root](#the-sentry-root) describes, and merge that alone. A
service root cannot read an output the Sentry root has not applied. The rest takes two pull
requests, each merged once the one before has deployed.

1. **Foundation admits the workload's identity** in `egress_allowlist["sentry"]`, the rule for
   Sentry's ingest host.
2. **The root wires each reporting workload.** It reads the Sentry root's
   `services["<repository>"]` output into `local.sentry` through `terraform_remote_state`, the way it
   reads foundation's outputs, and sets:
   - `SENTRY_DSN` to `local.sentry.dsn`, a plain value. A DSN is not a secret: Sentry documents it
     as safe to expose, and its public key only lets a sender submit events;
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

   Its tests mock the Sentry root's state with an `override_data` on
   `data.terraform_remote_state.sentry`.

Cloud Run reserves a workload's egress-subnet addresses 16 at a time, so check that foundation's
`egress_subnet_cidr` still fits every workload there. A subnet's range grows in place but never
shrinks. A job there can start without a network for a minute or more. golib gives each event
`sentryf.SendTimeout`, 2 minutes, to leave, and the job's flush outlasts it, so an event captured
before the network came up still arrives.

### Send traces and logs to Sentry

The workloads that report to Sentry also export their traces and logs to the same project over
OTLP/HTTP. Each root adds these variables to the reporting workloads' Sentry env:

- `OTEL_EXPORTER_OTLP_ENDPOINT` is
  `https://${local.foundation.sentry_ingest_host}/api/${local.sentry.project_id}/integration/otlp`,
  from foundation's and the Sentry root's outputs. The SDK appends `/v1/traces` and `/v1/logs`.
- `OTEL_EXPORTER_OTLP_HEADERS` is `local.sentry.otlp_headers`, a plain
  `x-sentry-auth=sentry sentry_key=<public key>` with the DSN's public key. The Go exporter splits
  it on the first `=`, so the space is fine.
- `OTEL_EXPORTER_OTLP_COMPRESSION` is `gzip`, and `OTEL_SERVICE_NAME` is the repository name.
- `OTEL_RESOURCE_ATTRIBUTES` is `a_novel.workload=api` on a workload that serves requests, and
  `a_novel.workload=job` on one a person or a schedule starts, a scheduler-called server included.
  The root's `tofu test` asserts each workload's value. Sentry stores an OTLP resource attribute
  under a `resource.` prefix, so it searches this one as `resource.a_novel.workload`.
- `OTEL` is `true`. A service exports only when both `OTEL` and the endpoint are set.

Egress needs no change, since the OTLP endpoints share the ingest host. The perimeter keeps
restricting the Telemetry and Cloud Trace APIs, so no workload can export to another project's.

A platform's server sends its spans over OTLP: its root sets these variables, egress and `cpu_idle`
like a service's, but no `SENTRY_*` variable, since no Sentry SDK runs on the server. The root also
sets `PUBLIC_SENTRY_DSN` to its project's DSN, which the browser's SDK reads once the visitor
consents.
nodelib-server reads the same variables, `OTEL_RESOURCE_ATTRIBUTES` included, since v2.2.1.

Foundation's single `sentry_ingest_host` input names the ingest host that every DSN and OTLP
endpoint points to. A move to another Sentry organization changes that input and the Sentry root,
in one pull request after the old resources are forgotten, as [The Sentry root](#the-sentry-root)
describes.

### The Sentry root

`environments/production/sentry` manages the Sentry organization through the community
`jianyuan/sentry` provider: the projects and their spike protection, then the alerts and dashboards.
It outputs `services[<repository>] = { project_id, dsn, otlp_headers }`, read from each project's
first client key by the `sentry_key` data source. A new project's key is read in the apply that
creates it.

- **It applies only when it changes,** in `deploy.yaml`'s own `sentry` job after bootstrap, never in
  the services matrix. Every service root applies on every merge, and a failed one stops the
  rollout, so a Sentry API outage there would hold every release. `check-service-roots.sh` skips it
  as it skips foundation. The services job waits for it: it runs only when the root changes, so a
  release never waits on Sentry, while a key or organization change reaches the services in the
  same deploy. The services still run when it fails, on the outputs it last applied. A deploy
  re-runs every root changed since the last successful one, so `!failure()`, which counts every
  ancestor job, would let one rejected Sentry change hold every release. `rollout` checks
  `needs.services.result` for the same reason.
- **Alerts post to Discord.** Each service has three metric monitors on its server spans
  (`is_transaction:true`, on the `events_analytics_platform` dataset): failing share, p95 duration,
  and telemetry stopped, a `count() < 1` over a window longer than the gap between its steady span
  sources. Their thresholds are the root's `alerts` input. One `sentry_alert` posts every monitor's
  issue to the channel in its `discord` input, through the Discord integration installed on the
  server by hand. Four dashboards cover every service, one per concern: requests, failing share,
  p95 duration and error events. Each span widget draws one line per service, and requests and job
  runs get a widget each, split on the workload label; the latency monitor reads requests only.
  A plan sends none of these to Sentry's API, so the first apply is the test of a new field, and a
  wrong search key shows only as empty widgets.
- **Its token never reaches state or a plan.** The root reads it through the Google provider's
  ephemeral `google_secret_manager_secret_version`. The `tofu` action passes `TF_VAR_writer`, so
  plans read `production-sentry-plan-token`, the only one bootstrap lets `infra-plan` read, and only
  deploys read `production-sentry-deploy-token`. Both are Sentry internal-integration tokens:
  organization tokens have fixed CI permissions and cannot manage projects.
  `docs/runbooks/secrets.md` lists their permissions.
- **Mocked providers cannot import,** so a root that adopts existing resources gates its `import`
  blocks behind a variable its tests turn off, and drops both once applied. A mocked plan also
  defers the key reads, since the projects are still to create, so its tests cannot assert the
  output; the service roots' tests check it from the reading side.
- **The provider drops a missing project from state, but reports a missing spike protection as an
  error.** A move to another organization therefore takes two pull requests. The first forgets the
  old resources and key reads with `removed` blocks set to `destroy = false`, and freezes the
  `services` output as literals of the old values, so the services keep planning. The second sets
  foundation's `sentry_ingest_host` with the root's `organization`, `base_url` and token versions,
  and restores the resources: one deploy then applies foundation, the Sentry root and the services
  in that order. Deleting the resource blocks alone would delete the old projects and their
  history.
- **The provider is pre-1.0,** so Renovate's updates wait for a review: a minor release can break,
  as `sentry_issue_alert` did when `sentry_alert` replaced it.

### Sentry deploys

Release CI creates each release when it cuts the tag, and records no deploy. Each service job of
`deploy.yaml` records the release's `production` deploy once its root serves it, through
`.github/scripts/sentry-deploys.mjs` on the `tofu` action's `plan` output.

- **A root serves its releases once its `tofu` step succeeds,** and the step runs only then: the
  plan rolls a failed service again, as the [release contract](#release-contract) describes. The
  plan's outputs prove nothing, since OpenTofu writes root outputs even when an apply fails.
- **Each release gets one deploy.** The script lists the release's deploys and records one only when
  `production` has none, so a deploy missed during a Sentry outage lands on the next merge.
- **Nothing fails the deploy.** A Sentry error, a release Sentry lacks or a missing token only
  warns.
- **The token is `release-ci`,** the organization token release CI uses, set as `SENTRY_AUTH_TOKEN`
  in the `production` environment. Its claims name the organization and its region, so an
  organization move only swaps the token there and in each `release` environment.
