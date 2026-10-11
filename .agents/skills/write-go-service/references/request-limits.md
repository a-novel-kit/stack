# Request limits

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to every
REST route a service adds or changes, and to any costly operation.

Every service bounds what one caller can consume, in its own code, so the limits are reviewed with
the routes they guard. golib's `httpf` carries the standard setup: `NewRouter` applies the body
limit and the trusted visitor IP, and `Limits` builds the rate limits.

## Standard limits

- **Body.** `REST_MAX_REQUEST_SIZE` defaults to 16 KiB, applied by `httpf.NewRouter`. chi cannot
  raise a global limit for one route: the outer reader always stops first. A route that needs more
  sits outside the global limit, in a group with its own `RequestSize`.
- **Rate.** Every route but the liveness ping sits in a router group behind
  `httpf.NewLimits(cfg.Rest).Standard()`: 5 requests per second per visitor. A group, because chi
  knows the route pattern only once routing matched, and limits count per method and route
  pattern; a raw path would give every path-parameter value a fresh budget.
- **Refusal.** Answer `429` with `Retry-After`, and document it on every limited route (see
  `write-openapi`).
- **Tests.** `REST_RATE_LIMITS_ENABLED=false` turns the limits off for a local test stack whose
  suites send bursts from one address. Production never sets it, and infra asserts so.

## The caller

- **Signed-in user** when the limiter runs after the auth middleware, from the verified claims.
- **Visitor IP** otherwise, an IPv6 address counted per /64 (`httprate.CanonicalizeIP`). An
  anonymous token is never a key: anyone can mint another.
- **Trusted header.** The platform calls services from its server, so the TCP peer is the platform.
  It sends the visitor's IP in `X-Client-IP`, and `REST_CLIENT_IP_HEADER` names that header.
  Name it only where IAM, or a NetworkPolicy, admits no caller but the platform: anyone else could
  forge a fresh budget per request.

## Strict limits

A route that issues tokens, checks a password or sends email adds a strict limit to the standard
one:

- per caller, so one visitor cannot farm tokens or mail;
- per target account, keyed by the email in the body, case-folded, when the route sends mail or
  checks a password. Many callers cannot then gang up on one mailbox or account.

Declare each policy once, by name, in the service's limits file, built on `httpf.Limits.By` with
the service's own keys, so every number is auditable in one place. Authentication's values are the
reference: anonymous tokens 3 per 5 minutes per visitor; login 10 per minute and 30 per 10 minutes
per visitor, plus 30 per 10 minutes per email; mail 3 per 5 minutes per caller, plus 3 per hour per
address.

## Costly routes

A route that calls a paid provider or spends heavy CPU or memory gets limits as strong as
authentication's entrypoints: a strict per-caller limit, and a concurrency gate sized to the
instance's memory. Authentication runs at most 4 Argon2 hashes at once, 64 MiB each, in a 512 MiB
instance. Excess calls wait for a slot rather than exhaust the instance.

## Counters

Counters live in memory, so each instance counts on its own. A service whose strict limits must be
exact runs on one instance. Needing a second is the trigger to move the counters to a shared store,
Valkey on Google Cloud.

## gRPC

Internal gRPC servers rely on IAM and internal ingress, Cloud Run's concurrency cap and grpc-go's
4 MiB message default. A per-user cap on a costly RPC belongs in core, where the user is known.
