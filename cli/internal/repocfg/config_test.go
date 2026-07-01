package repocfg

import (
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
		})
	}
}

func TestDetectClass(t *testing.T) {
	t.Parallel()
	cases := map[string]Class{
		"service-authentication": ClassService,
		"service-json-keys":      ClassService,
		"workflows":              ClassWorkflows,
		".github":                ClassMeta,
		"golib":                  ClassLibrary,
		"stack":                  ClassLibrary,
		"nodelib":                ClassLibrary,
		"platform-web":           ClassLibrary, // platform not modelled yet → default
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

func TestLoadOrgs(t *testing.T) {
	t.Parallel()
	for _, org := range []string{"a-novel", "a-novel-kit"} {
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
		})
	}
}

func TestLoadChecks(t *testing.T) {
	t.Parallel()
	c, err := LoadChecks()
	if err != nil {
		t.Fatalf("LoadChecks: %v", err)
	}
	if c.Integrations["actions"] == 0 {
		t.Fatal("integrations.actions missing")
	}
	if len(c.Always) == 0 {
		t.Fatal("always checks empty")
	}
	// merge-gate (a-novel-kit/.github#50) is required via the [Agent] App, so the
	// integration must resolve to that app id — not Actions.
	if c.Integrations["agent"] != 3549379 {
		t.Errorf("integrations.agent = %d, want 3549379 ([Agent] App)", c.Integrations["agent"])
	}
	var mergeGate *CheckDef
	for i := range c.Always {
		if c.Always[i].Context == "merge-gate" {
			mergeGate = &c.Always[i]
		}
	}
	if mergeGate == nil {
		t.Fatal("always set missing merge-gate")
	}
	if mergeGate.Integration != "agent" {
		t.Errorf("merge-gate integration = %q, want %q", mergeGate.Integration, "agent")
	}
	// main.yaml jobs are required by default; the exclusions drop reporting and
	// master-only jobs.
	if !slices.Contains(c.Exclude.Prefixes, "report-") {
		t.Errorf("exclude.prefixes missing report-; got %v", c.Exclude.Prefixes)
	}
	if len(c.Exclude.IfContains) == 0 {
		t.Error("exclude.if_contains empty (expected the master-only guard)")
	}
	// CodeQL keeps a minimal file-based language detection for codeql.yml.
	if !slices.Contains(c.CodeQL.Always, "actions") {
		t.Errorf("codeql.always missing actions; got %v", c.CodeQL.Always)
	}
	if len(c.CodeQL.Languages) == 0 {
		t.Error("codeql.languages empty")
	}
	// Codecov posts its own statuses (separate, bot-bypass ruleset).
	if len(c.Codecov.Checks) == 0 {
		t.Error("codecov.checks empty")
	}
}

func TestLoadLabels(t *testing.T) {
	t.Parallel()
	l, err := LoadLabels()
	if err != nil {
		t.Fatalf("LoadLabels: %v", err)
	}
	// meta is the new no-PR / meta-epic label.
	var meta *LabelDef
	for i := range l.Ensure {
		if l.Ensure[i].Name == "meta" {
			meta = &l.Ensure[i]
		}
	}
	if meta == nil {
		t.Fatal("ensure set missing the `meta` label")
	}
	if meta.Color != "bfd4f2" {
		t.Errorf("meta colour = %q, want %q", meta.Color, "bfd4f2")
	}
	// triage is retired in favour of the Triage board status.
	if !slices.Contains(l.Retire, "triage") {
		t.Errorf("retire set missing `triage`; got %v", l.Retire)
	}
}

func TestLoadRulesets(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"master", "require-approval", "codecov", "tags"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r, err := LoadRuleset(name)
			if err != nil {
				t.Fatalf("LoadRuleset(%s): %v", name, err)
			}
			if r.Name != name {
				t.Fatalf("name = %q, want %q", r.Name, name)
			}
			if len(r.Bypass) == 0 {
				t.Fatalf("%s: bypass empty", name)
			}
		})
	}
}

func TestLoadRepoOverride(t *testing.T) {
	t.Parallel()

	p, ok, err := LoadRepoOverride("a-novel-kit", "stack")
	if err != nil {
		t.Fatalf("LoadRepoOverride(stack): %v", err)
	}
	if !ok {
		t.Fatal("expected a stack override to exist")
	}
	if p.Class != ClassLibrary {
		t.Fatalf("stack base class = %q, want %q", p.Class, ClassLibrary)
	}

	if _, ok, err := LoadRepoOverride("a-novel", "service-authentication"); err != nil || ok {
		t.Fatalf("expected no override for service-authentication; ok=%v err=%v", ok, err)
	}
}

