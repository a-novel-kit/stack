package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/setup"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

const sandboxFlag = "--sandbox"

// sandboxSystem is the filesystem and process access runSandbox works
// through, so tests run it without cloning or spawning anything.
type sandboxSystem struct {
	tempDir   func() (string, error)
	clone     func(string) error
	removeAll func(string) error
	run       func(ctx context.Context, dir string, env, args []string, stdout, stderr io.Writer) error
	cwd       string
	environ   []string
}

// SandboxArgs removes a leading --sandbox flag and reports whether the
// invocation needs an ephemeral stack.
func SandboxArgs(args []string) ([]string, bool, error) {
	found := false
	for index, arg := range args {
		if arg == "--" {
			break
		}
		if arg != sandboxFlag {
			continue
		}
		if index != 0 || found {
			return nil, false, errors.New("--sandbox must be the first argument and may appear once")
		}
		found = true
	}
	if found {
		return args[1:], true, nil
	}
	return args, false, nil
}

// RunSandbox prepares an ephemeral stack and runs args inside it. The stack is
// removed before RunSandbox returns.
func RunSandbox(
	ctx context.Context,
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	system := sandboxSystem{
		tempDir:   func() (string, error) { return os.MkdirTemp("", "a-novel-sandbox-*") },
		clone:     setup.CloneStack,
		removeAll: os.RemoveAll,
		run: func(ctx context.Context, dir string, env, args []string, commandOut, commandErr io.Writer) error {
			cmd := exec.CommandContext(ctx, executable, args...)
			cmd.Dir, cmd.Env = dir, env
			cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, commandOut, commandErr
			return childExit(ctx, cmd.Run())
		},
		cwd:     cwd,
		environ: os.Environ(),
	}
	return runSandbox(ctx, args, system, stdout, stderr)
}

// childExit carries a child's exit status as an ExitError. A child killed by
// the interrupt that cancelled ctx exits as interrupted.
func childExit(ctx context.Context, err error) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return err
	}
	code := exitErr.ExitCode()
	if code < 0 && ctx.Err() != nil {
		code = exitAborted
	}
	return &ExitError{Code: code}
}

// runSandbox clones a temporary stack, populates it with `core sync`, and runs
// args in it from the directory matching the caller's.
func runSandbox(
	ctx context.Context,
	args []string,
	system sandboxSystem,
	stdout io.Writer,
	stderr io.Writer,
) (runErr error) {
	if len(args) == 0 {
		return errors.New("a command is required after --sandbox")
	}
	if unsupportedSandboxCommand(args[0]) {
		return fmt.Errorf("--sandbox cannot wrap %q because it uses the user-wide daemon", args[0])
	}

	root, err := system.tempDir()
	if err != nil {
		return fmt.Errorf("create temporary stack: %w", err)
	}
	defer func() {
		if err := system.removeAll(root); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("remove temporary stack %s: %w", root, err))
			return
		}
		_, _ = fmt.Fprintf(stderr, "✓ Removed sandbox %s\n", root)
	}()

	_, _ = fmt.Fprintf(stderr, "▸ Preparing sandbox %s\n", root)
	if err := system.clone(root); err != nil {
		return fmt.Errorf("clone stack: %w", err)
	}

	env := replaceEnv(system.environ, stacks.EnvVar, "sandbox:"+root)
	syncArgs := []string{commandCore, commandSync, "--root", root}
	if err := system.run(ctx, root, env, syncArgs, stderr, stderr); err != nil {
		var exitErr *ExitError
		if errors.As(err, &exitErr) {
			return err
		}
		return fmt.Errorf("populate sandbox: %w", err)
	}

	dir, err := sandboxWorkingDir(root, system.cwd)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintln(stderr, "▸ Running in sandbox")
	return system.run(ctx, dir, env, args, stdout, stderr)
}

// unsupportedSandboxCommand reports whether command drives the user-wide
// daemon, which a temporary stack cannot isolate.
func unsupportedSandboxCommand(command string) bool {
	return slices.Contains([]string{commandCore, commandInstall, commandRun}, command)
}

// sandboxWorkingDir maps cwd, when it sits in a workspace repository, onto the
// same directory of that repository's copy in the temporary stack at root.
func sandboxWorkingDir(root, cwd string) (string, error) {
	repoRoot, err := gitToplevel(cwd)
	if err != nil {
		return root, nil //nolint:nilerr // outside a repository, the command runs from the stack root
	}
	repo, err := repoFromGitRemote(repoRoot)
	if err != nil || (repo.Org != orgAnovel && repo.Org != orgAnovelKit) {
		return root, nil //nolint:nilerr // a foreign repository has no copy in the stack
	}
	rel, err := filepath.Rel(repoRoot, cwd)
	if err != nil {
		return "", fmt.Errorf("map working directory: %w", err)
	}
	mapped := filepath.Join(repo.Dir(root), rel)
	if info, err := os.Stat(mapped); err != nil || !info.IsDir() {
		return "", fmt.Errorf("working directory %s:%s is absent from the temporary stack", repo.FullName(), rel)
	}
	return mapped, nil
}

// replaceEnv returns env with name set to value, and any earlier value dropped.
func replaceEnv(env []string, name, value string) []string {
	prefix := name + "="
	out := slices.DeleteFunc(slices.Clone(env), func(entry string) bool { return strings.HasPrefix(entry, prefix) })
	return append(out, prefix+value)
}
