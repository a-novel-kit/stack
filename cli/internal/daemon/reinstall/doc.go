// Package reinstall implements the checkpoint that hands running go-exec
// targets from one daemon to the next.
//
//  1. The PrepareReinstall RPC writes a JSON checkpoint listing every running
//     go-exec target. Containers survive the daemon's death on their own, so
//     they stay out of it.
//  2. The daemon stops those targets through the normal Kill path, freeing
//     their ports, and exits cleanly.
//  3. `a-novel install` rebuilds the binary; `a-novel core restart
//     --preserve-targets` goes straight to the next step.
//  4. The next daemon reads the checkpoint, relaunches the listed targets on
//     their recorded ports, and deletes it.
//
// The checkpoint is handoff state only: losing it degrades behavior without
// breaking it, which keeps the daemon's recovery stateless.
package reinstall
