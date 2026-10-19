// Package runner supervises the lifecycle of the daemon's run targets: it
// starts them, tracks their phase and health, streams their logs, and tears
// them down. A target runs either as a go-exec host process or as a
// podman-compose container, and the runner presents one phase and health view
// over both.
//
// Dependency ordering and health-gating live here. Before a target starts, the
// runner brings its infra up, waits for health, and runs the one-shots
// (migrations, key rotation) to success, and every container `up` passes
// `--no-deps`. The stack's CONTRIBUTING.md explains why the daemon, not
// compose, owns the ordering.
package runner
