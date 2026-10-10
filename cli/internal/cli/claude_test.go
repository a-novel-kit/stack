package cli

import (
	"strings"
	"testing"
)

// TestClaudeCmdForwardsFlags pins the point of `a-novel claude`: every flag
// reaches claude, so `a-novel claude --continue` works.
func TestClaudeCmdForwardsFlags(t *testing.T) {
	cmd := newClaudeCmd()
	if !cmd.DisableFlagParsing {
		t.Error("DisableFlagParsing must stay on so claude's own flags pass through")
	}
	if !strings.Contains(cmd.Long, "a-novel help claude") {
		t.Error("Long help must point at 'a-novel help claude', since --help goes to claude")
	}
}
