package detect

import (
	"os"
	"os/exec"
	"path/filepath"
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

// TestHasNodeGenerate pins the lane-sensitivity of generate detection: only a
// non-go generate script is a node-side generation; generate:go is Go codegen.
func TestHasNodeGenerate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		pkg  string
		want bool
	}{
		{"GoOnly", `{"scripts":{"generate":"pnpm run generate:go","generate:go":"go generate ./..."}}`, false},
		{"NonGo", `{"scripts":{"generate":"pnpm run generate:mjml","generate:mjml":"bash mjml.sh"}}`, true},
		{"None", `{"scripts":{"build":"x","test":"y"}}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			mustWrite(t, filepath.Join(root, "package.json"), tc.pkg)
			if got := HasNodeGenerate(root); got != tc.want {
				t.Errorf("HasNodeGenerate = %v, want %v", got, tc.want)
			}
		})
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
