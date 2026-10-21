// Package discovery parses each stack's per-service podman-compose.yaml files
// and exposes the result as a structured model of services, targets, infra,
// volumes, and the dependency graph. It is the daemon's operational
// source of truth: supervision, env, logs, and volumes all key off these types.
//
// # The compose contract
//
// A service's builds/podman-compose.yaml doubles as the daemon's service
// manifest. The conventions below are what make discovery work; they hold for
// every service repo, so service compose files don't re-document them:
//
//   - Every cmd/<target> entrypoint has a compose mirror named
//     <service>-<target> gated behind profiles: ["<target>"]. The profile keeps
//     targets out of a plain `podman compose up`: the daemon runs them itself,
//     as a go-exec process by default or the profiled container on demand, and
//     orders them through depends_on. It also keeps a containerized copy from
//     binding the host port a locally-run target owns.
//   - Compose services without a profile are infrastructure (databases,
//     mailservers): always brought up, never run as targets.
//   - A target with a healthcheck, in compose or in the Dockerfile, is a
//     long-runner; one without is a one-shot. One-shots run to completion on
//     infra-up and must be idempotent, since every infra-up cycle re-runs them.
//   - A long-runner depends on its infra only: its standalone image runs the
//     service's one-shots at boot, so `podman compose --profile <target> up`
//     alone brings it up, and the daemon gates it on every one-shot instead.
//     podman-compose turns a dependency into podman's --requires, which runs
//     an exited one-shot again and fails when it ends before the check. A
//     target whose image cannot run its one-shot, or a one-shot needing
//     another, depends on that single one-shot, and the one-shot lists, after
//     its own profile, the profiles of what waits for it. The first profile
//     alone names a target.
//   - A service another waits on with service_healthy declares its
//     healthcheck in compose. Podman drops a Dockerfile's HEALTHCHECK from an
//     OCI image, which is its default build format and the format published
//     images use.
//   - Sibling services are never declared in each other's compose files. Each
//     service describes only its own infra; cross-service connections resolve
//     through daemon-allocated env (<SIBLING>_<VAR> references).
//   - Constants (credentials, test keys) are inlined; values the daemon
//     allocates or derives (ports, hosts, URLs, DSNs) appear as ${VAR}
//     references the daemon fills.
//
// Discovery reports every contract violation at daemon startup and leaves the
// offending entry out.
package discovery
