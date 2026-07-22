// Package runner supervises the lifecycle of the daemon's run targets: it
// starts them, tracks their phase and health, streams their logs, and tears
// them down. A target runs either as a go-exec host process or as a
// podman-compose container, and the runner presents one phase and health view
// over both.
//
// Dependency ordering and health-gating live here. Before a target starts, the
// runner brings its infra up, waits for health, and runs the one-shots
// (migrations, key rotation) to success, and every container `up` passes
// `--no-deps`. The daemon owns this because it supervises a graph compose
// cannot express — a go-exec host process depending on a container's health —
// and podman-compose does not reliably enforce `depends_on` conditions.
package runner
