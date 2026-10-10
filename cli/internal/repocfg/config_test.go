package repocfg

import (
	"regexp"
	"slices"
	"testing"
)

func TestLoadAllClasses(t *testing.T) {
	t.Parallel()
	for _, c := range AllClasses {
		t.Run(string(c), func(t *testing.T) {
			t.Parallel()
			p, err := LoadClass(c)
			if err != nil {
				t.Fatalf("LoadClass(%s): %v", c, err)
			}
			if p.Class != c {
				t.Fatalf("class field = %q, want %q", p.Class, c)
			}
			if p.CodeQuality {
				t.Error("code_quality = true, want false for every managed class")
			}
			if p.Features.PullRequests != "collaborators_only" {
				t.Errorf("pull_requests = %q, want collaborators_only for every managed class", p.Features.PullRequests)
			}
		})
	}
}

func TestDetectClass(t *testing.T) {
	t.Parallel()
	cases := map[string]Class{
		"service-authentication": ClassService,
		"service-json-keys":      ClassService,
		"infra":                  ClassInfra,
		"workflows":              ClassWorkflows,
		".github":                ClassMeta,
		"golib":                  ClassLibrary,
		"stack":                  ClassLibrary,
		"nodelib":                ClassLibrary,
		"platform-web":           ClassPlatform,
		"platform-studio":        ClassPlatform,
	}
	for repo, want := range cases {
		t.Run(repo, func(t *testing.T) {
			t.Parallel()
			if got := DetectClass(repo); got != want {
				t.Errorf("DetectClass(%q) = %q, want %q", repo, got, want)
			}
		})
	}
}

// TestLoadOrgs pins each org profile and its [Agent] app id resolution. The two
// orgs run separate Apps, so each org's merge-gate integration comes from its
// own profile. Only that org's own app id can satisfy its required check.
func TestLoadOrgs(t *testing.T) {
	t.Parallel()
	for org, agent := range map[string]int64{"a-novel": 3549319, "a-novel-kit": 3549379} {
		t.Run(org, func(t *testing.T) {
			t.Parallel()
			o, err := LoadOrg(org)
			if err != nil {
				t.Fatalf("LoadOrg(%s): %v", org, err)
			}
			if o.Org != org {
				t.Fatalf("org field = %q, want %q", o.Org, org)
			}
			for _, bot := range []string{"dependencies", "agent", "publish"} {
				if o.Bots[bot] == 0 {
					t.Fatalf("%s: bots.%s missing", org, bot)
				}
			}
			c, err := LoadChecks()
			if err != nil {
				t.Fatalf("LoadChecks: %v", err)
			}
			c.ResolveBotIntegrations(o)
			if got := c.Integrations["agent"]; got != agent {
				t.Errorf("%s agent integration = %d, want %d", org, got, agent)
			}
		})
	}
}

func TestLoadChecks(t *testing.T) {
	t.Parallel()
	c, err := LoadChecks()
	if err != nil {
		t.Fatalf("LoadChecks: %v", err)
	}
	if c.Integrations["actions"] == 0 || len(c.Always) == 0 {
		t.Fatalf("integrations.actions = %d, always = %v, want both set", c.Integrations["actions"], c.Always)
	}
	// merge-gate is required via the [Agent] App, whose id is per-org — so it must
	// NOT be hardcoded here; it's injected from the org profile (see TestLoadOrgs).
	if _, hardcoded := c.Integrations["agent"]; hardcoded {
		t.Error("integrations.agent must not be hardcoded — the [Agent] app id is per-org")
	}
	if !slices.Contains(c.Always, CheckDef{Context: "merge-gate", Integration: "agent"}) {
		t.Errorf("always = %v, want merge-gate via the agent integration", c.Always)
	}
	// main.yaml jobs are required by default; the exclusions drop reporting and
	// master-only jobs.
	if !slices.Contains(c.Exclude.Prefixes, "report-") || len(c.Exclude.IfContains) == 0 {
		t.Errorf("exclude = %+v, want the report- prefix and the master-only guard", c.Exclude)
	}
}

