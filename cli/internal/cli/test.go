package cli

import "github.com/a-novel-kit/stack/cli/internal/detect"

// testCapability is `a-novel test`: Go and pnpm tests, run in their compose envs.
var testCapability = capability{
	name:   "test",
	looks:  "a go.mod (Go tests) or a package.json with test* scripts (pnpm)",
	detect: detect.Tests,
	short:  "Run Go and pnpm tests, spinning up their podman-compose envs",
	long: `Discovers a Go test target per module and every pnpm "test"/"test:*" script
under the working directory, lets you pick which to run, runs them in parallel
and prints a report with coverage and the full output of every failure.

A Go module shipping builds/podman-compose.go[.<path>].test.yaml gets one target
per file, scoped to that path. A target with a compose env has it brought up
(host ports allocated, health awaited) around the run and torn down after, so
independent envs run concurrently.

With -y or without a terminal, every target runs without the picker,
sequentially unless --jobs says otherwise.`,
	example: `  a-novel test                  # pick, then run in parallel
  a-novel test -t go            # only Go test targets
  a-novel test -y               # run all tests, no prompt (CI-safe)
  a-novel test --dry-run        # show targets and their envs, run nothing`,
}
