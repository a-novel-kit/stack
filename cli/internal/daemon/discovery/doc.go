// Package discovery parses each stack's per-service podman-compose.yaml files
// and exposes the result as a structured model (services, targets, infra,
// volumes, networks, dependency graph). The result is the daemon's source of
// truth for everything operational — every later phase (supervision, env,
// logs, volumes) keys off these types.
//
// # The compose contract
//
// A service's builds/podman-compose.yaml doubles as the daemon's service
// manifest. The conventions below are what make discovery work; they hold for
// every service repo, so service compose files don't re-document them:
//
//   - Every cmd/<target> entrypoint has a compose mirror named
//     <service>-<target> gated behind profiles: ["<target>"]. The profile
//     keeps targets out of a plain `podman compose up`: the daemon runs
//     targets itself (go-exec by default, the profiled container on demand)
//     and orchestrates ordering through depends_on, never through Compose's
//     profile cascade. It also prevents a containerized copy from binding the
//     host port a locally-run target owns.
//   - Compose services without a profile are infrastructure (databases,
//     mailservers): always brought up, never run as targets.
//   - A target with a healthcheck (compose or Dockerfile) is a long-runner;
//     one without is a one-shot. One-shots run to completion on infra-up and
//     must be idempotent — they re-run on every infra-up cycle.
//   - Sibling services are never declared in each other's compose files. Each
//     service describes only its own infra; cross-service connections resolve
//     through daemon-allocated env (<SIBLING>_<VAR> references).
//   - Constants (credentials, test keys) are inlined; values the daemon
//     allocates or derives (ports, hosts, URLs, DSNs) appear as ${VAR}
//     references the daemon fills.
//
// Discovery is deliberately strict: contract violations surface as errors at
// daemon startup, so misconfigurations never become runtime surprises. The
// one place it is lenient is healthcheck classification — see the kind
// derivation in discovery.go for why.
package discovery
