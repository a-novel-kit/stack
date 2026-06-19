package repocfg

import "testing"

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
			if o.TeamID == 0 {
				t.Fatalf("team_id is zero for %s", org)
			}
			if len(o.BypassAlways) == 0 {
				t.Fatalf("bypass_always empty for %s", org)
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
	if _, ok := c.Languages["go"]; !ok {
		t.Fatal("languages.go missing")
	}
	if len(c.Always) == 0 {
		t.Fatal("always checks empty")
	}
}
