# Service isolation and failure safety

## Trace one selected service end to end

Follow image selection, provenance checks, secret metadata, planning, database changes, migrations,
candidate health, traffic, receipt publication, and compensation. For each phase, identify the
resources it reads, mutates, and can access through its identity. Check the negative case: the peer's
registry, secret, database, or backup is unavailable while the selected service's prerequisites work.

Separate service publication from runtime dependencies. Authentication can depend on the JSON Keys
API without requiring JSON Keys redeployment. A selected candidate's real dependency failure should
block promotion. An unrelated service's initializer secret or backup should not become a deployment
prerequisite merely because a shared compiler or monitor lists it.

Trace authority as well as resource layout. Dedicated VMs can still share backup credentials,
restoration authority, a deployment identity, a mutable receipt, or a state lock. Inspect the effective
plan for peer changes, including imports, replacements, scheduler toggles, and shared configuration.
Do not infer isolation from a service selector or from `-target`; a targeted plan is not an ownership
boundary.

Keep foundation, release, and recovery responsibilities explicit. Give each mutable resource and field
one owner. Exchange minimal versioned coordinates instead of giving consumers access to another
root's complete state. For a field handoff, retain safe creation defaults and ignore only the runtime-owned
field; check the provider cannot reapply it through other updates. Ignored drift grants no authority or
proof of quiescence. Separate state and service accounts do not prove isolation: verify the IAM
scope each required API supports. If a necessary permission is project-wide, choose an enclosing
project boundary or an explicitly privileged maintenance operation; a name filter is not authorization.

For opt-in project provisioning, prove that empty inputs preserve the existing resource graph. Trace
both the operator's configuration publisher and the recovery compiler before activation: a new HCL
input is not supported end to end if one path discards it or copies production ownership into recovery.
Keep the source ownership inventory until the recovery target has been checked against it, then
remove production ownership from the generated recovery inputs. An empty recovery project map alone
does not prevent choosing a live service project as the replacement target.
Shared VPC attachment and subnet access are separate grants; review workload routing and invocation
authority before claiming that an attached project is deployable or isolated.

Treat API enablement, Google service-agent creation, and its role bindings as separate prerequisites.
Use the native provider resource when declarative consumers need the identity before first use.
Trace the control-plane principal: Direct VPC subnet use belongs to the Cloud Run service agent,
not the application's runtime account. Keep host network grants and firewall policy with their
foundation owner; verify effective routing and IAM separately from mocked resource creation.
For managed VM groups, distinguish the Google APIs MIG agent, Compute Engine service agent and VM
runtime account. Default execution-account deprivileging does not remove a separate Google agent's
inherited or primitive grants. Verify its actual policy rather than treating an additive narrow role
as replacement of broader authority.

## Preserve the trust boundary

Treat candidate code, manifests, workflow inputs, and artifacts as untrusted until validated by
protected tooling. A plan can execute providers or external programs even with read-only credentials.
Keep untrusted PR checks cloud-blind; any credentialed candidate assessment follows the reviewed
maintainer-authorization path. Redacting output does not sandbox executable candidate code.

Validate complete image families in the deployment workflow before credentials or mutations that
require them. A required PR check cannot protect against every merge bypass. Verify immutable digests
and producer provenance; keep unchanged peer image evidence from a trusted receipt rather than
silently accepting unverified inputs.

Repeat mutable artifact and exact secret-version metadata checks before both planning and saved-plan
consumption. An enabled version is point-in-time evidence, not proof of runtime IAM or availability.
These checks need no payload access and must not query unrelated service secrets.
Trace those reads to the caller's declared permissions: container administration does not imply
version-metadata access. Keep that grant with its provisioning owner.

Bind each private plan to its root, service scope, commit, configuration, content hash, and allowed
attempt. Recheck the applicable deletion authorization and policy at consumption. Unknown values or
missing evidence fail closed. Keep plans, state, provider diagnostics, and credentials out of public
logs and artifacts. A label authorizes the reviewed destructive scope; it does not waive data guards.

When plans share a state folder, use artifact-specific lifecycle selectors and test that state and
configuration are excluded. Native asynchronous cleanup does not enforce the apply deadline.
Inventory may recognize saved-plan artifacts without treating them as initialized state or ignoring
workspace locks. Keep custody within the writer's grant instead of widening IAM to fit an old path.
Exercise proposed resource changes against the trusted plan policy as well as provider mocks.
When assessment runs protected-base tooling, land a reviewed policy change before configuration
that needs it; candidate policy edits cannot authorize their own assessment.

