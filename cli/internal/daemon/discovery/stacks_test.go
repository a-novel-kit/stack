package discovery

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// TestDiscoverStacksSkipsVanishedScratch pins the asymmetry that keeps the
// daemon startable: a scratch stack lives in a directory the OS reclaims, so
// its files can disappear while its $A_NOVEL_STACKS entry lives on in a shell
// config. Discovery skips the vanished entry and keeps the stacks that are
// still there, so a temp sweep never costs the operator their workspace.
func TestDiscoverStacksSkipsVanishedScratch(t *testing.T) {
	t.Parallel()

	present := t.TempDir()
	gone := filepath.Join(t.TempDir(), "swept")

	got, err := DiscoverStacks([]stacks.Stack{
		{Name: "default", Path: present, IsDefault: true},
		{Name: "swept", Path: gone},
	})
	if err != nil {
		t.Fatalf("a vanished scratch stack should not fail discovery: %v", err)
	}
	if len(got) != 1 || got[0].Name != "default" {
		t.Fatalf("discovered %v, want only the default stack", got)
	}
}

// TestDiscoverStacksFailsOnVanishedDefault is the other half: the default stack
// is the workspace, so its absence is a real misconfiguration and must be loud.
func TestDiscoverStacksFailsOnVanishedDefault(t *testing.T) {
	t.Parallel()

	gone := filepath.Join(t.TempDir(), "swept")

	if _, err := DiscoverStacks([]stacks.Stack{
		{Name: "default", Path: gone, IsDefault: true},
	}); err == nil {
		t.Fatal("a missing default stack should fail discovery")
	}
}

// TestDiscoverStacksSkipsNonDirectory covers the same skip for a path that
// exists as a file: just as unusable, and just as far from worth a dead daemon.
func TestDiscoverStacksSkipsNonDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	got, err := DiscoverStacks([]stacks.Stack{
		{Name: "default", Path: dir, IsDefault: true},
		{Name: "bogus", Path: file},
	})
	if err != nil {
		t.Fatalf("a non-directory scratch stack should not fail discovery: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("discovered %d stacks, want 1", len(got))
	}
}

// TestDiscoverService builds one service on disk and checks the classification
// every daemon layer keys off: a profiled compose service with a cmd dir is a
// target, kinded by the healthcheck its Dockerfile declares, and an unprofiled
// one is infra.
func TestDiscoverService(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "service-x")
	files := map[string]string{
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
    profiles: ["migrations"]
    depends_on: [postgres-x]
volumes:
  x-data:
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	svc, errs := discoverService("default", dir)
	if len(errs) != 0 {
		t.Fatalf("discoverService errors: %v", errs)
	}

	want := map[string]TargetKind{"migrations": TargetKindOneShot, "rest": TargetKindLongRunner}
	if len(svc.Targets) != len(want) {
		t.Fatalf("got %d targets, want %d", len(svc.Targets), len(want))
	}
	for _, tgt := range svc.Targets {
		if tgt.Kind != want[tgt.Name] {
			t.Errorf("target %s: kind %v, want %v", tgt.Name, tgt.Kind, want[tgt.Name])
		}
	}
	rest := svc.FindTargetByComposeName("service-x-rest")
	if rest == nil || rest.ID() != "default/service-x/rest" {
		t.Fatalf("FindTargetByComposeName(service-x-rest) = %+v", rest)
	}
	if !slices.Equal(rest.DependsOn, []string{"postgres-x", "service-x-migrations"}) {
		t.Errorf("rest depends on %v, want both deps sorted", rest.DependsOn)
	}
	if in := svc.FindInfra("postgres-x"); in == nil || len(in.Ports) != 1 {
		t.Errorf("FindInfra(postgres-x) = %+v, want the infra with its port", in)
	}
	if in := svc.FindInfra("missing"); in != nil {
		t.Errorf("FindInfra(missing) = %+v, want nil", in)
	}
	if len(svc.Volumes) != 1 || svc.Volumes[0].Name != "x-data" {
		t.Errorf("volumes = %+v, want x-data", svc.Volumes)
	}
}

// TestStacksLookups pins the name resolution every RPC goes through, where an
// empty stack name means the default stack.
func TestStacksLookups(t *testing.T) {
	t.Parallel()

	rest := &Target{Name: "rest", Service: "svc", Stack: "scratch"}
	x := Stacks{
		{Name: "default", Default: true, Services: []*Service{{Name: "svc", Stack: "default"}}},
		{Name: "scratch", Services: []*Service{{Name: "svc", Stack: "scratch", Targets: []*Target{rest}}}},
	}

	if st := x.Stack(""); st == nil || st.Name != "default" {
		t.Errorf(`Stack(""): got %+v, want the default stack`, st)
	}
	if st := x.Stack("missing"); st != nil {
		t.Errorf("Stack(missing): got %+v, want nil", st)
	}
	if svc := x.Service("", "svc"); svc == nil || svc.Stack != "default" {
		t.Errorf(`Service("", svc): got %+v, want the default stack's`, svc)
	}
	if svc := x.Service("scratch", "nope"); svc != nil {
		t.Errorf("Service(scratch, nope): got %+v, want nil", svc)
	}
	if tgt, svc := x.Target("scratch/svc/rest"); tgt != rest || svc == nil || svc.Stack != "scratch" {
		t.Errorf("Target(scratch/svc/rest): got (%+v, %+v)", tgt, svc)
	}
	if tgt, _ := x.Target("default/svc/rest"); tgt != nil {
		t.Errorf("Target(default/svc/rest): got %+v, want nil", tgt)
	}
	if got := x.ServiceNames(); !slices.Equal(got, []string{"svc", "svc"}) {
		t.Errorf("ServiceNames: got %v", got)
	}
}
