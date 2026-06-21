package repocfg

import (
	"strings"
	"testing"
)

func TestRenderCodeQL(t *testing.T) {
	t.Parallel()

	t.Run("named suite emits queries + filter in config", func(t *testing.T) {
		t.Parallel()
		out, err := RenderCodeQL([]string{"go", "actions"}, "security-and-quality", "master")
		if err != nil {
			t.Fatalf("RenderCodeQL: %v", err)
		}
		for _, want := range []string{
			`language: ["go", "actions"]`,
			`branches: ["master"]`,
			"config: |",
			"queries:",
			"- uses: security-and-quality",
			"query-filters:",
			"- exclude:",
			"id: actions/unpinned-tag",
		} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
		// The bare placeholder must be fully substituted.
		if strings.Contains(out, "__") {
			t.Errorf("unsubstituted placeholder remains:\n%s", out)
		}
	})

	t.Run("default suite omits queries but keeps the filter", func(t *testing.T) {
		t.Parallel()
		out, err := RenderCodeQL([]string{"go"}, "", "main")
		if err != nil {
			t.Fatalf("RenderCodeQL: %v", err)
		}
		if strings.Contains(out, "queries:") {
			t.Errorf("default suite must omit queries:\n%s", out)
		}
		for _, want := range []string{"config: |", "query-filters:", "id: actions/unpinned-tag"} {
			if !strings.Contains(out, want) {
				t.Errorf("missing %q in:\n%s", want, out)
			}
		}
	})
}
