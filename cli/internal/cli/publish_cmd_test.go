package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestResolveStampTargets(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, rel := range []string{"a/x/action.yaml", "a/y/action.yaml", "b/z/action.yaml", "top.yaml", "weird[1].yaml"} {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// glob across two levels matches the three action files, not top.yaml.
	got, err := resolveStampTargets([]string{filepath.Join(dir, "*", "*", "action.yaml")})
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("glob matched %d files, want 3: %v", len(got), got)
	}

	// a literal path resolves to itself.
	got, err = resolveStampTargets([]string{filepath.Join(dir, "top.yaml")})
	if err != nil || len(got) != 1 {
		t.Fatalf("literal: got %v err %v", got, err)
	}

	// a literal filename containing glob metacharacters resolves to itself,
	// not to whatever the brackets would match as a pattern.
	weird := filepath.Join(dir, "weird[1].yaml")
	got, err = resolveStampTargets([]string{weird})
	if err != nil || len(got) != 1 || got[0] != weird {
		t.Fatalf("literal-with-metachars: got %v err %v, want [%s]", got, err, weird)
	}

	// overlapping patterns de-dupe.
	got, err = resolveStampTargets([]string{
		filepath.Join(dir, "a", "*", "action.yaml"),
		filepath.Join(dir, "*", "*", "action.yaml"),
	})
	if err != nil || len(got) != 3 {
		t.Fatalf("dedupe: got %d %v err %v", len(got), got, err)
	}

	// nothing matched is an error (catches typos).
	if _, err := resolveStampTargets([]string{filepath.Join(dir, "nope", "*.yaml")}); err == nil {
		t.Fatal("expected an error when nothing matches")
	}
}

// initPublishRepo creates a git repo with one commit on master and a bare
// "origin" remote that already has that commit, so commands that compare HEAD
// against origin have something real to talk to.
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
