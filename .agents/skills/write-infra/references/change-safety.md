# Change safety

- [Prove the plan before review](#prove-the-plan-before-review): zero-diff proofs, moves, imports
- [Retire protected resources](#retire-protected-resources): unlock, retire, forget, tainted
  replacements
- [Read the live state](#read-the-live-state-before-trusting-the-code)
- [Release contract](#release-contract), [Capacity caps](#capacity-caps),
  [Planned downtimes](#planned-downtimes), [Databases and backups](#databases-and-backups),
  [Trust boundaries](#trust-boundaries), [Recovery](#recovery)
- [Add a service](#add-a-service): the pull-request order, the first plan

## Prove the plan before review

- **Pure refactors must plan to zero changes.** Locally, run
  `tofu plan -refresh=false -lock=false` against the real backend. It compares code with state
  without calling the APIs, so it works under a human account that cannot read the workload
  project.
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
  description are all force-new, so the plan deletes the binding and needs the label. To widen a
  conditioned grant, add a second binding with the new condition: it plans as an addition.
- **The cluster root reads foundation's applied outputs.** A new foundation output, or a new key in
  one such as a platform's edge, must deploy before the cluster root reads it, or the cluster's plan
  fails on a missing attribute. Ship the output in its own pull request first, then the change that
  uses it.
- **Roots also read each other's committed `terraform.tfvars`,** through
  `provider::terraform::decode_tfvars`: the cluster root reads every service root's pins and
  foundation's principals, Grafana reads foundation's node pool and the cluster's databases, and
  `recovery-database` reads the cluster's databases and each service root's database pin. No output
  shows these reads. Before removing or renaming an input, grep the whole repository for its name,
  `environments/recovery*` included (a-novel/infra#1408).
- **A plan shows the state it read.** When pull requests merge and deploy close together, re-run a
  plan that shows an unexpected change before judging it: a deploy may have applied it meanwhile.
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
  and an apply also polls each long-running operation, which takes a permission of its own. When
  the writer starts managing a new resource type, or takes an action it never took before, add the
  permissions in foundation first.
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
  resource that needs it, as bootstrap does for Cloud Scheduler's.
- **Artifact Registry's VPC-SC setting `depends_on` a repository in its location.** A location has
  no setting until the project holds a repository there, so the first apply in a new region
  answered 404 (a-novel/infra#1230, #1233).
- **Cloud Scheduler's first jobs in a new region can fail with
  `Error 409: Failed to perform tenant project creation`.** Google is still creating its tenant
  project for that region. Re-run the failed deploy job (a-novel/infra#1230).

## Read the live state before trusting the code

Production can differ from `master` even when drift is clean. Compare the planned diff with the
image or binary that will consume it, and fix the pins in the same change.

Foundation's cluster sends only its system components' logs to Cloud Logging, so workload logs stay
in the cluster and reach Grafana through Alloy. A failed cluster deploy's `Describe the databases`
step prints each database's unmet conditions, stuck pods and last warnings. To read further, open a
draft pull request whose `plan-cluster` job reads the pods, their logs and the custom resources from
the API server as the plan identity. Close it unmerged once read (a-novel/infra#1246, #1251).

## Release contract

1. **Each release image is pinned once,** by `tag@digest` in its service root's `terraform.tfvars`,
   the database image included. The cluster root reads every pin, and its deploy copies each into
   the service's `a-novel-<service>` repository before it applies. Foundation's `releases` set keeps
   those repositories beside the cluster, in `europe-west9`, and the nodes only read them.
   Foundation applies before the cluster, so a new repository exists before its first copy. A
   release without a database change keeps the reproducible digest and plans nothing for the
   database (a-novel/infra#1408).
   - The `a-novel-images` admission policy admits only images from the workload project's Artifact
     Registry, pinned by digest, in every namespace the cluster root manages.
2. **Migrations run once per image and lineage, before the service rolls.** The migrations Job's
   name ends with its image digest and a hash of `backup_server_name`. The apply waits for it to
   complete, and the service's workloads `depends_on` it. A finished Job stays as its record. A
   failed one fails the deploy and stays tainted, so the next deploy runs it again.
3. **A pod serves only once its probes pass.** `cluster-workload` gives a pod a minute to start,
   then routes calls to it only while its readiness probe answers. The apply waits for each
   rollout, which keeps previous pods serving until new ones are ready, so a release that never
   becomes ready fails the deploy without an outage.
4. **Post-deploy checks run one script, `modules/smoke-check/smoke.sh`,** in each service's own
   image, as the namespace's `smoke` identity, with the service token the kubelet projects for the
   service's audience:
   - a smoke Job is named after everything its pod runs, so a new image, script or setting runs it
     once. The deploy waits on it, and a failed Job fails the deploy;
   - a health CronJob runs the same check every five minutes, so a failing service shows as failed
     Jobs;
   - GenAI's provider check makes one real OpenAI call for each revision `genai_provider_check`
     names;
   - the script retries every five seconds for two minutes, then prints the last exit code and
     answer. A deadline, not an attempt count, bounds it, so slow attempts cannot outlast the job;
   - it skips once a planned downtime has started, and re-tests the start when it fails, since a
     check begun just before `start` fails when the services stop. Clearing the downtime runs the
     Job again.
5. Nothing rolls a release back. A pull request that lowers a pinned version fails its plan, so a
   bad release is fixed forward, through a hotfix when the service's `master` carries unreleased
   work (`manage-versions`). Migrations stay backward compatible; data rollback is a recovery,
   never part of a deploy.
   - **A release pin that merges while a branch is open turns the branch's pins into downgrades.**
     The branch's plans then fail with `… lowers <image> from <version>`. Rebase the branch onto
     `master`.
6. The pins the last apply deployed come from the plan's `output_changes.image_copies.before`. The
   plan's `prior_state` already holds the new outputs, so it cannot tell a changed pin apart.

## Capacity caps

- **The node pool bounds the cluster.** It runs between foundation's `cluster.min_nodes` and
  `max_nodes`, and the autoscaler adds a node only when pods cannot be placed. Two nodes still hold
  every pod's requests if the third is lost (a-novel/infra#1326). Capacity that stays is a decision,
  which Grafana prompts.
- **Each service runs inside its namespace's budget,** the cluster root's `service_budgets`: the
  CPU and memory its pods request, and how many run. Together the budgets fit the node pool at its
  maximum. A change that raises a workload's replicas or requests checks its budget, which covers a
  rollout's surge pod, the database's two instances and the jobs. A quota on requests refuses a
  container that sets none, so the namespace's limit range gives each one a small request.
- **Foundation's `cost_cap` sets the CPU quota,** the hard ceiling only Google raises, above the
  node pool's maximum. Growth past it starts with a quota request.
- **GKE node boot disks and the databases' volumes share one SSD quota.** The nodes' pd-balanced
  boot disks and the PersistentVolumes all count against the region's `SSD_TOTAL_GB`, 500 GB by
  default. Size them together. A replica whose volume cannot be provisioned leaves its database
  never `Ready`. Foundation's `cost_cap` raises the quota through a Cloud Quotas preference, which
  takes effect only once Google grants it.
- **A service whose rate limits count in each pod** keeps the pod count its limits assume:
  Authentication runs one pod until it counts through its store, as
  [request limits](../../write-go-service/references/request-limits.md#counters) describes. Raising
  it is a design change, not a tuning one.
- **A gRPC workload's call cap** (`GRPC_MAX_CONCURRENT_CALLS`) sits beside its `GOMEMLIMIT` in the
  cluster root, so a change to one is reviewed with the other: the cap times one call's peak memory
  fits the memory limit. `cluster-workload` makes a gRPC workload's Service headless, so callers
  balance their calls across its pods; an HTTP workload keeps its ClusterIP.
- **A service whose calls mostly wait scales on its calls in flight, beside CPU.** CPU stays low
  on its full pods. `cluster-workload`'s `scaling_gauge` reads the pods' `grpc_server_calls_in_flight`
  on their metrics port through an `AutoscalingMetric`. The autoscaler keeps it at 60% of
  `GRPC_MAX_CONCURRENT_CALLS` per pod, and CPU at 70%. It absorbs bursts only: `replicas.max` times
  the call cap bounds the calls the service runs at once, and so the load it sends downstream.

## Planned downtimes

- **Planned downtimes come only from the admin-only `downtime.yaml`,** which writes `DOWNTIME`. A
  full deploy applies it once the run ends, but only when the run's `downtime` job succeeded: a run
  that failed before writing deploys nothing. The cluster root gives each listed service's
  workloads `DOWNTIME_START`, and they refuse work from `start` until someone clears it; `end` only
  informs users.
- **A downtime's GKE maintenance exclusion lands only when foundation applies.** The exclusion
  holds node upgrades through the window. When a merge lands while the downtime's full deploy waits
  behind another deploy, the merge's deploy replaces it, and applies foundation only when
  foundation changed. Check that foundation applied after the downtime was written. Otherwise an
  admin dispatches `deploy.yaml`, which applies every root, before `start`.
- **A downtime post's color follows its headline.** Once `start` has passed, every post that reads
  "Expected back" shows the in-progress color, whatever change it announces.
- **Users see a downtime whenever a deploy can enforce one.** Any deploy applies `DOWNTIME`, so a
  schedule publishes `downtime.json` before writing it, and a clear deletes it before publishing.
- **A PostgreSQL major upgrade waits for its service's downtime.** CloudNativePG upgrades the major
  in place, with every instance stopped until `pg_upgrade` ends. `policy.jq` flags a change of the
  database's `imageCatalogRef.major`: its pull request needs `allow-downtime`, and `DOWNTIME` must
  list `service-<service>.database` with a `start` already passed. A deploy applies it only once
  that downtime has started. Ask for a full backup before the downtime starts.

## Databases and backups

- **Each service's database is a CloudNativePG Cluster** in its namespace: a primary and a replica
  on separate nodes, archiving WAL to the service's Barman bucket. The deploy waits until both
  instances serve and WAL reaches Barman.
- **A new database image rolls the database by switchover,** with no planned downtime: the service's
  `ImageCatalog` changes, and CloudNativePG restarts the replica on it, then hands the primary over.
  The switchover breaks every connection to the old primary, so the calls in flight fail. At low
  traffic one failed call per service crosses the failing-share threshold, which fires for about ten
  minutes with GenAI's faults alert, then resolves alone. Check that the failures stopped instead of
  silencing the alerts (a-novel/infra#1408).
- **`backup_server_name` names every database's lineage,** the name it archives under in its
  bucket. A new name recreates every database empty, rolls their clients and runs the migrations
  again. Never change it to repair one database.
- **Services connect as `app`,** the database's owner, with the credentials CloudNativePG generates
  in the `database-app` Secret. A client's pods read the password at start, so they roll with each
  new lineage. They verify the server as `database-rw.<service>.svc` against a CA the cluster root
  issues through cert-manager: CloudNativePG's own CA renews every few months, and a running
  service loads its trusted roots once.
- **Barman owns backup expiry,** at 14 days of base backups and the WAL they need. Never add
  age-based deletion to a backup bucket. Never delete WAL to free space. A daily base backup runs
  from the replica, and each new lineage starts with one.
- **A backup bucket keeps Object Versioning beside its retention policy.** Retention refuses to
  replace a live object, and versioning lets the replaced version turn noncurrent and stay retained.
  Barman rewrites `backup.info` from `STARTED` to `DONE` (a-novel/infra#1252).
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
  - Renovate delays OpenTofu, provider and cluster add-on updates by 7 days (6 hours for OpenTofu
    and provider patches) to bound a compromised release.
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
- **Internet egress leaves through Cloud NAT, past two allowlists.** Firewall rules see the node,
  never the pod, so foundation's `egress_allowlist` lets every node reach each listed host. The
  cluster root's `internet_egress` then admits each host only for the pods that call it, through an
  `FQDNNetworkPolicy`, which matches the addresses the pods' own lookups return. A new host joins
  both. The providers' own allowlists, such as the Workspace SMTP relay's, name foundation's
  `cluster_nat_address`.
  - A firewall rule names a host by FQDN only when every client resolves it alike, as Cloudflare's
    anycast does for `api.openai.com`. The firewall admits only the addresses its own lookup
    returned, and Google's domains answer each client location with another frontend: an FQDN rule
    for `smtp-relay.gmail.com` dropped most of Authentication's mail (a-novel/infra#1033). Match
    Google's hosts with the `google-services` address group, Google's ranges minus its Cloud
    customers', which `refresh-google-ranges.yaml` keeps current.
  - A refused connection times out without a log, and a health probe can pass by luck, so prove an
    allowlist change with a real call from the workload, such as an email.
- **DNS resolves only the allowlist.** The response policy `a-novel-dns-allowlist` resolves the
  private zones, Compute Engine's internal names and the egress allowlist's hosts. Every other name
  answers with a CNAME to `blocked.internal.` and no address, so no outside name server receives the
  lookup (a-novel/infra#1016).
  - A host joins both allowlists through its one `egress_allowlist` entry. Removing it deletes
    rules, so that pull request needs the deletion label.
  - A wildcard stops at any level that another rule names, as RFC 4592 wildcards do, so foundation
    refuses every level above an allowed name. The bypass rules need `google-beta`.
  - The `dns_query` logs show each lookup and its answer. Lookups reach Cloud DNS through the
    cluster's DNS caches, so the logs name what was resolved, not which pod resolved it.
- **The cluster's default-deny policies admit DNS to the `kube-system` pods whose `k8s-app` is
  `kube-dns` or `node-local-dns`.** GKE Standard turns NodeLocal DNSCache on by default since
  1.34.1-gke.3720000: each node's `node-local-dns` pod answers the queries sent to kube-dns'
  address. A peer that admits only `kube-dns` times out every lookup with `Errno -3`. The cluster
  root's test requires both values (a-novel/infra#1247).
- **Google API calls stay inside the `a_novel_production` perimeter,** a VPC Service Controls
  perimeter around the workload project, in foundation's `service-perimeter.tf`. It enforces its
  status. Its dry-run spec repeats the status, so to try a rule before enforcing it, add it to the
  spec alone.
  - Its restricted services are every API the project enables, plus Secret Manager and the APIs
    GKE's nodes call, and its VPC reaches only those. A pull request that enables an API adds it to
    the list, or foundation's test fails.
  - CI's deploy and plan identities and the operators enter from any source. Any identity inside
    may call Secret Manager and Cloud Storage in management, and only there. A new call that
    crosses the perimeter needs a rule in the same pull request.
  - Each project's `cloudaudit.googleapis.com/policy` log holds the refused calls, and the calls a
    spec-only rule would refuse. Prove a perimeter change with a real call from a workload.
  - Management stays outside, since its Workflows call Discord and GitHub, which Workflows inside a
    perimeter cannot reach.
- **Organization guardrails are set by hand,** from the setup runbook: the plan identity's deny
  policy, and the access policy with its two grants. Organization Administrator holds none of their
  roles, so an admin first grants themselves Access Context Manager Admin or Deny Admin. `gcloud`
  bills a user's calls to their configured project, which needs each API it calls enabled.
- **The writer's federation condition** pins `deploy.yaml` and `recovery.yaml` on `master` behind
  the `production` environment. Changing it is a bootstrap change, applied by the last trusted
  workflow before a new one depends on it.
- **`infra-downtime` trusts only `downtime.yaml` on `master`.** It starts, lists and cancels the
  `downtime-announcements` executions. Workflows grants that project-wide, so it could start
  `github-dispatch` too, which therefore refuses any workflow its schedules do not list. Both
  `downtime.yaml` jobs gate on the admin role, since "Re-run this job" skips the first one.
- **The read-only identity reads the cluster, never its Secrets.** Pull requests can mint it, so
  it holds `container.viewer`, which leaves Secrets out, and calls no service. Helm keeps its
  release records in ConfigMaps for that reason, and no release's values hold a secret.
- **A secret the cluster chooses itself comes from External Secrets' `Password` generator,** never
  from an OpenTofu `random_password`. An `ExternalSecret` with `refreshPolicy: CreatedOnce` writes
  it once into a Secret, so neither state nor a plan holds the value. A `random_password` sits in
  state, which pull requests read, and a `kubernetes_secret_v1` fails every plan's refresh, since
  the plan identity cannot read it. Authentication's counter store is the model: one generator's
  `secretKeys` gives each ACL user a password of its own (a-novel/infra#1421).
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
- **A service admits only the callers its `SERVICE_TOKEN_CALLERS` lists.** Each call carries a
  service token: a Kubernetes service-account token the kubelet projects for the callee's audience,
  which the service verifies against the cluster's issuer. Its network policy admits the same pods.
  A service that trusts a forwarded visitor header, as Authentication trusts `X-Client-IP`, lists
  only the platforms and its smoke check.
- **Only named humans run a maintenance Job.** Authentication's `maintenance` CronJob is the model:
  - its template never runs on its own: it is suspended, and its image's default only prints help.
    It never retries, so a failed run returns to the operator;
  - the principals in foundation's `authentication_maintenance_principals` reach the control plane
    and read logs, and the cluster root's RBAC lets them create Jobs in the namespace and follow
    their pods;
  - an admission policy keeps every field of such a Job as the template sets it, but the
    container's arguments, which name the operation;
  - the setup runbook gives the command.
- **Inventory IAM by principal and resource:**
  - each workload runs as its own Kubernetes service account and mounts no API token, only the
    service tokens of the services it calls;
  - each namespace's `secrets` account reads only its service's secrets in management, through
    External Secrets and Workload Identity, and its database writes only its own Barman bucket;
  - the nodes run as `a-novel-cluster-nodes`, which pulls images and writes the nodes' own logs and
    metrics, and nothing else;
  - humans get no write access in the workload project. The database operators read the services'
    namespaces through Kubernetes' built-in `view` role, never their Secrets, and the named operators
    only run the maintenance Jobs.

## Recovery

Restores go into a disposable `a-novel-recovery-<id>` project, never production. `recovery.yaml`'s
cluster drill builds a throwaway cluster there, restores the listed services' databases from their
Barman buckets with production's committed pins, runs each database's SQL checks, then destroys the
cluster whatever the outcome. The runbook covers project creation, the temporary grants and
cleanup. A successful backup proves nothing until a drill restores it and its SQL checks pass.

## Add a service

A service ships in two pull requests, the second merged once the first has deployed and its secret
values are in. a-novel/infra#1404 is the worked example for a platform.

1. **Bootstrap** creates the secret containers, and the Barman bucket of a service with a database
   (`cluster_databases`). Add version 1 of each secret value by hand: the cluster's External
   Secrets waits for it.
2. **One pull request runs the service:**
   - its root holds its pins and secret versions, and `main.yaml` gains its `plan-<root>` job.
     `deploy.yaml` and `drift.yaml` find the root on disk; `lint-repository` names what is missing;
   - the service joins foundation's `releases` set, which creates its `a-novel-<service>`
     repository;
   - the cluster root adds its namespace, budget, database with its queries, secrets and
     `release_roles`, its workloads through `cluster-workload` with their network policies, and its
     smoke and health checks. Each service it calls lists it in `SERVICE_TOKEN_CALLERS`;
   - a new internet host joins foundation's `egress_allowlist` and the cluster root's
     `internet_egress`;
   - Grafana covers it, as [Grafana coverage](grafana.md) describes.

- **A platform's public edge goes first.** Its address and certificate live in foundation's
  `platform_hosts`, and the cluster root reads them as an output. The certificate waits on the
  domain's DNS records, which the output lists (a-novel/infra#1393).
- **The new root's first plan fails.** OpenTofu's GCS backend creates a missing state under a lock
  even with `-lock=false`, and the plan identity only reads the state bucket, so
  `storage.objects.create` is denied on `<root>/default.tflock`. The deploy creates the state on
  merge. `plan-<root>` becomes a required check only once the GitHub root reads the merged
  `main.yaml`, and plan jobs skip the merge queue, so the failure blocks nothing.
