package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewClaudeCmd pins the point of `a-novel claude`: every flag reaches
// claude, so `a-novel claude --continue` works.
func TestNewClaudeCmd(t *testing.T) {
	t.Parallel()

	cmd := newClaudeCmd()
	require.True(t, cmd.DisableFlagParsing, "DisableFlagParsing must stay on so claude's own flags pass through")
	require.Contains(t, cmd.Long, "a-novel help claude", "Long help must point at 'a-novel help claude', since --help goes to claude")
}
