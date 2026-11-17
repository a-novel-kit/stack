package cli

import "github.com/a-novel-kit/stack/cli/internal/detect"

// buildCapability is `a-novel build`: Go modules, pnpm build scripts and Podman images.
var buildCapability = capability{
	name:   "build",
	looks:  "a go.mod (Go), a package.json with build* scripts (pnpm), or a root Dockerfile / builds/*.Dockerfile (Podman)",
	detect: detect.Builds,
	short:  "Build Go modules, pnpm scripts and Podman images",
	long: `Discovers a Go module per go.mod, every pnpm "build"/"build:*" script, a root
Dockerfile and each builds/*.Dockerfile under the working directory, lets you
pick what to build, builds it in parallel and prints a report.

With -y or without a terminal, every target builds without the picker,
sequentially unless --jobs says otherwise.`,
	example: `  a-novel build                 # pick, then build in parallel
  a-novel build -t go,podman    # only Go and Podman targets
  a-novel build -j 4            # cap parallelism at 4
  a-novel build -y              # build everything, no prompt (CI-safe)
  a-novel build --dry-run       # show what would build, run nothing`,
}
