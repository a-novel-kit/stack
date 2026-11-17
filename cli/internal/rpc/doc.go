// Package rpc reaches the daemon over its unix socket. New returns the
// generated connect-rpc client, and Call and Stream issue its RPCs with a
// daemon that does not answer mapped to NotRunningError.
package rpc
