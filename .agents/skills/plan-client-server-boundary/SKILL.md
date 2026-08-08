---
name: plan-client-server-boundary
description: >
  Plan responsibility boundaries between browser/app clients and backend services: trusted state,
  authorization, validation, invariants, atomic operations, workflow orchestration, evolvable data,
  retries, and performance. Load it with `plan-feature` whenever a feature adds or changes a public
  API, moves behavior between frontend and backend, composes several service calls, stores
  client-defined JSON, or introduces a long-running or paid operation.
---

# Plan client/server boundaries

Load `plan-feature` first. Specialize its design phase with the principle below before freezing an
API, data model, or work breakdown.

## Use the established foundation

Apply **separation of protected mechanism from client policy**. Use **SSS CEC — Simple Secure Server,
Complex Ergonomic Client** as the Agora shorthand, not as the name of an external standard.

Treat SSS CEC as a client/server application of two established ideas:

- Separate protection and mechanism from application-level policy and management. The Exokernel
  architecture is the close analogy: a small trusted boundary safely exposes primitives while
  untrusted application code supplies replaceable abstractions and policy.
- Apply the end-to-end argument: keep application-specific behavior at the endpoint that has the
  complete requirement unless moving it lower is necessary for correctness or measurably improves
  performance.

Do not deliberately make a client complicated. Accept product complexity at the replaceable client
edge when that preserves flexibility, then contain it in typed client/domain modules with ergonomic
APIs instead of scattering orchestration through visual components.

## Assign authority before behavior

Keep these responsibilities on the **server**:

- Derive identity from trusted authentication and authorize every object access independently.
- Validate every untrusted request and enforce security, privacy, quota, billing, ownership, and
  resource-consumption limits. Treat client validation as UX only.
- Own authoritative facts and invariants. Compute values such as permissions, prices, entitlements,
  balances, policy selections, and provider controls from trusted state.
- Expose a small set of meaningful resource or capability operations. Make each write atomic with
  respect to the invariant it owns; use idempotency, conditional writes, or version tokens where
  retries and concurrency can repeat or race it.
- Keep secrets, privileged dependencies, private data, and audit evidence behind the trust boundary.
- Persist durable state and durable execution. Represent long-running or paid work as an
  owner-scoped job with bounded submission, status, cancellation, idempotency, and usage rather than
  relying on one browser connection.

Keep these responsibilities in the **client** when the server duties above remain intact:

- Interpret product definitions, traverse flows, choose ordering and context, compose independent
  operations, and decide which proposals or intermediate results to save.
- Own presentation policy, form shape, progressive disclosure, optimistic interaction, conflict UI,
  local drafts, and recovery guidance.
- Validate and migrate evolution-heavy client-owned documents, select workflow-specific schemas,
  and adapt one stable set of server capabilities into several user journeys.
- Resume orchestration from durable resource and job identifiers after reload, disconnect, or
  replacement by another client implementation.
- Provide the ergonomic high-level API in a typed client library when several frontend features need
  the same composition. Keep the wire contract primitive and the consuming API pleasant.

## Keep the stable envelope; loosen only the payload

Never use “ditch data stability” without qualifying what becomes flexible. Preserve durable bytes or
semantics, ownership, resource identity, version/concurrency tokens, timestamps, size and retention
limits, and stable entry/exit contracts.

Allow opaque or weakly constrained versioned JSON only when its meaning belongs to a replaceable
client workflow and accepting an older or newer shape cannot violate a server invariant. Let the
client validate and migrate that payload. Elevate a shape into a server-owned/public contract when
several independent clients must interpret it identically, it crosses a publication or integration
boundary, or malformed content could affect authorization, money, privacy, or another user's data.

Prefer a stable ownership envelope around evolvable content over either extreme: a rigid server
schema for every UI revision or an unbounded arbitrary-JSON endpoint with no domain boundary.

## Use this placement test

Keep behavior on the server when **any** answer is yes:

- Must several writes succeed or fail together to preserve a business invariant?
- Does the behavior decide access, money, quota, identity, privacy, provider policy, or another
  authoritative fact?
- Must it continue safely after every client disconnects, or coordinate concurrent callers?
- Would exposing the required data, secret, or internal topology cross a trust boundary?
- Must independent clients observe one shared interpretation at the same time?
- Would client composition create an unmeasured N+1, high-latency, high-payload, or failure-prone
  path?

Move behavior to the client only when **all** answers are yes:

