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
			Codecov:  repocfg.CodecovAuto,
			Rulesets: repocfg.ClassRulesets{Master: true, RequireApproval: true},
		},
		Discovered: &repocfg.Discovered{
			Checks:      []repocfg.CheckRef{{Context: "lint-go"}, {Context: "test"}},
			CodeQLLangs: []string{"go"},
		},
		CodecovReports: true, // gates codecov: auto on
	}

	var buf bytes.Buffer
	renderSummary(&buf, target)
	got := buf.String()

	for _, want := range []string{
		"a-novel/service-auth", "class service",
		"Features", "squash", "auto-merge", "signoff",
		"CodeQL", "go (security-and-quality)",
		"codecov",           // auto + reports → enforced
		"lint-go, test (2)", // discovered checks
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestRenderSummaryCodecovAutoNoReports(t *testing.T) {
	t.Parallel()
	target := &repocfg.RepoTarget{
		Org: orgAnovel, Repo: "lib-x",
		Class: &repocfg.ClassPreset{
			Class:    repocfg.ClassLibrary,
			Codecov:  repocfg.CodecovAuto,
			Rulesets: repocfg.ClassRulesets{Master: true},
		},
		Discovered:     &repocfg.Discovered{},
		CodecovReports: false, // auto but no reports → no codecov ruleset
	}
	var buf bytes.Buffer
	renderSummary(&buf, target)
	if strings.Contains(buf.String(), "codecov") {
		t.Errorf("codecov should be absent when auto + no reports:\n%s", buf.String())
	}
}
