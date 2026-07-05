package detect

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExistsUnder(t *testing.T) {
	t.Parallel()

	// A stack-shaped layout: a signal at the root (package.json) and a Go
	// module + proto config in a sub-directory (cli/), the case the root-only
	// detection used to miss.
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package.json"), "{}")
	mustWrite(t, filepath.Join(root, "cli", "go.mod"), "module x\n")
	mustWrite(t, filepath.Join(root, "cli", "buf.yaml"), "version: v2\n")

	cases := []struct {
		name  string
		paths []string
		want  bool
	}{
		{"RootSignal", []string{"package.json"}, true},
		{"SubdirGoMod", []string{"go.mod"}, true},
		{"SubdirBufEither", []string{"buf.yaml", "buf.gen.yaml"}, true},
		{"Absent", []string{"Cargo.toml"}, false},
		{"Empty", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ExistsUnder(root, tc.paths); got != tc.want {
				t.Errorf("ExistsUnder(%v) = %v, want %v", tc.paths, got, tc.want)
			}
		})
	}
}

// TestExistsUnder_PrunesGitignored guards the property that lets `repo update`
// run from the stack root without falsely detecting languages from the
// gitignored app/ and kit/ sibling checkouts.
func TestExistsUnder_PrunesGitignored(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	root := t.TempDir()
	runGit(t, root, "init")
	mustWrite(t, filepath.Join(root, ".gitignore"), "ignored/\n")
	mustWrite(t, filepath.Join(root, "ignored", "go.mod"), "module ignored\n")

	// go.mod exists ONLY inside the gitignored subtree → must not be detected.
	if ExistsUnder(root, []string{"go.mod"}) {
		t.Error("ExistsUnder descended into a gitignored directory")
	}
	// A non-ignored signal at the root is still found.
	mustWrite(t, filepath.Join(root, "package.json"), "{}")
	if !ExistsUnder(root, []string{"package.json"}) {
		t.Error("ExistsUnder missed a non-ignored root signal")
	}
}

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

// runGit runs `git -C dir <args...>`, panicking on failure.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		panic(string(out))
	}
}

// TestComposeDependents guards the classifier that drives build.composeUpPhased:
// a service is a "dependent" (second wave) iff it declares a depends_on: block.
// Both the map and short-list forms count; a service with none is first-wave
// infra. The result preserves source order — composeUpPhased itself splits into
// only two waves, but the ordered list is what a deeper (multi-wave) scheduler
// would build on.
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