When one protected workflow selects several roots, authorize project and backend coordinates against
protected registration before authentication or initialization. Reuse the operator's intent validator
for direct workflow submissions. Shared approval and serialization need not mean shared activation:
authorize image publication separately from resource creation or execution, and keep plan/apply free
of implicit image writes. Keep state, plan and configuration
custody in the same selected scope; reject workspace and CLI overrides that can redirect the backend.
Match native variable names exactly rather than relying on a decoder's case-insensitive field matching.
An inactive root needs explicit activation that includes trusted assessment and drift coverage;
adding a manual selector alone must not silently enroll it in live operations.

Inventory IAM by principal and resource. Runtime, backup, metadata-only monitor, restore, and deploy
roles have different needs. Avoid granting a monitor database passwords or backup payload access
when a metadata API can answer the check. Keep create-only backup writers and explicitly reviewed
restore authority. Check IAM inheritance and additive bindings when live evidence is available.

Choose roles by their permission sets, not their names. A predefined deployment or invocation role
can also authorize job execution, advancement or recovery. Prefer resource-scoped standard roles;
use a small custom role when their bundled permissions cross the required boundary. Verify each
API's supported IAM resources and condition attributes rather than assuming `resource.name` works
everywhere. Keep required Google service-agent roles distinct from workload and human authority.

Audit the plan reader separately from the apply executor: resource metadata access does not imply
IAM-policy inspection. Provisioning also needs attachment permission on its exact execution identities;
order those grants before resources that use them. An administrator that can rewrite IAM can escalate
its authority, so a configuration-only role is an operating contract, not a security sandbox.

Scope routine job updates to existing application jobs; project-wide mutation also reaches auxiliary
jobs such as rollout probes. Keep job creation/retirement and IAM maintenance with protected bootstrap
and foundation owners. Resource-scoped grants require the jobs to exist first: document that ordering
and the one-writer state handoff without giving routine release bootstrap authority.
Enforce create-only bootstrap in the reviewed plan policy, even when its executor has broader IAM:
allow only selected-resource creates and no-ops. Imports, moves and updates require separate ownership
reconciliation; a deletion-approval label must not waive this boundary.

An account allowed to deploy code and attach an application identity can indirectly exercise that
identity's privileges, even without direct secret access or token-creation permission. Review source,
images, runtime attachment and inherited IAM together. Mounted secret references describe configuration,
not the limit of a runtime identity's effective secret access. Keep execution artifacts separate from
durable receipts so a worker's output-writing permission cannot become release-completion authority.

Treat verifier images as control-plane code. Keep their publication authority separate from application
release, including repository-level grants; an image-name convention is not an IAM boundary. Retain
digests while supported releases or recovery records reference them, and check inherited write/delete
authority before relying on immutable tags or a reader-only binding.

Shared infrastructure needs disjoint authorization namespaces. Managed-folder IAM is additive: a
child of an old writer's folder does not isolate a new service. Use sibling paths outside that grant.
Federated principals are pool-scoped, not provider-scoped; separate providers alone are insufficient.
Bind each account to a provider-controlled attribute and validate the exact trusted claims. Test
denied peer access as well as allowed own-service operations before activating the boundary.

Emergency access revocation must work without a clean checkout or unrelated setup permissions.
Provisioning cleanup follows the workload's actual parent and verifies both removed temporary grants
and retained standing access before publishing readiness. Keep conditional grants distinct from
unconditional ones when selecting a binding for removal.

Review VPC routes, private DNS, ingress and egress, service invocation, database users, secret access,
and host metadata access together. Private addressing alone is insufficient. Identify paths that
bypass VPC egress controls, such as public egress from a `PRIVATE_RANGES_ONLY` Cloud Run service.
Keep public API access intentional and service-to-service access authenticated.

## Define the failure contract

An established API rollout keeps the serving revision while the candidate receives no ordinary
traffic. Verify the exact candidate with an application-level probe before promotion; readiness or
an open port is not dependency health. Describe first launch separately: no prior revision exists.
Use liveness for a stuck process, avoiding dependency-driven restart cascades.

Application compensation restores serving revisions and compatible configuration. It does not undo
migrations or concurrent database writes. Never automatically restore an old data backup as ordinary
deployment rollback. Destructive recovery needs explicit write quiescence, a reviewed recovery point,
and acceptance of the lost-write window. Expand/contract migrations remain a service responsibility.

