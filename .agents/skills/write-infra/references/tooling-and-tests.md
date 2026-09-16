# Tooling and tests

## Keep infrastructure declarative

Check OpenTofu and provider capabilities before choosing an imperative implementation. Resource
configuration, straightforward derivations, input constraints, and resource-graph tests belong in HCL
when it expresses them clearly. Aim for declarative definitions to dominate the maintained
implementation by removing custom machinery. Generating HCL from code or hiding scripts in provisioners
does not advance that goal; do not inflate configuration to meet a file or line ratio.

Use blocking validations or preconditions for deployment gates. OpenTofu `check` assertions only warn;
their failure does not stop a plan or apply. Preserve the existing fail-closed boundary when moving
validation out of a script, including private diagnostics and receipt compatibility.

## Choose from the execution environments

Inventory operator machines, CI runners, database hosts, and recovery containers before selecting a
language. The repository's accepted design owns the choice. Declarative HCL and YAML, SQL executed by
PostgreSQL, and third-party executables do not require translating everything into a general-purpose
language. Authored imperative policy should have one home; a thin launcher may only invoke it.

Consider the dependency and artifact distribution cost, credentials, host restrictions, debugging,
signal handling, and testing. Do not compare interpreter speed for control-plane code. A single
language hidden inside several shell wrappers with validation and retry logic is still several
implementations. Conversely, a new compiled CLI is not simpler merely because it is one binary.

Separate runtime dependencies from development tools with native package-manager filters. Verify a
fresh install from an empty store: a small final dependency tree does not prove unused packages were
never fetched or built. Keep development caches out of operational jobs and disable lifecycle scripts
when the operational dependencies do not need them.

Keep production tooling in the infra repository unless a genuinely shared contract justifies moving
it. The local workspace CLI is not a production dependency. Publish host/job helpers as reviewed,
pinned artifacts with provenance and a usable rollback version. Prove they can run with the exact
database image before adopting a runtime that those images do not contain.

## Buy the mechanism, retain the policy

Use `choose-dependency` to compare the standard library, installed tools, and maintained dependencies.
Prefer provider resource management, official cloud clients, registry tools, and schema validators to
handwritten HTTP authentication, pagination, upload retries, format parsers, or credential helpers.
Reuse mature executables through structured argument arrays; never build command strings containing
untrusted input or secrets.

Schema errors can contain private values and unexpected property names. Expose fixed messages or
reviewed schema-rule locations in public logs. Compare coercion behavior as well as valid outputs when
replacing a validator; record intentional input tightening separately from behavioral parity.

Keep the repository's service ownership, image-family contract, deletion decision, and receipt rules
explicit. A generic orchestrator does not automatically preserve them. Before adding a platform,
identify the custom files it replaces and the new operational services, IAM, state, and deployment
artifacts it introduces. Do not make both OpenTofu and a deployment controller own the same field.

Native workflow environments and concurrency should do the work they support. Verify their current
behavior from official documentation rather than recreating it or assuming they guarantee ordering,
unbounded queues, or recovery after runner loss.

Prefer a native operation ID over before/after resource-list discovery. Verify its expected scope and
commit before reporting success. A lost dispatch response is an uncertain mutation, not proof that
nothing happened: stop with an inspection path instead of resending unless the API provides a supported
idempotency guarantee.

Consolidate duplicated policy into named domain operations rather than a configurable mini-framework.
Explicit project, region, service, and operation inputs make one-shot commands reproducible. Derive
ephemeral coordinates instead of requiring users to keep a large shell session alive. Return opaque
identifiers on stdout and bounded, non-sensitive diagnostics on stderr.

## Replace implementations in bounded batches

Choose a complete capability each batch can replace and remove. Record its entry points, tests, docs,
and trusted workflow references. Count the added build steps, wrappers, dependencies, and test scaffolds
alongside deleted code. A language port that leaves a compatibility layer and increases the maintenance
surface needs further simplification before adoption.

Exercise the same fixtures against old and new implementations during the transition. Once parity and
rollout evidence are accepted, delete the superseded implementation and temporary parity harness in that
batch. A later incident must not leave two active code paths with different safety behavior.

Separate policy changes from language ports when both are needed. Preserve receipt and backup format
compatibility until their retention obligations end. Retire one-time migration code only after source,
state, and supported recovery artifacts no longer depend on it; age alone is not evidence.

## Keep tests that protect a decision

Use a small set of test layers with different jobs:

- Pure contract tests cover scope selection, image-family consistency, schema validation, and plan
  policy, including malformed and unknown inputs.
- Workflow tests inspect structured permissions, checkout provenance, environment selection, and
  credential ordering. Prefer parsing YAML over matching its formatting.
- Adapter and state-transition tests inject errors before and after each meaningful mutation. Verify
  private output handling, ambiguous outcomes, bounded retries, and peer preservation.
- Mocked provider tests check the resource graph and IAM/network contracts. Confirm every provider is
  mocked and the backend disabled before running them without cloud authority.
- Human-approved isolated drills establish real restore, traffic, and failure evidence when a change
  affects those guarantees. Mock success cannot substitute for them; unrelated mechanical edits do
  not need a new drill.

For each existing test, name the regression it catches and whether another layer already covers that
contract. Keep one authoritative check at the layer that owns it. Remove assertions about internal
variable names, incidental wording, or exact source spelling once behavior is covered. Retain focused
static checks when the trust boundary itself is static, such as an untrusted checkout before cloud
authentication. Shell syntax checks for documented commands can be valuable until those commands are
replaced by tested operator entry points.

Use table-driven cases and small fake adapters for important failures. Avoid emulating an entire
cloud CLI or testing third-party internals. Assert safety-critical ordering inside the fake mutation:
a saved plan must already be consumed when apply starts. Checking only the final state misses a
replay window. A replacement dependency still needs an adapter contract
test for the assumptions the repository relies on. Prefer documented dry-run interfaces when testing
Renovate instead of importing its private modules. Verify which stages the dry run reaches: Renovate's
local lookup reports update candidates but does not create branches or enforce a PR's minimum group
size. Cover the repository's declared policy separately and limit claims to the exercised stages.

Report test duration and maintenance burden alongside code size. Target confidence in security and
recovery decisions, with no blanket coverage percentage or deletion quota.