func TestLoadLabels(t *testing.T) {
	t.Parallel()
	l, err := LoadLabels()
	if err != nil {
		t.Fatalf("LoadLabels: %v", err)
	}
	named := func(name string) int {
		return slices.IndexFunc(l.Ensure, func(d LabelDef) bool { return d.Name == name })
	}
	// meta is the no-PR / meta-epic label.
	if meta := named("meta"); meta < 0 || l.Ensure[meta].Color != "bfd4f2" {
		t.Errorf("ensure set lacks the `meta` label coloured bfd4f2; got %v", l.Ensure)
	}
	if named("append-only-override") < 0 {
		t.Error("ensure set lacks the `append-only-override` label")
	}
	// triage is retired in favour of the Triage board status.
	if !slices.Contains(l.Retire, "triage") {
		t.Errorf("retire set missing `triage`; got %v", l.Retire)
	}
}

// TestLoadRulesets loads every shipped ruleset. Coverage once shipped too, as a
// required check exempting the bot Apps so dependency PRs were not blocked on
// it. A ruleset bypass exempts the author of a PULL REQUEST, but the merge queue
// validates required checks against a merge GROUP, which has no author, so a
// coverage provider that accepted an upload without posting its status stalled
// the entire queue. Removing the template is the whole removal, because the
// ruleset SET is derived (see TestBuildPlanPrunesUnknownRulesets).
func TestLoadRulesets(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"master", "commit-messages", "require-approval", "tags", "release-lines"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, err := LoadRuleset(name)
			if err != nil {
				t.Fatalf("LoadRuleset(%s): %v", name, err)
			}
			if r.Name != name || len(r.Bypass) == 0 {
				t.Fatalf("name = %q, bypass = %v, want %q with a bypass list", r.Name, r.Bypass, name)
			}
		})
	}
	if _, err := LoadRuleset("codecov"); err == nil {
		t.Error("rulesets/codecov.yaml still ships — coverage must not gate a merge")
	}
}

func TestLoadRepoOverride(t *testing.T) {
	t.Parallel()

	p, ok, err := LoadRepoOverride("a-novel-kit", "stack")
	if err != nil || !ok {
		t.Fatalf("LoadRepoOverride(stack) = ok %v, err %v, want an override", ok, err)
	}
	if p.Class != ClassLibrary || p.CodeQuality || p.Features.PullRequests != "collaborators_only" {
		t.Errorf("stack override = class %q, code_quality %v, pull_requests %q, want library, false, collaborators_only",
			p.Class, p.CodeQuality, p.Features.PullRequests)
	}
	if _, ok, err := LoadRepoOverride("a-novel", "service-authentication"); err != nil || ok {
		t.Fatalf("expected no override for service-authentication; ok=%v err=%v", ok, err)
	}
}

// TestBuildRuleset pins template resolution. An unknown bypass entry is an
// error, not a silent drop. Admins expand to two actors, and the release bot
// bypasses tags in always mode, since it creates the tag directly, rather than
// require-approval's PR-only exempt mode. Only the flagged rules emit their
// API rule.
func TestBuildRuleset(t *testing.T) {
	t.Parallel()
	org := &OrgProfile{Org: "a-novel-kit", Bots: map[string]int64{"publish": 1734949, "agent": 3549379}}
	testCases := []struct {
		name    string
		spec    RulesetSpec
		wantErr bool
		modes   []string // each bypass actor's mode, in order
		rules   []string
	}{
		{name: "Error/UnknownBypass", spec: RulesetSpec{Name: rulesetMaster, Bypass: []string{"typo-bot"}}, wantErr: true},
		{
			name:  "Success/AdminsAndBot",
			spec:  RulesetSpec{Name: rulesetMaster, Bypass: []string{"admins", "publish"}},
			modes: []string{modeAlways, modeAlways, modeAlways},
		},
		{
			name:  "Success/TagsCreationRules",
			spec:  RulesetSpec{Name: rulesetTags, Bypass: []string{"agent"}, Rules: RulesetRules{Creation: true, Update: true}},
			modes: []string{modeAlways},
			rules: []string{"creation", "update"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			rs, err := BuildRuleset(&testCase.spec, org, nil)
			if (err != nil) != testCase.wantErr {
				t.Fatalf("BuildRuleset error = %v, want error %v", err, testCase.wantErr)
			}
			if err != nil {
				return
			}
			var modes, rules []string
			for _, a := range rs.BypassActors {
				modes = append(modes, a.BypassMode)
			}
			for _, r := range rs.Rules {
				rules = append(rules, r.Type)
			}
			if !slices.Equal(modes, testCase.modes) || !slices.Equal(rules, testCase.rules) {
				t.Errorf("bypass modes = %v, rules = %v, want %v, %v", modes, rules, testCase.modes, testCase.rules)
			}
		})
	}
}

