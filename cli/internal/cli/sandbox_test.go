package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestSandboxArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		args    []string
		want    []string
		enabled bool
		wantErr bool
	}{
		{name: "leading flag", args: []string{"--sandbox", "alpha"}, want: []string{"alpha"}, enabled: true},
		{name: "missing flag", args: []string{"beta"}, want: []string{"beta"}},
		{name: "later flag", args: []string{"gamma", "--sandbox"}, wantErr: true},
		{name: "duplicate flag", args: []string{"--sandbox", "--sandbox", "gamma"}, wantErr: true},
		{name: "child flag after separator", args: []string{"gamma", "--", "--sandbox"}, want: []string{"gamma", "--", "--sandbox"}},
		{name: "empty invocation"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, enabled, err := SandboxArgs(test.args)
			if (err != nil) != test.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if enabled != test.enabled {
				t.Errorf("enabled = %v, want %v", enabled, test.enabled)
			}
			if !slices.Equal(got, test.want) {
				t.Errorf("args = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRunSandbox(t *testing.T) {
	t.Parallel()

	tempParent := t.TempDir()
	root := filepath.Join(tempParent, "sandbox")
	currentRepo := filepath.Join(tempParent, "worktree")
	cwd := filepath.Join(currentRepo, "internal")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	mustGit(t, currentRepo, "init", "--quiet")
	mustGit(t, currentRepo, "remote", "add", "origin", "git@github.com:a-novel/service-authentication.git")

	type call struct {
		dir       string
		env, args []string
		stdout    io.Writer
	}
	var calls []call
	removed := false
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	system := sandboxSystem{
		tempDir: func() (string, error) { return root, os.Mkdir(root, 0o700) },
		clone: func(path string) error {
			return os.MkdirAll(filepath.Join(path, "app", "service-authentication", "internal"), 0o700)
		},
		removeAll: func(path string) error {
			removed = true
			return os.RemoveAll(path)
		},
		run: func(_ context.Context, dir string, env, args []string, commandOut, _ io.Writer) error {
			calls = append(calls, call{dir: dir, env: slices.Clone(env), args: slices.Clone(args), stdout: commandOut})
			return nil
		},
		cwd:     cwd,
		environ: []string{"PATH=/bin", "A_NOVEL_STACKS=old:/old"},
	}

	if err := runSandbox(t.Context(), []string{"reconcile", "--all"}, system, stdout, stderr); err != nil {
		t.Fatalf("runSandbox: %v", err)
	}
	if _, err := os.Stat(root); !removed || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary stack not removed (removeAll called: %v, stat: %v)", removed, err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want sync and requested command", len(calls))
	}
	if !slices.Equal(calls[0].args, []string{commandCore, commandSync, "--root", root}) {
		t.Errorf("sync args = %v", calls[0].args)
	}
	if calls[0].stdout != stderr {
		t.Error("sync output must use stderr so command stdout stays clean")
	}
	if !slices.Equal(calls[1].args, []string{"reconcile", "--all"}) {
		t.Errorf("command args = %v", calls[1].args)
	}
	if wantDir := filepath.Join(root, "app", "service-authentication", "internal"); calls[1].dir != wantDir {
		t.Errorf("command dir = %q, want %q", calls[1].dir, wantDir)
	}
	if calls[1].stdout != stdout {
		t.Error("requested command did not inherit stdout")
	}
	if got := envValues(calls[1].env, "A_NOVEL_STACKS"); !slices.Equal(got, []string{"sandbox:" + root}) {
		t.Errorf("A_NOVEL_STACKS = %v", got)
	}
}

// TestRunSandboxFailures pins that an allocated stack is always removed, even
// when the command fails, that a failed removal is reported, and that daemon
// commands are refused before any stack is allocated.
func TestRunSandboxFailures(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, command, wantMsg string
		runErr, removeErr      error
		wantAllocated          bool
	}{
		{name: "Error/CommandFails", command: "failing-command", runErr: &ExitError{Code: 7}, wantMsg: "exit status 7", wantAllocated: true},
		{name: "Error/CleanupFails", command: "successful-command", removeErr: errors.New("busy"), wantMsg: "remove temporary stack", wantAllocated: true},
		{name: "Error/DaemonCommand/core", command: commandCore, wantMsg: "user-wide daemon"},
		{name: "Error/DaemonCommand/install", command: commandInstall, wantMsg: "user-wide daemon"},
		{name: "Error/DaemonCommand/run", command: commandRun, wantMsg: "user-wide daemon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), "sandbox")
			allocated, removed := false, false
			system := sandboxSystem{
				tempDir: func() (string, error) {
					allocated = true
					return root, os.Mkdir(root, 0o700)
				},
				clone: func(string) error { return nil },
				removeAll: func(path string) error {
					removed = true
					return errors.Join(tc.removeErr, os.RemoveAll(path))
				},
				run: func(_ context.Context, _ string, _, args []string, _, _ io.Writer) error {
					if args[0] == tc.command {
						return tc.runErr
					}
					return nil
				},
				cwd:     t.TempDir(),
				environ: []string{"PATH=/bin"},
			}

			err := runSandbox(t.Context(), []string{tc.command}, system, io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tc.wantMsg) || (tc.runErr != nil && !errors.Is(err, tc.runErr)) {
				t.Fatalf("error = %v, want %q", err, tc.wantMsg)
			}
			if allocated != tc.wantAllocated || removed != allocated {
				t.Errorf("allocated = %v, removed = %v; want allocated %v and removed whenever allocated", allocated, removed, tc.wantAllocated)
			}
		})
	}
}

func TestRootRejectsMisplacedSandboxFlag(t *testing.T) {
	t.Parallel()

	_, err := runCmd(t, NewRoot(), "secrets", "ls", "--sandbox")
	if err == nil || !strings.Contains(err.Error(), "must be the first argument") {
		t.Fatalf("error = %v", err)
	}
}

func envValues(env []string, name string) []string {
	prefix := name + "="
	var values []string
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			values = append(values, strings.TrimPrefix(entry, prefix))
		}
	}
	return values
}
