package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPublishVersionRefusesNonInteractive(t *testing.T) {
	// Not parallel: swaps the package-level stdinIsTTY seam.
	orig := stdinIsTTY
	stdinIsTTY = func() bool { return false }
	t.Cleanup(func() { stdinIsTTY = orig })

	cmd := newPublishVersionCmd()
	cmd.SetArgs([]string{"patch"})
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected non-interactive publish to be refused, got nil")
	}
	if !strings.Contains(err.Error(), "non-interactively") {
		t.Fatalf("expected an interactive-only refusal, got: %v", err)
	}
}

func TestStampFile(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		content string
		prefix  string
		version string

		expectCount   int
		expectContent string
		expectErr     bool
	}{
		{
			name: "openapi version line",

			content: "info:\n  version: v1.2.3\n",
			prefix:  "version: ",
			version: "2.0.0",

			expectCount:   1,
			expectContent: "info:\n  version: v2.0.0\n",
		},
		{
			name: "module path with regex prefix",

			content: "go get github.com/a-novel/service-json-keys/v2@v2.1.3\n",
			prefix:  "a-novel/service-json-keys/[^/]+",
			version: "2.2.0",

			expectCount:   1,
			expectContent: "go get github.com/a-novel/service-json-keys/v2@v2.2.0\n",
		},
		{
			name: "multiple occurrences all stamped",

			content: "version: v1.0.0\nversion: v1.0.0\n",
			prefix:  "version: ",
			version: "1.1.0",

			expectCount:   2,
			expectContent: "version: v1.1.0\nversion: v1.1.0\n",
		},
		{
			name: "no match leaves file untouched",

			content: "nothing to see here\n",
			prefix:  "version: ",
			version: "9.9.9",

			expectCount:   0,
			expectContent: "nothing to see here\n",
		},
		{
			name: "invalid prefix regex",

			content: "version: v1.0.0\n",
			prefix:  "version: (",
			version: "1.1.0",

			expectErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "doc.md")
			if err := os.WriteFile(path, []byte(testCase.content), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}

			count, err := stampFile(path, testCase.prefix, testCase.version)
			if testCase.expectErr {
				if err == nil {
					t.Fatal("expected an error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("stampFile: %v", err)
			}
			if count != testCase.expectCount {
				t.Errorf("count = %d, want %d", count, testCase.expectCount)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read back: %v", err)
			}
			if string(got) != testCase.expectContent {
				t.Errorf("content = %q, want %q", got, testCase.expectContent)
			}
		})
	}
}

func TestReadPackageVersion(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		packageJSON string
		missingFile bool

		expect    string
		expectErr bool
	}{
		{
			name: "plain version",

			packageJSON: `{"name": "stack", "version": "1.4.2"}`,

			expect: "1.4.2",
		},
		{
			name: "missing version field",

			packageJSON: `{"name": "stack"}`,

			expectErr: true,
		},
		{
			name: "invalid json",

			packageJSON: `{`,

			expectErr: true,
		},
		{
			name: "missing file",

			missingFile: true,

			expectErr: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			if !testCase.missingFile {
				if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(testCase.packageJSON), 0o600); err != nil {
					t.Fatalf("write fixture: %v", err)
				}
			}

			got, err := readPackageVersion(root)
			if testCase.expectErr {
				if err == nil {
					t.Fatal("expected an error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("readPackageVersion: %v", err)
			}
			if got != testCase.expect {
				t.Errorf("version = %q, want %q", got, testCase.expect)
			}
		})
	}
}

func TestIsPnpmWorkspace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if isPnpmWorkspace(root) {
		t.Error("empty dir reported as workspace")
	}
	if err := os.WriteFile(filepath.Join(root, "pnpm-workspace.yaml"), []byte("packages:\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if !isPnpmWorkspace(root) {
		t.Error("dir with pnpm-workspace.yaml not reported as workspace")
	}
}

