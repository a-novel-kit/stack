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

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/workspace/workspacetest"
)

// TestRunSandbox pins that the requested command runs in the temporary stack
// after its sync, that an allocated stack is always removed, even when the
// command fails, that a failed removal is reported, and that daemon commands
// are refused before any stack is allocated.
func TestRunSandbox(t *testing.T) {
	t.Parallel()

	t.Run("Success", func(t *testing.T) {
		t.Parallel()

		tempParent := t.TempDir()
		root := filepath.Join(tempParent, "sandbox")
		currentRepo := filepath.Join(tempParent, "worktree")
		cwd := filepath.Join(currentRepo, "internal")
		require.NoError(t, os.MkdirAll(cwd, 0o700))
		workspacetest.Git(t, currentRepo, "init", "--quiet")
		workspacetest.Git(t, currentRepo, "remote", "add", "origin", "git@github.com:a-novel/service-authentication.git")

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

		require.NoError(t, runSandbox(t.Context(), []string{"reconcile", "--all"}, system, stdout, stderr))
		require.True(t, removed, "removeAll not called")
		_, err := os.Stat(root)
		require.ErrorIs(t, err, os.ErrNotExist, "temporary stack not removed")
		require.Len(t, calls, 2, "want sync and the requested command")
		require.Equal(t, []string{commandCore, commandSync, "--root", root}, calls[0].args)
		require.Same(t, stderr, calls[0].stdout, "sync output must use stderr so command stdout stays clean")
		require.Equal(t, []string{"reconcile", "--all"}, calls[1].args)
		require.Equal(t, filepath.Join(root, "app", "service-authentication", "internal"), calls[1].dir)
		require.Same(t, stdout, calls[1].stdout, "requested command did not inherit stdout")
		require.Equal(t, []string{"sandbox:" + root}, envValues(calls[1].env, "A_NOVEL_STACKS"))
	})

	testCases := []struct {
		name, command, wantMsg string
		runErr, removeErr      error
		wantAllocated          bool
	}{
		{name: "Error/CommandFails", command: "failing-command", runErr: &ExitError{Code: 7}, wantMsg: "exit status 7", wantAllocated: true},
		{name: "Error/CleanupFails", command: "successful-command", removeErr: errors.New("busy"), wantMsg: "remove temporary stack", wantAllocated: true},
		{name: "Error/DaemonCommand/Core", command: commandCore, wantMsg: "user-wide daemon"},
		{name: "Error/DaemonCommand/Install", command: commandInstall, wantMsg: "user-wide daemon"},
		{name: "Error/DaemonCommand/Run", command: commandRun, wantMsg: "user-wide daemon"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
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
					return errors.Join(testCase.removeErr, os.RemoveAll(path))
				},
				run: func(_ context.Context, _ string, _, args []string, _, _ io.Writer) error {
					if args[0] == testCase.command {
						return testCase.runErr
					}
					return nil
				},
				cwd:     t.TempDir(),
				environ: []string{"PATH=/bin"},
			}

			err := runSandbox(t.Context(), []string{testCase.command}, system, io.Discard, io.Discard)
			require.ErrorContains(t, err, testCase.wantMsg)
			if testCase.runErr != nil {
				require.ErrorIs(t, err, testCase.runErr)
			}
			require.Equal(t, testCase.wantAllocated, allocated)
			require.Equal(t, allocated, removed, "want the stack removed whenever allocated")
		})
	}
}

func envValues(env []string, name string) []string {
	prefix := name + "="
	var values []string
	for _, entry := range env {
		if after, ok := strings.CutPrefix(entry, prefix); ok {
			values = append(values, after)
		}
	}
	return values
}
