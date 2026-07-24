package detect

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
)

// mustWrite writes content to path, creating parent directories. It panics on
// failure — a setup error, not a runtime outcome under test.
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		panic(err)
	}
}

// TestGlobalAppRoots pins the stack-root run fan-out: both app/service-* and
// app/platform-* checkouts (each its own git repo) are returned, sorted; a
// prefixed dir that is not its own git repo, and a non-app-prefixed dir, are
// skipped.
func TestGlobalAppRoots(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	// Two real app repos: a service and a platform.
	mustWrite(t, filepath.Join(root, "app", "service-auth", ".git"), "")
	mustWrite(t, filepath.Join(root, "app", "platform-studio", ".git"), "")
	// A prefixed dir that is not its own git repo (no .git) — skipped.
	mustWrite(t, filepath.Join(root, "app", "service-nogit", "go.mod"), "module x\n")
	// An unrelated dir sharing neither prefix — skipped.
	mustWrite(t, filepath.Join(root, "app", "docs", ".git"), "")

	got := globalAppRoots(root)
	want := []string{
		filepath.Join(root, "app", "platform-studio"),
		filepath.Join(root, "app", "service-auth"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("globalAppRoots = %v, want %v", got, want)
	}
}

// TestComposeDependents guards the classifier that drives build.composeUpPhased:
// a service is a "dependent" (second wave) iff it declares a depends_on: block.
// Both the map and short-list forms count; a service with none is first-wave
// infra. The result preserves source order.
func TestComposeDependents(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
		want []string
	}{
		{
			name: "MapFormLongCondition",
			yaml: `services:
  postgres-x:
    image: postgres
  service-x:
    build:
      context: ..
    depends_on:
      postgres-x:
        condition: service_healthy
`,
			want: []string{"service-x"},
		},
		{
			name: "ShortListForm",
			yaml: `services:
  postgres-x:
    image: postgres
  mailserver:
    image: mailpit
  service-x:
    depends_on:
      - postgres-x
      - mailserver
`,
			want: []string{"service-x"},
		},
		{
			name: "DBOnlyNoDependents",
			yaml: `services:
  postgres-x:
    image: postgres
    ports:
      - "${POSTGRES_PORT}:5432"
`,
			want: nil,
		},
		{
			name: "MultipleDependentsInSourceOrder",
			yaml: `services:
  postgres-x:
    image: postgres
  seed-x:
    depends_on:
      postgres-x:
        condition: service_healthy
  service-x:
    depends_on:
      seed-x:
        condition: service_completed_successfully
`,
			want: []string{"seed-x", "service-x"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := filepath.Join(t.TempDir(), "podman-compose.test.yaml")
			mustWrite(t, f, tc.yaml)
			if got := composeDependents(f); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("composeDependents = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestGoTests_TestCacheByEnv locks in the -count=1 policy: env-less kit-lib
// targets omit it (so Go's test cache serves the tight loop), while env-backed
// targets keep it (their Postgres state is invisible to that cache).
func TestGoTests_TestCacheByEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.26.4\n")

	t.Run("env-less target is cacheable", func(t *testing.T) {
		targets := goTests(dir, ".", nil)
		if len(targets) != 1 {
			t.Fatalf("want 1 env-less target, got %d", len(targets))
		}
		if slices.Contains(targets[0].Args, "-count=1") {
			t.Errorf("env-less target must omit -count=1; args=%v", targets[0].Args)
		}
		if targets[0].Env != nil {
			t.Errorf("env-less target must have no compose env")
		}
	})

	t.Run("env-backed target keeps -count=1", func(t *testing.T) {
		compose := filepath.Join(dir, "compose.yaml")
		mustWrite(t, compose, "services:\n  db:\n    image: postgres\n")
		envs := []envFile{{env: string(KindGo), path: []string{"internal"}, file: compose, id: "go.internal"}}

		targets := goTests(dir, ".", envs)
		if len(targets) != 1 {
			t.Fatalf("want 1 env-backed target, got %d", len(targets))
		}
		if !slices.Contains(targets[0].Args, "-count=1") {
			t.Errorf("env-backed target must keep -count=1; args=%v", targets[0].Args)
		}
		if targets[0].Env == nil {
			t.Errorf("env-backed target must carry its compose env")
		}
	})
}

// TestGoTests_ScopedEnvCatchAll reproduces the issue: a module whose only Go env
// covers ./internal, with a test package under pkg/go that no env names. Before the
// catch-all, that package ran nowhere and the suite reported success.
func TestGoTests_ScopedEnvCatchAll(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.26.4\n")
	mustWrite(t, filepath.Join(dir, "internal", "dao", "dao_test.go"), "package dao\n")
	mustWrite(t, filepath.Join(dir, "pkg", "go", "client_test.go"), "package client\n")

	compose := filepath.Join(dir, "compose.yaml")
	mustWrite(t, compose, "services:\n  db:\n    image: postgres\n")
	envs := []envFile{{env: string(KindGo), path: []string{"internal"}, file: compose, id: "go.internal"}}

	targets := goTests(dir, ".", envs)

	var scoped, catchAll *Target

	for i := range targets {
		switch targets[i].Name {
		case "go.internal":
			scoped = &targets[i]
		case uncoveredTargetName:
			catchAll = &targets[i]
		}
	}

	if scoped == nil {
		t.Fatal("the scoped internal target is missing")
	}

	if catchAll == nil {
		t.Fatal("no catch-all target: pkg/go would run nowhere")
	}

	// The catch-all names pkg/go and not internal, which the scoped env already runs.
	if !slices.Contains(catchAll.Args, "./pkg/go") {
		t.Errorf("catch-all must select ./pkg/go; args=%v", catchAll.Args)
	}

	if slices.Contains(catchAll.Args, "./internal") || slices.Contains(catchAll.Args, "./internal/dao") {
		t.Errorf("catch-all must not re-run the scoped subtree; args=%v", catchAll.Args)
	}

	// It carries no env and is cacheable, like the kit-lib path.
	if catchAll.Env != nil {
		t.Errorf("catch-all must carry no compose env")
	}

	if slices.Contains(catchAll.Args, "-count=1") {
		t.Errorf("catch-all must be cacheable (no -count=1); args=%v", catchAll.Args)
	}
}

// A module fully covered by its scoped envs needs no catch-all.
func TestGoTests_NoCatchAllWhenFullyCovered(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.26.4\n")
	mustWrite(t, filepath.Join(dir, "internal", "dao", "dao_test.go"), "package dao\n")
	mustWrite(t, filepath.Join(dir, "pkg", "go", "client_test.go"), "package client\n")

	compose := filepath.Join(dir, "compose.yaml")
	mustWrite(t, compose, "services:\n  db:\n    image: postgres\n")
	envs := []envFile{
		{env: string(KindGo), path: []string{"internal"}, file: compose, id: "go.internal"},
		{env: string(KindGo), path: []string{"pkg", "go"}, file: compose, id: "go.pkg"},
	}

	for _, target := range goTests(dir, ".", envs) {
		if target.Name == uncoveredTargetName {
			t.Errorf("no catch-all should be emitted when every test package is covered")
		}
	}
}

func TestUncoveredGoSelectors(t *testing.T) {
	t.Parallel()

	seg := func(parts ...string) []string { return parts }

	cases := []struct {
		name    string
		testPkg [][]string
		scoped  [][]string
		want    []string
	}{
		{
			name:    "pkg/go uncovered by an internal-only env",
			testPkg: [][]string{seg("internal", "dao"), seg("pkg", "go")},
			scoped:  [][]string{seg("internal")},
			want:    []string{"./pkg/go"},
		},
		{
			name:    "everything covered yields nothing",
			testPkg: [][]string{seg("internal", "dao"), seg("pkg", "go")},
			scoped:  [][]string{seg("internal"), seg("pkg", "go")},
			want:    nil,
		},
		{
			name:    "the module root is a selector on its own",
			testPkg: [][]string{{}, seg("cmd", "rest")},
			scoped:  [][]string{seg("internal")},
			want:    []string{".", "./cmd/rest"},
		},
		{
			// pkg is not covered by pkg/go — the prefix must match whole segments,
			// not a string prefix.
			name:    "a sibling under a partly-scoped tree stays uncovered",
			testPkg: [][]string{seg("pkg", "js"), seg("pkg", "go")},
			scoped:  [][]string{seg("pkg", "go")},
			want:    []string{"./pkg/js"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := uncoveredGoSelectors(c.testPkg, c.scoped)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("uncoveredGoSelectors = %v, want %v", got, c.want)
			}
		})
	}
}

// A bare go env (podman-compose.go.test.yaml, no path) runs ./... and covers the
// whole module, so no catch-all is emitted even with tests spread across it. This is
// service-authentication's layout, and a spurious catch-all there would re-run its
// DB-backed tests with no env and fail.
func TestGoTests_BareEnvCoversWholeModule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module x\n\ngo 1.26.4\n")
	mustWrite(t, filepath.Join(dir, "internal", "dao", "dao_test.go"), "package dao\n")
	mustWrite(t, filepath.Join(dir, "cmd", "rest", "main_test.go"), "package main\n")

	compose := filepath.Join(dir, "compose.yaml")
	mustWrite(t, compose, "services:\n  db:\n    image: postgres\n")
	envs := []envFile{{env: string(KindGo), path: nil, file: compose, id: "go"}}

	for _, target := range goTests(dir, ".", envs) {
		if target.Name == uncoveredTargetName {
			t.Errorf("a bare env covers ./...; no catch-all should be emitted, got args %v", target.Args)
		}
	}
}
