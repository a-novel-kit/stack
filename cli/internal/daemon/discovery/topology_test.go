package discovery

import "testing"

func TestRenderTopology(t *testing.T) {
	t.Parallel()

	svc := &Service{
		Name:  "service-x",
		Infra: []*Infra{{Name: "postgres-x"}},
		Targets: []*Target{
			{Name: "grpc", Kind: TargetKindLongRunner, DependsOn: []string{"postgres-x", "service-x-migrations"}},
			{Name: "migrations", Kind: TargetKindOneShot, DependsOn: []string{"postgres-x"}},
			{Name: "rest", Kind: TargetKindLongRunner},
		},
	}

	want := "service-x\n" +
		"├─ postgres-x             (infra)\n" +
		"├─ migrations             (one-shot)  depends-on: postgres-x\n" +
		"├─ grpc                   (long-runner)  depends-on: postgres-x, service-x-migrations\n" +
		"└─ rest                   (long-runner)\n"
	if got := RenderTopology(svc); got != want {
		t.Errorf("RenderTopology:\n got %q\nwant %q", got, want)
	}
}
