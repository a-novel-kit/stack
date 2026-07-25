package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/setup"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

const sandboxFlag = "--sandbox"

type sandboxSystem struct {
	tempDir   func() (string, error)
	clone     func(string) error
	removeAll func(string) error
	run       func(context.Context, string, []string, []string, io.Writer, io.Writer) error
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
		tempDir: func() (string, error) {
			return os.MkdirTemp("", "a-novel-sandbox-*")
		},
		clone:     setup.CloneStack,
		removeAll: os.RemoveAll,
		run: func(
			ctx context.Context,
			dir string,
			env []string,
			args []string,
			commandOut io.Writer,
			commandErr io.Writer,
		) error {
			cmd := exec.CommandContext(ctx, executable, args...)
			cmd.Dir = dir
			cmd.Env = env
			cmd.Stdin = stdin
			cmd.Stdout = commandOut
			cmd.Stderr = commandErr
			if err := cmd.Run(); err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					code := exitErr.ExitCode()
					if code < 0 && ctx.Err() != nil {
						code = 130
					}
					return &ExitError{Code: code}
				}
				return err
			}
			return nil
		},
		cwd:     cwd,
		environ: os.Environ(),
	}
	return runSandbox(ctx, args, system, stdout, stderr)
}

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
	if err := system.run(
		ctx,
		root,
		env,
		[]string{commandCore, commandSync, "--root", root},
		stderr,
		stderr,
	); err != nil {
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

func unsupportedSandboxCommand(command string) bool {
	switch command {
	case commandCore, commandInstall, commandRun:
		return true
	default:
		return false
	}
}

func sandboxWorkingDir(root, cwd string) (string, error) {
	repoRoot, org, repo, ok := sandboxRepoIdentity(cwd)
	if !ok {
		return root, nil
	}
	sandboxRepo := root
	switch {
	case org == orgAnovelKit && repo == stackLabel:
	case org == orgAnovelKit:
		sandboxRepo = filepath.Join(root, "kit", repo)
	case org == orgAnovel:
		sandboxRepo = filepath.Join(root, "app", repo)
	default:
		return root, nil
	}
	rel, err := filepath.Rel(repoRoot, cwd)
	if err != nil {
		return "", fmt.Errorf("map working directory: %w", err)
	}
	mapped := filepath.Join(sandboxRepo, rel)
	info, statErr := os.Stat(mapped)
	if statErr != nil || !info.IsDir() {
		return "", fmt.Errorf("working directory %s/%s:%s is absent from the temporary stack", org, repo, rel)
	}
	return mapped, nil
}

func sandboxRepoIdentity(cwd string) (string, string, string, bool) {
	repoRoot, err := gitToplevel(cwd)
	if err != nil {
		return "", "", "", false
	}
	org, repo, err := repoFromGitRemote(repoRoot)
	if err != nil {
		return "", "", "", false
	}
	return repoRoot, org, repo, true
}

func replaceEnv(env []string, name, value string) []string {
	prefix := name + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}
