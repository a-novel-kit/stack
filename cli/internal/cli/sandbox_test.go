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
	if err := os.Mkdir(currentRepo, 0o700); err != nil {
		t.Fatal(err)
	}
	mustGit(t, currentRepo, "init", "--quiet")
	mustGit(t, currentRepo, "remote", "add", "origin", "git@github.com:a-novel/service-authentication.git")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}

	type call struct {
		dir    string
		env    []string
		args   []string
		stdout io.Writer
	}
	var calls []call
	removed := false
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	system := sandboxSystem{
		tempDir: func() (string, error) {
			if err := os.Mkdir(root, 0o700); err != nil {
				return "", err
			}
			return root, nil
		},
		clone: func(path string) error {
			return os.MkdirAll(filepath.Join(path, "app", "service-authentication", "internal"), 0o700)
		},
		removeAll: func(path string) error {
			removed = true
			return os.RemoveAll(path)
		},
		run: func(
			_ context.Context,
			dir string,
			env []string,
			args []string,
			commandOut io.Writer,
			_ io.Writer,
		) error {
			calls = append(calls, call{
				dir:    dir,
				env:    slices.Clone(env),
				args:   slices.Clone(args),
				stdout: commandOut,
			})
			return nil
		},
		cwd:     cwd,
		environ: []string{"PATH=/bin", "A_NOVEL_STACKS=old:/old"},
	}

	err := runSandbox(
		context.Background(),
		[]string{"reconcile", "--all"},
		system,
		stdout,
		stderr,
	)
	if err != nil {
		t.Fatalf("runSandbox: %v", err)
	}
	if !removed {
		t.Fatal("temporary stack was not removed")
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary stack still exists: %v", err)
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
	wantDir := filepath.Join(root, "app", "service-authentication", "internal")
	if calls[1].dir != wantDir {
		t.Errorf("command dir = %q, want %q", calls[1].dir, wantDir)
	}
	if calls[1].stdout != stdout {
		t.Error("requested command did not inherit stdout")
	}
	if got := envValues(calls[1].env, "A_NOVEL_STACKS"); !slices.Equal(got, []string{"sandbox:" + root}) {
		t.Errorf("A_NOVEL_STACKS = %v", got)
	}
}

func TestRunSandboxRemovesStackAfterCommandFailure(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sandbox")
	runs := 0
	removed := false
	system := sandboxSystem{
		tempDir: func() (string, error) {
			return root, os.Mkdir(root, 0o700)
		},
		clone: func(string) error { return nil },
		removeAll: func(path string) error {
			removed = true
			return os.RemoveAll(path)
		},
		run: func(context.Context, string, []string, []string, io.Writer, io.Writer) error {
			runs++
			if runs == 2 {
				return &ExitError{Code: 7}
			}
			return nil
		},
		cwd:     t.TempDir(),
		environ: []string{"PATH=/bin"},
	}

	err := runSandbox(context.Background(), []string{"failing-command"}, system, io.Discard, io.Discard)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Code != 7 {
		t.Fatalf("error = %v, want exit code 7", err)
	}
	if !removed {
		t.Fatal("temporary stack was not removed after command failure")
	}
}

func TestRunSandboxReportsCleanupFailure(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "sandbox")
	system := sandboxSystem{
		tempDir:   func() (string, error) { return root, os.Mkdir(root, 0o700) },
		clone:     func(string) error { return nil },
		removeAll: func(string) error { return errors.New("busy") },
		run: func(context.Context, string, []string, []string, io.Writer, io.Writer) error {
			return nil
		},
		cwd:     t.TempDir(),
		environ: []string{"PATH=/bin"},
	}

	err := runSandbox(context.Background(), []string{"successful-command"}, system, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "remove temporary stack") {
		t.Fatalf("error = %v, want cleanup failure", err)
	}
}

func TestRunSandboxRefusesDaemonCommandsBeforeAllocating(t *testing.T) {
	t.Parallel()

	for _, command := range []string{commandCore, commandInstall, commandRun} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			allocated := false
			system := sandboxSystem{
				tempDir: func() (string, error) {
					allocated = true
					return t.TempDir(), nil
				},
			}
			err := runSandbox(context.Background(), []string{command}, system, io.Discard, io.Discard)
			if err == nil || !strings.Contains(err.Error(), "user-wide daemon") {
				t.Fatalf("error = %v", err)
			}
			if allocated {
				t.Error("sandbox was allocated for an unsupported command")
			}
		})
	}
}

func TestRootRejectsMisplacedSandboxFlag(t *testing.T) {
	t.Parallel()

	root := NewRoot(LegacyHandlers{})
	root.SetArgs([]string{"secrets", "ls", "--sandbox"})
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	err := root.Execute()
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