// initPublishRepo creates a git repo with one commit on master and a bare
// "origin" remote that already has that commit, so preflight's
// origin-comparison and dry-run push have something real to talk to.
func initPublishRepo(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	origin := t.TempDir()

	mustGit := func(dir string, args ...string) {
		t.Helper()
		if out, err := runGit(dir, args...); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	mustGit(origin, "init", "--bare", "--quiet", "--initial-branch=master")
	mustGit(root, "init", "--quiet", "--initial-branch=master")
	mustGit(root, "config", "user.email", "test@a-novel.dev")
	mustGit(root, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"version": "1.0.0"}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	mustGit(root, "add", "-A")
	mustGit(root, "commit", "--quiet", "-m", "init")
	mustGit(root, "remote", "add", "origin", origin)
	mustGit(root, "push", "--quiet", "-u", "origin", "master")
	return root
}

func TestPublishPreflight(t *testing.T) {
	t.Parallel()

	t.Run("clean synced master passes", func(t *testing.T) {
		t.Parallel()

		root := initPublishRepo(t)
		if err := publishPreflight(root, "master"); err != nil {
			t.Fatalf("preflight: %v", err)
		}
	})

	t.Run("dirty tree refused", func(t *testing.T) {
		t.Parallel()

		root := initPublishRepo(t)
		if err := os.WriteFile(filepath.Join(root, "junk.txt"), []byte("wip"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		if err := publishPreflight(root, "master"); err == nil || !strings.Contains(err.Error(), "not clean") {
			t.Fatalf("expected not-clean refusal, got %v", err)
		}
	})

	t.Run("feature branch refused", func(t *testing.T) {
		t.Parallel()

		root := initPublishRepo(t)
		if out, err := runGit(root, "checkout", "--quiet", "-b", "feat/x"); err != nil {
			t.Fatalf("checkout: %v\n%s", err, out)
		}
		if err := publishPreflight(root, "master"); err == nil || !strings.Contains(err.Error(), "current branch is feat/x") {
			t.Fatalf("expected branch refusal, got %v", err)
		}
	})

	t.Run("unpushed commit refused", func(t *testing.T) {
		t.Parallel()

		root := initPublishRepo(t)
		if out, err := runGit(root, "commit", "--quiet", "--allow-empty", "-m", "local-only"); err != nil {
			t.Fatalf("commit: %v\n%s", err, out)
		}
		if err := publishPreflight(root, "master"); err == nil || !strings.Contains(err.Error(), "not in sync") {
			t.Fatalf("expected sync refusal, got %v", err)
		}
	})
}

func TestGitToplevel(t *testing.T) {
	t.Parallel()

	root := initPublishRepo(t)
	sub := filepath.Join(root, "deep", "inside")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := gitToplevel(sub)
	if err != nil {
		t.Fatalf("gitToplevel: %v", err)
	}
	// Resolve symlinks on both sides — macOS TempDirs live under /private.
	wantResolved, _ := filepath.EvalSymlinks(root)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Errorf("toplevel = %q, want %q", gotResolved, wantResolved)
	}

	if _, err = gitToplevel(t.TempDir()); err == nil {
		t.Error("expected error outside a git repo, got none")
	}

	// An independent repo nested inside (a pulled checkout under app/) resolves
	// to ITSELF, not the outer repo — this is what scopes `publish` to a single
	// service when you cd into it.
	nested := filepath.Join(root, "app", "service-x")
	if err = os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if out, gErr := runGit(nested, "init", "--quiet", "--initial-branch=master"); gErr != nil {
		t.Fatalf("init nested: %v\n%s", gErr, out)
	}
	got, err = gitToplevel(nested)
	if err != nil {
		t.Fatalf("gitToplevel(nested): %v", err)
	}
	wantNested, _ := filepath.EvalSymlinks(nested)
	gotNested, _ := filepath.EvalSymlinks(got)
	if gotNested != wantNested {
		t.Errorf("nested toplevel = %q, want %q (must scope to the nested repo, not the outer)", gotNested, wantNested)
	}
}

func TestGitIgnored(t *testing.T) {
	t.Parallel()

	// app/ stands in for a pulled checkout (gitignored); pkg/ is a tracked
	// own package — the case that must keep publishing.
	root := t.TempDir()
	mustGit := func(args ...string) {
		t.Helper()
		if out, err := runGit(root, args...); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	mustGit("init", "--quiet", "--initial-branch=master")
	mustGit("config", "user.email", "test@a-novel.dev")
	mustGit("config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("app/\n"), 0o600); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	for _, dir := range []string{"app/service", "pkg/rest"} {
		p := filepath.Join(root, filepath.FromSlash(dir))
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(p, "package.json"), []byte("{}"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	mustGit("add", "-A")
	mustGit("commit", "--quiet", "-m", "init")

	testCases := []struct {
		name string

		rel    string
		expect bool
	}{
		{name: "gitignored pulled dir", rel: "app/service", expect: true},
		{name: "tracked own package", rel: "pkg/rest", expect: false},
		{name: "repo root is not ignored", rel: ".", expect: false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := gitIgnored(root, testCase.rel)
			if err != nil {
				t.Fatalf("gitIgnored(%q): %v", testCase.rel, err)
			}
			if got != testCase.expect {
				t.Errorf("gitIgnored(%q) = %v, want %v", testCase.rel, got, testCase.expect)
			}
		})
	}
}

func TestGoTagPrefix(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		// goMods are paths (slash-separated, relative to root) of files to
		// create and commit. The helper only counts those whose base name is
		// exactly "go.mod"; the others exercise the lookalike filter.
		goMods []string

		expect    string
		expectErr bool
	}{
		{
			name:   "module at repo root",
			goMods: []string{"go.mod"},
			expect: "",
		},
		{
			name:   "module in a sub-directory",
			goMods: []string{"cli/go.mod"},
			expect: "cli/",
		},
		{
			name:   "module in a nested sub-directory",
			goMods: []string{"a/b/go.mod"},
			expect: "a/b/",
		},
		{
			name:   "no go module",
			goMods: nil,
			expect: "",
		},
		{
			name:   "root module wins over a nested one",
			goMods: []string{"go.mod", "tools/go.mod"},
			expect: "",
		},
		{
			name:      "multiple sub-directory modules refused",
			goMods:    []string{"x/go.mod", "y/go.mod"},
			expectErr: true,
		},
		{
			name:   "ignores files merely ending in go.mod",
			goMods: []string{"sub/cargo.mod"},
			expect: "",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			mustGit := func(args ...string) {
				t.Helper()
				if out, err := runGit(root, args...); err != nil {
					t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
				}
			}
			mustGit("init", "--quiet", "--initial-branch=master")
			mustGit("config", "user.email", "test@a-novel.dev")
			mustGit("config", "user.name", "test")

			// Always keep at least one tracked file so the commit succeeds.
			if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("x\n"), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			for _, gm := range testCase.goMods {
				p := filepath.Join(root, filepath.FromSlash(gm))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(p, []byte("module example.com/x\n"), 0o600); err != nil {
					t.Fatalf("write go.mod: %v", err)
				}
			}
			mustGit("add", "-A")
			mustGit("commit", "--quiet", "-m", "init")

			got, err := goTagPrefix(root)
			if testCase.expectErr {
				if err == nil {
					t.Fatal("expected an error, got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("goTagPrefix: %v", err)
			}
			if got != testCase.expect {
				t.Errorf("prefix = %q, want %q", got, testCase.expect)
			}
		})
	}
}
