// Package cli hosts the go:generate directives of the cli module. Run
// `go generate ./...` from cli/ to regenerate the derived code.
package cli

// The proto stubs: Go bindings and connect-rpc handlers.
//
//go:generate rm -rf proto/gen
//go:generate go tool -modfile=buf.mod buf generate
