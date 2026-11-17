package discovery

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

// TestDiscoverService builds one service on disk and checks the classification
// every daemon layer keys off: a profiled compose service with a cmd dir is a
// target, kinded by the healthcheck its Dockerfile declares and named by its
// first profile, and an unprofiled one is infra.
func TestDiscoverService(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		dir := filepath.Join(t.TempDir(), "service-x")
		fsutiltest.Write(t, dir, map[string]string{
			"cmd/rest/main.go":       "package main\n",
			"cmd/migrations/main.go": "package main\n",
			"builds/rest.Dockerfile": "FROM scratch\nHEALTHCHECK CMD [\"/rest\", \"health\"]\n",
			"builds/podman-compose.yaml": `
services:
  postgres-x:
    ports: ["${POSTGRES_PORT}:5432"]
  service-x-rest:
    profiles: ["rest"]
    build: { context: .., dockerfile: builds/rest.Dockerfile }
    depends_on: { service-x-migrations: { condition: service_completed_successfully }, postgres-x: {} }
  service-x-migrations:
    profiles: ["migrations", "rest"]
    depends_on: [postgres-x]
volumes:
  x-data:
`,
		})

		svc, errs := discoverService("default", dir)
		require.Empty(t, errs)

		want := map[string]TargetKind{"migrations": TargetKindOneShot, "rest": TargetKindLongRunner}
		require.Len(t, svc.Targets, len(want))
		for _, tgt := range svc.Targets {
			require.Equal(t, want[tgt.Name], tgt.Kind, "target %s", tgt.Name)
		}
		rest := svc.FindTargetByComposeName("service-x-rest")
		require.NotNil(t, rest)
		require.Equal(t, "default/service-x/rest", rest.ID())
		require.Equal(t, []string{"postgres-x", "service-x-migrations"}, rest.DependsOn)
		infra := svc.FindInfra("postgres-x")
		require.NotNil(t, infra)
		require.Len(t, infra.Ports, 1)
		require.Nil(t, svc.FindInfra("missing"))
		require.Len(t, svc.Volumes, 1)
		require.Equal(t, "x-data", svc.Volumes[0].Name)
	})

	// A compose file discovery cannot read drops its service, and the one
	// problem it reports names the file once.
	errorCases := []struct {
		name    string
		compose string
	}{
		{name: "Error/MissingCompose"},
		{name: "Error/MalformedCompose", compose: "services:\n  x:\n    ports: {a: b}\n"},
	}
	for _, testCase := range errorCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "service-y")
			composePath := filepath.Join(dir, "builds", "podman-compose.yaml")
			if testCase.compose != "" {
				fsutiltest.Write(t, dir, map[string]string{"builds/podman-compose.yaml": testCase.compose})
			}

			svc, errs := discoverService("default", dir)
			require.Nil(t, svc)
			require.Len(t, errs, 1)
			msg := errs[0].Error()
			require.Equal(t, 1, strings.Count(msg, composePath), "problem %q names %s once", msg, composePath)
		})
	}
}