A singleton database restart interrupts that service even when its API has multiple instances. Avoid
restarting an unchanged database. If a changed database requires disruption, bound and report it;
do not advertise zero downtime without an HA design and measured evidence. Minimum Cloud Run instances
reduce cold starts but do not make an instance permanent or in-memory work durable.

Model timeout, cancellation, runner loss, and a lost API response alongside ordinary command errors.
When a coordinator compensates after cancellation, stop and wait for its local child processes first;
give compensation a separate bounded context. This cannot cancel an already accepted cloud operation,
so reconcile live state before restoring anything.
Persist private intent and the exact target before mutation, then the server operation/execution ID
when returned. Do not claim to save a server-generated ID before it exists. An accepted request whose
response was lost remains ambiguous unless the API provides idempotency or authoritative reconciliation.
Retry only with that evidence; never replay a migration merely because its response was lost. Recovery
must survive loss of the original runner and must not alter an unselected service.

Only an acknowledged new intent reservation may authorize dispatch. Matching stored intent after a
lost acknowledgement does not restore that authority. Publishing observed completion evidence is a
different operation: identical immutable read-back can establish success without repeating work.
Bind that evidence to the reserved release and task configuration before it can authorize rollout.

Cloud Run task count, parallelism and retry settings constrain one execution, not independently
dispatched executions. Keep same-service exclusion across job updates, migrations, scheduled mutations
and rollout through completion evidence; zero task retries is neither a lock nor a replay defense.
Trace every writer before choosing a lock. A workflow or backend lock can end while accepted cloud work
continues; a persistent storage guard does not fence delayed requests to other APIs. Do not expire or
force-release one without settling the prior writer and native work. A declared job UID and image
identify configuration, not successful execution evidence.

For reviewed service-root applies, hold admission through convergence, configuration publication and
immutable completion evidence. Bind publication to the same private input snapshot hashed by the plan;
do not let a standalone configuration writer imply that an apply completed. Keep the acknowledged live
guard generation in the admitting process and condition removal on that generation, never an archived
version or a successor. A partial enrollment is not end-to-end exclusion; keep other writers inactive.
If only final guard removal failed, protected recovery may repeat that conditional deletion after
verifying immutable convergence/configuration evidence and the completed original workflow attempt.
Share this cleanup path with native releases whose exact successful completion is already recorded.
Derive the operation kind and original writer from verified evidence; the operator selects the service
and guard generation. Bind workflow action as well as commit before conditional removal.
Workflow completion alone is insufficient. Keep incomplete applies blocked; do not replay them or
invent missing completion evidence to unlock. An absent guard is a no-op, not new admission authority.
For a native rollout whose completion was never saved, reuse the ordinary writer's success proof
inside the existing protected finisher rather than adding a second coordinator. Require the original
writer to have ended and its exact guard to remain live before create-only publication. Bind the full
approved release/rollout and migration job UID/template, not just the shared network/database boundary;
prove actual traffic and saved successful execution. Missing migration proof or uncertain native work
stays blocked. Completion repair may write evidence, never replay the operation it describes.

Inspect how a provider establishes an inactive schedule: creation followed by pause is not atomic.
Delay a fresh caller's invocation grant until pause succeeds, and reconcile in-flight dispatches;
existing or inherited grants need separate handling. A scheduler's HTTP acknowledgement can precede
the target job's completion. Pause and drain are separate steps, and zero delivery retries do not
turn at-least-once scheduling into exclusive execution.
For delayed scheduled requests, acquire admission at the actual dispatcher before submitting work,
not at schedule acknowledgement. A managed workflow can retain observation after the CI runner exits;
save its execution/revision and the returned cloud operation before waiting. Failed or cancelled
dispatchers need their own native alert, including failures before the target job exists.
Use the platform's actual IAM granularity: project-scoped invocation is not exact-workflow isolation.
An additional workflow in that project changes the scheduler's effective trust boundary.

If scheduled mutation and release share admission through native completion, leave the schedule
unchanged instead of adding pause/resume coordination. First retire every bypass and reconcile work
accepted before enrollment; a shared guard cannot retroactively exclude it. Keep approval waits inside
the admitting operation's deadline, without giving the observer approval or advancement permission.
Timeout retains the guard and unknown native outcome; it does not authorize a fresh invocation.

When adopting managed rollouts, transfer the complete API specification and traffic to one writer;
ignoring only traffic in the old resource can still leave competing revision writers. Verify the
platform's first-launch, cancellation, retry, and rollback semantics. A skipped bootstrap canary is
not pre-promotion health evidence, and a new rollback rollout is not an instantaneous traffic rewind.
Platform retry support does not make external migration hooks idempotent.

