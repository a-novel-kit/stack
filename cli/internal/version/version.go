// Package version resolves the build version shown in the CLI banner and by
// `a-novel version`.
//
// Resolution order (first non-empty wins):
//
//  1. The ldflags-injected [Version] var — set by release builds with
//     -X github.com/a-novel-kit/stack/cli/internal/version.Version=v1.2.3
//  2. The module version recorded by `go install module@vX.Y.Z`
//     (debug.ReadBuildInfo → Main.Version).
//  3. The VCS revision baked in by the Go toolchain for `go build` inside a
//     git checkout (short commit, suffixed "-dirty" when the tree is modified).
//  4. The literal "dev" when nothing else is available.
package version

import (
	"runtime/debug"
)

// Version is intentionally a plain var (not a const) so release pipelines can
// override it at link time without touching source. Leave it empty here — an
// empty value means "fall back to build info", which keeps local `go run` and
// `go install @latest` honest about what they actually are.
var Version = ""

// fallback is returned when neither ldflags nor build metadata yield anything
// usable. Kept as a named constant so callers can compare against it if needed.
const fallback = "dev"

// String returns the resolved version string, e.g. "v2.1.0", "a1b2c3d-dirty",
// or "dev". It never returns an empty string.
func String() string {
	if Version != "" {
		return Version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return fallback
	}

	// `go install module@vX.Y.Z` records the tag here. Local builds record the
	// sentinel "(devel)", which we deliberately ignore in favour of the VCS
	// revision below — a commit hash is far more actionable than "(devel)".
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}

	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}

	if revision != "" {
		short := revision
		if len(short) > 7 {
			short = short[:7]
		}
		if modified == "true" {
			return short + "-dirty"
		}
		return short
	}

	return fallback
}
