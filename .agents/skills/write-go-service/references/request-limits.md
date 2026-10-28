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
- **Rate.** `httpf.NewRouter` limits every route but the ping (`REST_LIVENESS_PATH`) to
  `REST_RATE_LIMIT` per visitor, `5/1s` by default. Limits count per method and route pattern, which
  the router looks up itself: a raw path would give every path-parameter value a fresh budget.
- **Refusal.** Answer `429` with `Retry-After`, and document it on every limited route (see
  `write-openapi`).
- **Tests.** `REST_RATE_LIMITS_ENABLED=false` turns the limits off for a local test stack whose
  suites send bursts from one address. Production never sets it, and infra asserts so.

## The caller

- **Signed-in user** when the limiter runs after the auth middleware, from the verified claims.
  Authentication's client exports this key as `serviceauthentication.CallerKey`.
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

Every rate or concurrency limit is configuration, like a timeout: an environment variable with its
default in `env.go`, never a number in a middleware. A rate is an `httpf.Rate`, written `<requests>/<window>`
(`3/5m`) in its variable and in its default: `config.Must(httpf.ParseRate("3/5m"))` writes the
default, and `config.LoadEnv` reads the variable with `httpf.ParseRate`. Name each policy once in the
service's limits file, built on `httpf.Limits.By` with a caller key or `httpf.BodyKeyRate` for a
target named in the body. The limits file exposes one named middleware per guarded route family,
built once from the config, so the route table mounts each with chi's `With` and stays one line per
route: `r.With(limit.Login).Put("/", handlerTokenCreate.ServeHTTP)`. Its constructor names every
limiter as a local first, then composes the families from those names:
`Login: chi.Chain(loginBurst, login, loginEmail).Handler`, never a call nested in the literal. One
limiter may serve several families: `By` counts per route, so they never share a budget. A key or gate that another
service could use belongs in golib, not in the service. Authentication's defaults are the reference:
anonymous tokens 3 per 5 minutes per visitor; login 10 per minute and 30 per 10 minutes per visitor,
plus 30 per 10 minutes per email; mail 3 per 5 minutes per caller, plus 3 per hour per address.

## Costly routes

A route that calls a paid provider or spends heavy CPU or memory gets limits as strong as
authentication's entrypoints: a strict per-caller limit, and a concurrency gate sized to the
instance's memory, also read from the environment: `httpf.LimitConcurrency` on the route, or a
semaphore around the costly work itself when it runs below the handler. Size a CPU-bound gate to the
CPUs, `runtime.GOMAXPROCS(0)`: more concurrent work adds memory but no throughput. Authentication runs
`ARGON2_MAX_CONCURRENT` Argon2 hashes at once, one per CPU by default as Keycloak does, 64 MiB each.
Excess calls wait for a slot rather than exhaust the instance.

## Counters

Counters live in memory, so each instance counts on its own. A service whose strict limits must be
exact runs on one instance. Needing a second is the trigger to move the counters to a shared store,
Valkey on Google Cloud.

## gRPC

Internal gRPC servers rely on IAM and internal ingress, Cloud Run's concurrency cap and grpc-go's
4 MiB message default. A per-user cap on a costly RPC belongs in core, where the user is known.
