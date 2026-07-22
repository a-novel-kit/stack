// Package generate hosts the go:generate directives for the cli module.
// Run `go generate ./...` from the cli/ root to regenerate derived code.
package cli

// Generate proto stubs (Go bindings + connect-rpc handlers).
//
//go:generate rm -rf proto/gen
//go:generate go tool -modfile=buf.mod buf generate
