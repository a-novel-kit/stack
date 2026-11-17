package setup

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/a-novel-kit/stack/cli/internal/shared/podman"
)

// checkPodman verifies podman is on PATH and the daemon is responsive.
func checkPodman(ctx context.Context) error {
	if _, err := exec.LookPath("podman"); err != nil {
		return errors.New("podman not on PATH; install podman first (https://podman.io)")
	}
	if err := podman.Run(ctx, nil, "info", "--format", "{{.Host.Hostname}}"); err != nil {
		return fmt.Errorf("`podman info` failed: %w (is podman service running?)", err)
	}
	return nil
}

// checkGit verifies git is on PATH.
func checkGit() error {
	if _, err := exec.LookPath("git"); err != nil {
		return errors.New("git not on PATH; install git first")
	}
	return nil
}

// checkGitHubSSH runs a non-destructive `ssh -T git@github.com`. GitHub exits 1
// with "successfully authenticated, but GitHub does not provide shell access",
// which is the success signal. Any other response means SSH access is broken.
func checkGitHubSSH(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "ssh", "-T", "-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=5", "-o", "BatchMode=yes", "git@github.com")
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "successfully authenticated") {
		return nil
	}
	return fmt.Errorf("ssh to git@github.com didn't produce the expected success banner (output: %q)",
		truncate(strings.TrimSpace(string(out)), 100))
}

// truncate keeps the first maxLen runes of s, marking a cut with "...".
func truncate(s string, maxLen int) string {
	if r := []rune(s); len(r) > maxLen {
		return string(r[:maxLen]) + "..."
	}
	return s
}