Observe the exact native rollout through completion, including required deploy/verify jobs: an
ignored job can coexist with top-level success. Treat lost observation as unknown cloud outcome,
not failed deployment; report required human action without granting the observer mutation authority.
Keep observation-only retries separate from jobs that dispatch mutations. Their read-only concurrency
group must remain available while a writer is active; this exception does not loosen writer exclusion.
Bind inspection to an independently approved service scope before obtaining credentials.
Trace state and completion-record access separately: an existing state reader may lack the latter.
Grant only the required evidence reads, not the writer identity or unrelated receipt prefixes.
For interrupted work, select retained object generations and verify the linked intent, completion
and configuration hashes; reading the current object name can silently select a successor. Report
historical completion separately from the live guard and native work. Successful inspection grants
no retry or unlock authority; missing evidence does not prove that no mutation happened.
When validated intent already determines the completion record's name and the record binds its guard,
derive that name instead of adding a second pointer write. Pin the selected generation and verify its
full operation identity. A committed record can prove completion after its write acknowledgement was
lost; a failed pinned download cannot be treated as an absent record.

Keep an operations notification path independent of the CI runner. Prefer native event alerts scoped
to the exact project, location and pipeline. Match documented event fields: platform failure events
can have informational log severity. Check log routing, channel delivery and notification limits;
silence-based incident closure is not recovery evidence and must not authorize another deployment.

For periodic-job monitoring, distinguish observed zero successes from absent samples and a never-seen
series. Native absence policies can require prior metric history; seed and observe the success signal
after installation or modification before accepting coverage. Check alignment, retest and missing-data
semantics together so sparse healthy executions do not page between runs. Scope the metric to exact
owned jobs and project-local channels; an alert is neither execution exclusion nor permission to retry.

Locate the verifier's execution environment before choosing its transport. A private service URL
does not make a hosted build worker part of its VPC. Bind evidence to the exact project, service,
revision, phase, and probe execution, with candidate and post-promotion checks kept distinct. A
private probe needs invocation authority, not the application's database or secret-reading identity.
Give it a separate network policy too: reusing the application tag can reintroduce database reachability.
Validate the probe template before execution and the exact returned execution after completion; a
job name alone does not bind its current image, identity or overrides. Recheck phase traffic after the
probe, while retaining external serialization: two snapshots do not constitute a lock.

Keep rollout progress and durable recovery evidence separate. A healthy rollout whose receipt was
not published is incomplete, but missing evidence alone must not trigger a new rollout or migration.
An immutable native completion record does not become a supported recovery receipt by naming it one;
its consumer and interruption/finish procedure need explicit format binding and drill evidence.
An inactive code-only pilot must state its missing runtime contracts and activation gates; mocked
provider tests cannot prove that the verifier, effective IAM, or interruption recovery works live.

## Prove recovery rather than backup existence

Bind a recovery point to its source service and database identity, compatible database image and
major version, backup object generation, integrity evidence, and required secret versions. A completed
upload is not a restore test. Preserve old backup readers until retained recovery points expire or
have a reviewed replacement.

Restore into an isolated target before exposing services. Keep schedulers, public ingress, and the
human-only initializer absent unless the recovery plan explicitly needs them. Verify dependency
health from the permitted network, measure the full operator recovery time separately from automated
restore time, then revoke temporary access and remove only the approved disposable target.

Do not change storage lifecycle rules independently of the backup engine. Physical backups and WAL
form recovery chains; generic age-based deletion can invalidate retained backups. A new backup tool
needs an explicit IAM, retention, format-transition, and restore-proof assessment.

## Primary references

- [Cloud Run runtime contract](https://docs.cloud.google.com/run/docs/container-contract) governs
  shutdown and instance lifecycle; recheck it before making availability promises.
- [OpenTofu saved plans](https://opentofu.org/docs/cli/commands/plan/) can contain sensitive values.
- [OpenTofu remote state](https://opentofu.org/docs/language/state/remote-state-data/) explains why
  access to outputs also permits reading the underlying snapshot.
- [Managed folders](https://docs.cloud.google.com/storage/docs/managed-folders) inherit parent grants;
  [federation principals](https://docs.cloud.google.com/iam/docs/principal-identifiers#v1)
  identify identities and attribute sets within a pool.
- [GitHub concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)
  documents queue behavior; a mutex alone does not prove every release will run in dispatch order.
- [Cloud Run task retries](https://docs.cloud.google.com/run/docs/configuring/max-retries) apply to
  tasks within an execution, not repeated job dispatch.