// TestBuildRulesetCommitMessagePattern pins the commit-messages ruleset: it stays
// in Evaluate until the automation's messages comply, it covers every branch but
// the merge queue's, only Dependabot bypasses it, and its pattern accepts the
// subjects the fleet writes while rejecting the rest. GitHub evaluates metadata patterns as
// RE2, the dialect Go's regexp implements, so a pattern verified here behaves
// the same there.
func TestBuildRulesetCommitMessagePattern(t *testing.T) {
	t.Parallel()
	spec, err := LoadRuleset("commit-messages")
	if err != nil {
		t.Fatalf("LoadRuleset(commit-messages): %v", err)
	}
	org := &OrgProfile{Org: "a-novel", Bots: map[string]int64{"agent": 3549319, "publish": 1718144}}
	rs, err := BuildRuleset(spec, org, nil)
	if err != nil {
		t.Fatalf("BuildRuleset: %v", err)
	}
	if rs.Enforcement != "evaluate" {
		t.Fatalf("enforcement = %q, want evaluate", rs.Enforcement)
	}
	refs := rs.Conditions["ref_name"].(map[string]any)
	if !slices.Equal(refs["include"].([]string), []string{"~ALL"}) ||
		!slices.Equal(refs["exclude"].([]string), []string{"refs/heads/gh-readonly-queue/**"}) {
		t.Fatalf("ref_name = %v, want every branch but the merge queue's", refs)
	}
	if len(rs.BypassActors) != 1 || *rs.BypassActors[0].ActorID != dependabotAppID {
		t.Fatalf("bypass actors = %+v, want Dependabot alone", rs.BypassActors)
	}
	var params map[string]any
	for _, r := range rs.Rules {
		if r.Type == "commit_message_pattern" {
			params = r.Parameters
		}
	}
	if params == nil {
		t.Fatal("commit-messages ruleset emitted no commit_message_pattern rule")
	}
	if params["operator"] != "regex" || params["negate"] != false {
		t.Fatalf("operator/negate = %v/%v, want regex/false", params["operator"], params["negate"])
	}
	pattern := regexp.MustCompile(params["pattern"].(string))

	for _, subject := range []string{
		"feat(repocfg): restrict pull requests to collaborators (#530)",
		"chore(deps): update module golang.org/x/text to v0.41.0 [security] (#525)",
		"fix!: drop the legacy flag",
		"refactor(pkg-js)!: rename the client\n\nBody text.",
		"revert: feat(repocfg): restrict pull requests to collaborators",
	} {
		if !pattern.MatchString(subject) {
			t.Errorf("pattern rejects conventional subject %q", subject)
		}
	}
	for _, subject := range []string{
		"2.9.0",
		"Update README.md",
		"feat: ",
		"feat(repocfg) missing colon",
		"Feat: capitalised type",
		"fixup! feat(repocfg): restrict pull requests to collaborators",
		"Merge branch 'master' into feat/repocfg/x",
		`Revert "feat(repocfg): restrict pull requests to collaborators"`,
	} {
		if pattern.MatchString(subject) {
			t.Errorf("pattern accepts non-conventional subject %q", subject)
		}
	}
}
