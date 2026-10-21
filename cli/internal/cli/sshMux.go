package cli

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
)

// githubSSHHost is the login behind every SSH remote of the workspace.
const githubSSHHost = "git@github.com"

// githubSessionCap is how many sessions GitHub accepts on one SSH connection.
// It refuses the next one, and ssh then opens a connection of its own.
const githubSessionCap = 10

// ownMuxOpts multiplex GitHub connections when the user's SSH config does not.
// The connection outlives the run, so a concurrent or follow-up run reuses it.
var ownMuxOpts = []string{
	"-o", "ControlMaster=auto",
	"-o", "ControlPath=~/.ssh/a-novel-%C",
	"-o", "ControlPersist=10m",
}

// sshMux shares one authenticated SSH connection to GitHub between git calls,
// so a hardware security key asks for one touch rather than one per repository.
type sshMux struct {
	mu sync.Mutex
	// opts are the ssh options that reach the connection, nil when the user's
	// own SSH config multiplexes.
	opts []string
	// slots caps concurrent sessions at githubSessionCap; nil until open succeeds.
	slots chan struct{}
}

// githubMux is the process-wide connection a multi-repo command opens.
var githubMux sshMux

// open starts sharing for the rest of the process. It leaves git's SSH setup
// alone when the user customized the ssh command, or when no connection opens.
func (m *sshMux) open() {
	if os.Getenv("GIT_SSH_COMMAND") != "" {
		return
	}
	if out, _ := exec.Command("git", "config", "--get", "core.sshCommand").Output(); len(out) > 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.opts = ownMuxOpts
	if out, err := exec.Command("ssh", "-G", githubSSHHost).Output(); err == nil &&
		strings.Contains(string(out), "\ncontrolpath ") {
		m.opts = nil
	}
	if m.connect() {
		m.slots = make(chan struct{}, githubSessionCap)
	}
}

// connect reports whether the connection is up, and opens it when it is not.
// Callers hold m.mu, so a lapsed connection reopens once. Opening prompts for
// the key's touch on the terminal.
func (m *sshMux) connect() bool {
	if exec.Command("ssh", slices.Concat(m.opts, []string{"-O", "check", githubSSHHost})...).Run() == nil {
		return true
	}
	start := exec.Command("ssh", slices.Concat(m.opts, []string{"-fN", githubSSHHost})...)
	start.Stderr = os.Stderr
	return start.Run() == nil
}

// acquire takes a session on the connection, reopening it if it lapsed, and
// returns the environment git needs to use it plus the session's release.
// Before open, or once the connection cannot reopen, git connects on its own.
func (m *sshMux) acquire() ([]string, func()) {
	if m.slots == nil {
		return nil, func() {}
	}
	m.slots <- struct{}{}
	release := func() { <-m.slots }

	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.connect() || m.opts == nil {
		return nil, release
	}
	return []string{"GIT_SSH_COMMAND=ssh " + strings.Join(m.opts, " ")}, release
}
