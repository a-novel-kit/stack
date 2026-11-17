// Package rpc reaches the daemon over its unix socket. New returns the
// generated connect-rpc client, Call and Stream issue its RPCs with the
// daemon's errors mapped, and Word names the proto enums the way the CLI and
// the UI print them.
package rpc
