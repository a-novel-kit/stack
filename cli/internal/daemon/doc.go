// Package daemon owns the daemon's process lifecycle: socket listener,
// graceful shutdown, signal handling, and recovery of orphan containers.
// The RPC handlers live in internal/daemon/server.
package daemon