- Can it be composed from independently authorized, valid, and atomic server operations?
- Can every partial result be observed, resumed, retried, compensated, or intentionally abandoned
  without corrupting authoritative state?
- Is it product or interaction policy likely to vary by client or redesign?
- Can the client safely receive every input and choose every exposed option?
- Is the round-trip, payload, concurrency, battery, and failure budget acceptable under measurement?

When only network cost fails the test, add the narrowest bulk read, projection, batch submission, or
gateway aggregation that fixes the measured path. Do not move the whole workflow server-side by
default. When atomicity or trust fails, keep that unit on the server even if the surrounding journey
stays client-orchestrated.

## Plan the boundary

1. Map the user journey and identify every read, decision, side effect, and durable result.
2. Mark the trust source, invariant owner, and atomic commit boundary for each side effect.
3. Design the smallest meaningful server capabilities. Avoid both screen-shaped endpoints and raw
   table mutation APIs.
4. Compose at least two materially different client journeys from the same capabilities. Require a
   new server operation only for a new authoritative fact, invariant, protected capability, or
   measured performance need.
5. Attack the design with a malicious, stale, duplicated, reordered, concurrent, and disconnected
   client. Specify idempotency, conflict detection, partial completion, cancellation, and recovery.
6. Count network round trips and fan-out on critical journeys. Measure total user-facing latency and
   total resource cost; lower server CPU alone is not a performance result.
7. Record the decision in the planning issue before breaking work down by repository.

Add this conditional section to the `plan-feature` issue body:

```markdown
## Client/server boundary

| Concern   | Server mechanism / authority      | Client policy / composition | Failure & recovery                    |
| --------- | --------------------------------- | --------------------------- | ------------------------------------- |
| <concern> | <trusted primitive and invariant> | <ergonomic workflow>        | <retry, conflict, partial completion> |

- **Stable envelope:** <identity, ownership, versioning, limits, entry/exit contracts>.
- **Evolvable payload:** <client-owned shape and migration responsibility, or none>.
- **Performance budget:** <round trips, payload/fan-out, measurement and any bulk projection>.
- **Why this cut:** <how a redesign can change the client without weakening server guarantees>.
```

## Reject these shortcuts

- Trusting client-side validation, identity, totals, policy, or hidden fields.
- Splitting one invariant into a sequence of independently fallible client writes.
- Adding an endpoint per screen, button, Engine, or journey when stable primitives compose it.
- Calling raw table CRUD “simple”; a primitive is domain-meaningful and invariant-safe.
- Freezing an evolution-heavy client document into server logic solely because the current UI knows
  its schema.
- Calling arbitrary JSON “flexible” when it can change access, money, public interoperability, or
  another user's state.
- Claiming client orchestration improves performance without counting network calls, payloads,
  retries, mobile latency, and partial failures.
- Moving long-running execution into a browser lifecycle; keep orchestration policy client-side and
  each durable paid or privileged operation server-side.
- Using “smart endpoints and dumb pipes” as a synonym. In microservices literature, the smart
  endpoint is usually the service, not the browser client.

## Research basis

- [Engler, Kaashoek, and O'Toole — Exokernel](https://pdos.csail.mit.edu/6.1810/2017/readings/engler95exokernel.pdf): separate resource protection from application-level management.
- [Saltzer, Reed, and Clark — End-to-End Arguments in System Design](https://doi.org/10.1145/357401.357402): place application-specific functions at the endpoint that can completely implement them.
- [Fielding — REST](https://ics.uci.edu/~fielding/pubs/dissertation/rest_arch_style.htm): use stateless interactions and a uniform resource interface for visibility, scalability, and independent evolution.
- [Saltzer and Schroeder — The Protection of Information in Computer Systems](https://www.cs.virginia.edu/~evans/cs551/saltzer/): keep protection mechanisms economical and mediate every access.
- [OWASP input validation](https://cheatsheetseries.owasp.org/cheatsheets/Input_Validation_Cheat_Sheet.html) and [business-logic security](https://cheatsheetseries.owasp.org/cheatsheets/Business_Logic_Security_Cheat_Sheet.html): enforce trust and legal combinations on the server.
- [RFC 9110](https://www.rfc-editor.org/rfc/rfc9110.html): use idempotent semantics and conditional requests to make retries and concurrent writes safe.
- [Azure gateway aggregation](https://learn.microsoft.com/en-us/azure/architecture/patterns/gateway-aggregation): treat network chattiness as the counterweight to client composition.
