package discovery_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
)

func TestRenderTopology(t *testing.T) {
	t.Parallel()

	svc := &discovery.Service{
		Name:  "service-x",
		Infra: []*discovery.Infra{{Name: "postgres-x"}},
		Targets: []*discovery.Target{
			{Name: "grpc", Kind: discovery.TargetKindLongRunner, DependsOn: []string{"postgres-x", "service-x-migrations"}},
			{Name: "migrations", Kind: discovery.TargetKindOneShot, DependsOn: []string{"postgres-x"}},
			{Name: "rest", Kind: discovery.TargetKindLongRunner},
		},
	}

	want := "service-x\n" +
		"├─ postgres-x             (infra)\n" +
		"├─ migrations             (one-shot)  depends-on: postgres-x\n" +
		"├─ grpc                   (long-runner)  depends-on: postgres-x, service-x-migrations\n" +
		"└─ rest                   (long-runner)\n"
	require.Equal(t, want, discovery.RenderTopology(svc))
}
