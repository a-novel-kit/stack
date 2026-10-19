// Package server implements the daemon's handler for the anovel.v1.CoreService
// connect-rpc API. The Server holds the long-lived daemon state, such as the
// discovered stacks, the target supervisor, the env builder and the log hub,
// and each RPC is a method that reads or changes it.
package server
