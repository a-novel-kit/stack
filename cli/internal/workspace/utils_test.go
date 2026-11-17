package workspace_test

import (
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/workspace/workspacetest"
)

func TestMain(m *testing.M) {
	workspacetest.IsolateGit()
	m.Run()
}
