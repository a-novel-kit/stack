// Package env owns the daemon's environment variables. It allocates host ports
// with refcounting, synthesizes the HOST and URL variables of an allocated port,
// and resolves a reference to another service's variables against that
// service's allocations.
//
// The runner builds a target's env when it spawns the process, claims the infra
// ports at infra-up, and releases a consumer's ports when it terminates. The
// GetEnv RPC reads the same env without allocating anything.
package env
