// Package setup implements `a-novel core setup`, the interactive first-time
// bootstrap. It checks the host tools, creates the state directories, clones
// missing stacks, manages a marker-delimited block in the user's shell rc and
// starts the daemon.
//
// Re-running it on a system that is already set up changes nothing and exits 0.
package setup
