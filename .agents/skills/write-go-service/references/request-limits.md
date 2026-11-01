# Request limits

Read this reference when routed here by [write-go-service](../SKILL.md). Its rules apply to every
REST route and gRPC method a service adds or changes, and to any costly operation.

Every service bounds what one caller can consume, in its own code, so the limits are reviewed with
the routes they guard. golib's `httpf` carries the standard setup: `NewRouter` applies the body
limit and the trusted visitor IP, and `Limits` builds the rate limits.

## Decide on every route and method

Each route or method you add or change gets one of these, decided with the code it guards:

- **Standard only:** reads and cheap writes. The router's per-visitor rate (REST) or the server's
  call cap (gRPC) covers them.
- **Strict limits:** a route that issues tokens, checks a password or sends email, as
  [Strict limits](#strict-limits) describes.
- **Costly:** a route or method that calls a paid provider or spends heavy CPU or memory: a strict
  per-caller rate and a concurrency gate on REST ([Costly routes](#costly-routes)), a method cap on
  gRPC ([gRPC](#grpc)).
- **Retune:** a limit that legitimate traffic hits changes its default in `env.go`, with the reason
  beside it. Never remove a limit to make a test pass: turn the stack's limits off instead.

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

- **The standard limit always counts in each pod,** and so does every concurrency gate. N pods give
  a visitor N times `REST_RATE_LIMIT`, and run N times `ARGON2_MAX_CONCURRENT` hashes at once.
- **`httpf.Limits` counts through a shared store when the service names one** (golib v0.60.0).
  `REST_RATE_LIMIT_STORE_ADDR` (`host:port`), `REST_RATE_LIMIT_STORE_USERNAME` and
  `REST_RATE_LIMIT_STORE_PASSWORD` point it at a Valkey or Redis server, so every pod shares each
  strict limit's budget. An empty address keeps the counters in the process. The keys name the
  route and the caller, so each service needs a store of its own.
- **A store that fails a command, or takes over 100 ms to answer, sends the limit back to counting
  in the pod** until the store answers again. Each budget then counts once per pod, and every
  request counted that way carries `rate_limit.local=true` on its server span. The store never
  refuses a request.
- **The store's ACL user needs only `MGET`, `MULTI`, `INCRBY`, `EXPIRE`, `EXEC` and `PING` on
  `httprate:*` keys.** The client logs in with `HELLO` alone, which needs no grant.
- **A service whose strict limits must be exact runs one pod until it counts through a store.**
  Authentication's store runs in its own namespace (a-novel/.github#550).

## gRPC

gRPC servers are internal. Only our services call them, each proving its identity with a service
token, and network policies admit only declared callers. Per-caller rate limits therefore stay at
the edge, on the REST routes that see visitors, and a per-user cap on a costly RPC belongs in core,
where the user is known. A gRPC server bounds its calls in flight, per instance, and keeps grpc-go's
4 MiB message default.

- **Server cap.** `grpcf.NewCallLimit`, from `GRPC_MAX_CONCURRENT_CALLS`, caps the calls one
  instance runs at once, health checks aside. Size it so the cap times one call's peak memory fits
  the container's memory limit; infra sets it beside `GOMEMLIMIT`. Pass the `CallLimit` to
  `grpcf.NewServer` as an extra option.
- **Method cap.** `grpcf.MethodCallLimit(fullMethod, limit)` gives one method a cap of its own,
  inside the server's, as a route's rate limit adds to the router's. Add one when a method costs far
  more than its neighbours, such as a paid provider call or a large payload, so its calls cannot fill
  the server and starve cheap ones like a status poll. Each cap is configuration like a rate: an
  environment variable with its default in `env.go`, and the `CallLimit` built once in the server's
  wiring file, never a literal in `main`.
- **Refusal.** A full server or method refuses a call with `UNAVAILABLE` before reading it.
  In-cluster callers retry `UNAVAILABLE` on the next pod through golib's service config, so a
  refusal moves the call rather than failing it.
- **The retry rule.** `UNAVAILABLE` means the call did not run. A handler that has had an effect, a
  write, a paid call or an email, answers another code, or the caller's retry runs the effect twice.
- **Scaling.** A service whose calls mostly wait, so CPU never shows its load, serves the cap's
  gauge, `grpc_server_calls_in_flight`, on a metrics server of its own at `METRICS_PORT`
  (`CallLimit` is an `http.Handler`). Infra scales the service on its calls in flight, as
  `write-infra` describes (a-novel/.github#536).
