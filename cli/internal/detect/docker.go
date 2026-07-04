package detect

import (
	"os"
	"path/filepath"
	"strings"
)

// jobBasenames are Dockerfile basenames that, by a-novel convention, produce a
// one-shot "job" image rather than a long-lived server. Their tag is namespaced
// under jobs/ and stripped of dashes (rotate-keys → jobs/rotatekeys).
//
// Which basenames are jobs cannot be derived from the filename alone — it is
// domain knowledge (migrations is a job, rest is not). Extend this set when a
// new job kind appears.
var jobBasenames = map[string]struct{}{
	"migrations":  {},
	"init":        {},
	"rotate-keys": {},
	"rotatekeys":  {},
	"cron":        {},
}

// detectPodman emits one target per *.Dockerfile in dir/builds/. The build
// context is dir, and the image tag is derived from the go.mod located in dir
// itself (the same directory as builds/) plus the Dockerfile name. By
// a-novel convention builds/ always sits at the module root next to go.mod, so
// this is intentionally a same-directory lookup, not a walk up the tree; when
// no sibling go.mod exists registryBase falls back to a localhost/ prefix.
func detectPodman(dir, rel string) []Target {
	buildsDir := filepath.Join(dir, "builds")
	entries, err := os.ReadDir(buildsDir)
	if err != nil {
		return nil
	}

	registry := registryBase(dir)

	var targets []Target
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".Dockerfile") {
			continue
		}

		image := imageName(e.Name())
		tag := registry + "/" + image + ":local"
		dockerfileRel := filepath.Join("builds", e.Name())

		targets = append(targets, Target{
			Kind:   KindPodman,
			Name:   e.Name(),
			RelDir: rel,
			Dir:    dir,
			Detail: tag,
			Cmd:    "podman",
			// docker-format manifest so the image is consumable by
			// podman-compose without a registry push.
			Args: []string{buildArg, "--format", "docker", "-f", dockerfileRel, "-t", tag, "."},
		})
	}
	return targets
}

// registryBase computes the ghcr.io image prefix for a service directory from
// its go.mod module path:
//
//	module github.com/a-novel/service-json-keys/v2
//	  → ghcr.io/a-novel/service-json-keys
//
// When there is no go.mod (a non-Go repo that still ships Dockerfiles) it
// falls back to a host-local, push-safe prefix derived from the directory
// name: localhost/<dir-base>.
func registryBase(dir string) string {
	modPath := filepath.Join(dir, "go.mod")
	module := ""
	if fileExists(modPath) {
		module = goModulePath(modPath)
	}
	if module == "" {
		return "localhost/" + filepath.Base(dir)
	}

	module = stripMajorSuffix(module)
	parts := strings.Split(module, "/")
	// Expect host/owner/repo (github.com/a-novel/service-json-keys). Anything
	// shorter can't name a registry path, so degrade gracefully.
	if len(parts) < 3 {
		return "localhost/" + filepath.Base(dir)
	}
	owner, repo := parts[1], parts[2]
	return "ghcr.io/" + owner + "/" + repo
}

// imageName maps a Dockerfile filename to its image-name segment, following the
// a-novel image-naming convention:
//
//	rest.Dockerfile            → rest
//	standalone.grpc.Dockerfile → standalone-grpc      (dots → dashes)
//	migrations.Dockerfile      → jobs/migrations       (job → jobs/ prefix)
//	rotate-keys.Dockerfile     → jobs/rotatekeys       (job → strip dashes)
func imageName(filename string) string {
	base := strings.TrimSuffix(filename, ".Dockerfile")

	// The "kind" is the first dot-separated segment: for "standalone.grpc" it
	// is "standalone"; for "rotate-keys" it is "rotate-keys".
	head := base
	if i := strings.IndexByte(base, '.'); i >= 0 {
		head = base[:i]
	}

	if _, isJob := jobBasenames[head]; isJob {
		return "jobs/" + strings.ReplaceAll(head, "-", "")
	}

	return strings.ReplaceAll(base, ".", "-")
}
