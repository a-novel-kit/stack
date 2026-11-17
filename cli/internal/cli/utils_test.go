package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/a-novel-kit/stack/cli/internal/workspace/workspacetest"
)

// Tests never wait out GitHub's secondary rate limit.
func init() { rateLimitWait = 0 }

func TestMain(m *testing.M) {
	workspacetest.IsolateGit()
	m.Run()
}

// Failures the stubbed gh returns, worded as gh reports them.
var errGHNotFound = errors.New("gh: Not Found (HTTP 404)")

// stubGH routes every gh call through respond, which sees the args joined.
func stubGH(t *testing.T, respond func(call string) (string, error)) {
	t.Helper()
	workspacetest.Swap(t, &ghExec, func(args ...string) (string, error) {
		return respond(strings.Join(args, " "))
	})
}

// runCmd executes cmd with args and returns its combined output.
func runCmd(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	cmd.SetArgs(args)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	err := cmd.Execute()
	return out.String(), err
}

// writeFixture writes content to dir/name, creating parent directories.
func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		panic(err)
	}
}

func readFixture(t *testing.T, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		panic(err)
	}
	return string(b)
}
