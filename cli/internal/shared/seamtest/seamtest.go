// Package seamtest swaps the package-level seams tests replace, such as a
// command runner or a terminal check.
package seamtest

import "testing"

// Swap sets *p to v for the test and restores it on cleanup. A test that swaps
// a package-level seam cannot run in parallel.
func Swap[T any](t *testing.T, p *T, v T) {
	t.Helper()

	orig := *p
	*p = v
	t.Cleanup(func() { *p = orig })
}
