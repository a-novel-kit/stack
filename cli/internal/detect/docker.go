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
// The filename alone does not say which basenames are jobs; it is domain
// knowledge, so extend this set when a new job kind appears.
var jobBasenames = map[string]struct{}{
	"migrations":  {},
	"init":        {},
	"rotate-keys": {},
	"rotatekeys":  {},
	"cron":        {},
}

// detectPodman emits a target for a root Dockerfile and each *.Dockerfile in
// dir/builds/. The root target uses the repository image name; named targets
// append their Dockerfile-derived image segment.
func detectPodman(dir, rel string) []Target {
	registry := registryBase(dir)
	var targets []Target

	if fileExists(filepath.Join(dir, "Dockerfile")) {
		targets = append(targets, podmanTarget(dir, rel, "Dockerfile", "Dockerfile", registry+":local"))
	}

	buildsDir := filepath.Join(dir, "builds")
	entries, err := os.ReadDir(buildsDir)
	if err != nil {
		return targets
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".Dockerfile") {
			continue
		}

		image := imageName(e.Name())
		tag := registry + "/" + image + ":local"
		dockerfileRel := filepath.Join("builds", e.Name())

		targets = append(targets, podmanTarget(dir, rel, e.Name(), dockerfileRel, tag))
	}
	return targets
}

// podmanTarget forwards required Dockerfile secrets from uppercase environment
// variables derived from their IDs.
func podmanTarget(dir, rel, name, dockerfileRel, tag string) Target {
	secretIDs := dockerfileRequiredSecretIDs(filepath.Join(dir, dockerfileRel))
	args := make([]string, 0, 8+len(secretIDs))
	args = append(args, buildArg, "--format", "docker")
	for _, id := range secretIDs {
		args = append(args, "--secret=id="+id+",env="+dockerfileSecretEnv(id))
	}
	args = append(args, "-f", dockerfileRel, "-t", tag, ".")

	return Target{
		Kind:   KindPodman,
		Name:   name,
		RelDir: rel,
		Dir:    dir,
		Detail: tag,
		Cmd:    "podman",
		Args:   args,
	}
}

// dockerfileRequiredSecretIDs returns required secret mount IDs in source order.
func dockerfileRequiredSecretIDs(path string) []string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	seen := make(map[string]struct{})
	var ids []string
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, token := range strings.Fields(line) {
			mount, ok := strings.CutPrefix(token, "--mount=")
			if !ok {
				continue
			}

			var id string
			secret := false
			required := false
			for _, option := range strings.Split(mount, ",") {
				switch {
				case option == "type=secret":
					secret = true
				case option == "required=true":
					required = true
				case strings.HasPrefix(option, "id="):
					id = strings.TrimPrefix(option, "id=")
				}
			}
			if !secret || !required || id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	return ids
}

// dockerfileSecretEnv maps a Dockerfile secret ID to its host environment name.
func dockerfileSecretEnv(id string) string {
	replacer := strings.NewReplacer("-", "_", ".", "_")
	return strings.ToUpper(replacer.Replace(id))
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
