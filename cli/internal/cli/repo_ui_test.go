package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/repocfg"
)

func TestRenderSummary(t *testing.T) {
	t.Parallel()
	target := &repocfg.RepoTarget{
		Org:  orgAnovel,
		Repo: "service-auth",
		Class: &repocfg.ClassPreset{
			Class:    repocfg.ClassService,
			Features: repocfg.Features{Issues: true, Projects: true},
			Merge:    repocfg.Merge{Squash: true, AutoMerge: true, SignoffRequired: true},
			Security: repocfg.SecurityToggles{SecretScanning: true, PushProtection: true, Dependabot: true},
			CodeQL:   repocfg.CodeQLPreset{Enabled: true, QuerySuite: "security-and-quality"},
			Rulesets: repocfg.ClassRulesets{Master: true, RequireApproval: true},
		},
		Discovered: &repocfg.Discovered{
			Checks:      []repocfg.CheckRef{{Context: "lint-go"}, {Context: "test"}},
			CodeQLLangs: []string{"go"},
		},
	}

	var buf bytes.Buffer
	renderSummary(&buf, target)
	got := buf.String()

	for _, want := range []string{
		"a-novel/service-auth", "class service",
		"Features", "squash", "auto-merge", "signoff",
		"CodeQL", "go (security-and-quality)",
		"lint-go, test (2)", // discovered checks
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\n--- got ---\n%s", want, got)
		}
	}
}

// TestRenderSummaryOmitsRetiredRulesets guards the summary against announcing a
// ruleset repocfg no longer applies. The summary is what an operator reads before
// confirming a reconcile, so listing a retired gate would describe protection the
// plan is in fact about to delete — the most misleading thing this view could say.
func TestRenderSummaryOmitsRetiredRulesets(t *testing.T) {
	t.Parallel()
	target := &repocfg.RepoTarget{
		Org: orgAnovel, Repo: "lib-x",
		Class: &repocfg.ClassPreset{
			Class:    repocfg.ClassLibrary,
			Rulesets: repocfg.ClassRulesets{Master: true, RequireApproval: true, Tags: true},
		},
		Discovered: &repocfg.Discovered{},
	}
	var buf bytes.Buffer
	renderSummary(&buf, target)
	if strings.Contains(buf.String(), "codecov") {
		t.Errorf("summary still lists the retired codecov ruleset:\n%s", buf.String())
	}
}
