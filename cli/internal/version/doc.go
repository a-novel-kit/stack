// Package version resolves the build version shown in the CLI banner and by
// `a-novel version`.
//
// Resolution order (first non-empty wins):
//
//  1. The [Version] var, when a build injects it with
//     -ldflags "-X github.com/a-novel-kit/stack/cli/internal/version.Version=v1.2.3".
//  2. The module version recorded by `go install module@vX.Y.Z`
//     (debug.ReadBuildInfo → Main.Version).
//  3. The VCS revision baked in by the Go toolchain for `go build` inside a
//     git checkout (short commit, suffixed "-dirty" when the tree is modified).
//  4. The literal "dev" when nothing else is available.
package version
