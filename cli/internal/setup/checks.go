package setup

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// checkPodman verifies podman is on PATH and the daemon is responsive.
func checkPodman() error {
	if _, err := exec.LookPath("podman"); err != nil {
		return errors.New("podman not on PATH; install podman first (https://podman.io)")
	}
	cmd := exec.Command("podman", "info", "--format", "{{.Host.Hostname}}")
	if err := cmd.Run(); err != nil {
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

// checkGitHubSSH does a non-destructive `ssh -T git@github.com`. GitHub
// returns exit 1 with the string "successfully authenticated, but
// GitHub does not provide shell access" — that's our success
// indicator. Any other response means SSH access isn't working.
func checkGitHubSSH() error {
	cmd := exec.Command("ssh", "-T", "-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=5", "-o", "BatchMode=yes", "git@github.com")
	out, _ := cmd.CombinedOutput()
	if strings.Contains(string(out), "successfully authenticated") {
		return nil
	}
	return fmt.Errorf("ssh to git@github.com didn't produce the expected success banner (output: %q)",
		truncate(strings.TrimSpace(string(out)), 100))
}

func truncate(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "..."
	}
	return s
}
