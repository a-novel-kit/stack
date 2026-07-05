// Package runner supervises the lifecycle of the daemon's run targets: it
// starts them, tracks their phase and health, streams their logs, and tears
// them down. A target runs in one of two modes — go-exec (a `go run` host
// process) or container (a podman-compose service) — and the runner presents a
// uniform phase/health view over both.
//
// Dependency ordering and health-gating live here rather than in compose:
// before a target starts, the runner brings its infra up and waits for health,
// then runs one-shots (migrations, key rotation) to success, and every
// container `up` passes `--no-deps`. The daemon owns this because it supervises
// a graph compose cannot express — a go-exec host process depending on a
// container's health — and podman-compose does not reliably enforce
// `depends_on` conditions. See the cli README's architecture notes for the
// wider rationale.
package runner
