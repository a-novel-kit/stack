package version

import (
	"runtime/debug"
)

// Version is the version a release build injects at link time. Left empty, it
// makes String fall back to the build info.
var Version = ""

// fallback is returned when neither ldflags nor build metadata yield anything
// usable.
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
	// sentinel "(devel)", which yields to the more actionable VCS revision
	// below.
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
		short := revision[:min(7, len(revision))]
		if modified == "true" {
			return short + "-dirty"
		}
		return short
	}

	return fallback
}