func TestBuildRulesetBypassResolution(t *testing.T) {
	t.Parallel()
	org := &OrgProfile{Org: "a-novel-kit", Bots: map[string]int64{"publish": 1734949}}

	t.Run("unknown entry is an error, not a silent drop", func(t *testing.T) {
		t.Parallel()
		spec := &RulesetSpec{Name: rulesetMaster, Target: "branch", Enforcement: "active", Bypass: []string{"typo-bot"}}
		if _, err := BuildRuleset(spec, org, nil); err == nil {
			t.Fatal("expected an error for an unresolvable bypass entry")
		}
	})

	t.Run("known entries resolve", func(t *testing.T) {
		t.Parallel()
		spec := &RulesetSpec{Name: rulesetMaster, Target: "branch", Enforcement: "active", Bypass: []string{"admins", "publish"}}
		rs, err := BuildRuleset(spec, org, nil)
		if err != nil {
			t.Fatalf("BuildRuleset: %v", err)
		}
		// admins -> OrganizationAdmin + RepositoryRole; publish -> one Integration.
		if len(rs.BypassActors) != 3 {
			t.Fatalf("expected 3 bypass actors, got %d", len(rs.BypassActors))
		}
	})

	t.Run("tags ruleset bot bypass is always-mode", func(t *testing.T) {
		t.Parallel()
		// The release bot creates the tag directly, so it must bypass with mode
		// "always" — not the PR-only "exempt" used on the require-approval ruleset.
		o := &OrgProfile{Org: "a-novel-kit", Bots: map[string]int64{"agent": 3549379}}
		spec := &RulesetSpec{Name: rulesetTags, Target: "tag", Enforcement: "active", Bypass: []string{"agent"}}
		rs, err := BuildRuleset(spec, o, nil)
		if err != nil {
			t.Fatalf("BuildRuleset: %v", err)
		}
		if len(rs.BypassActors) != 1 {
			t.Fatalf("expected 1 bypass actor, got %d", len(rs.BypassActors))
		}
		if rs.BypassActors[0].BypassMode != modeAlways {
			t.Fatalf("tags bot bypass mode = %q, want %q", rs.BypassActors[0].BypassMode, modeAlways)
		}
	})

	t.Run("codecov ruleset exempts the agent (the release bump push)", func(t *testing.T) {
		t.Parallel()
		// The dispatched release pushes the version-bump commit straight to the
		// default branch as the agent bot; codecov must exempt it or that push is
		// rejected on required_status_checks. "exempt" is enough — the same mode on
		// require-approval already lets the agent's direct push through.
		spec, err := LoadRuleset("codecov")
		if err != nil {
			t.Fatalf("LoadRuleset(codecov): %v", err)
		}
		o := &OrgProfile{Org: "a-novel-kit", Bots: map[string]int64{
			"dependencies": 1734926, "publish": 1734949, "agent": 3549379,
		}}
		rs, err := BuildRuleset(spec, o, nil)
		if err != nil {
			t.Fatalf("BuildRuleset(codecov): %v", err)
		}
		var agent *APIBypassActor
		for i := range rs.BypassActors {
			if rs.BypassActors[i].ActorID != nil && *rs.BypassActors[i].ActorID == 3549379 {
				agent = &rs.BypassActors[i]
			}
		}
		if agent == nil {
			t.Fatal("codecov ruleset does not bypass the agent bot — the release bump push would be rejected")
		}
		if agent.BypassMode != modeExempt {
			t.Fatalf("agent bypass mode = %q, want %q", agent.BypassMode, modeExempt)
		}
	})
}

// TestBuildRulesetCreationRule covers the creation/update rules added for the
// release-tag lockdown — only those flags emit their API rule.
func TestBuildRulesetCreationRule(t *testing.T) {
	t.Parallel()
	org := &OrgProfile{Org: "a-novel-kit", Bots: map[string]int64{"agent": 3549379}}
	spec := &RulesetSpec{
		Name: rulesetTags, Target: "tag", Enforcement: "active",
		Bypass: []string{"agent"},
		Rules:  RulesetRules{Creation: true, Update: true},
	}
	rs, err := BuildRuleset(spec, org, nil)
	if err != nil {
		t.Fatalf("BuildRuleset: %v", err)
	}
	got := map[string]bool{}
	for _, r := range rs.Rules {
		got[r.Type] = true
	}
	if !got["creation"] || !got["update"] {
		t.Fatalf("expected creation+update rules, got rule types %v", got)
	}
	if got["deletion"] {
		t.Fatalf("deletion rule emitted without the flag set")
	}
}
