package stacks

import (
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	t.Setenv("HOME", "/home/me")

	cases := []struct {
		raw     string
		want    []Stack
		wantErr bool
	}{
		{"", []Stack{{Name: DefaultName, Path: "/home/me/git-projects/a-novel", IsDefault: true}}, false},
		{
			" main : ~/work , scratch:/tmp/x ,",
			[]Stack{{Name: "main", Path: "/home/me/work", IsDefault: true}, {Name: "scratch", Path: "/tmp/x"}},
			false,
		},
		{"main", nil, true},
		{":/path", nil, true},
		{"main:", nil, true},
		{"a:/x,a:/y", nil, true},
	}
	for _, c := range cases {
		got, err := Parse(c.raw)
		if (err != nil) != c.wantErr {
			t.Errorf("Parse(%q): got err %v, want error %v", c.raw, err, c.wantErr)
			continue
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("Parse(%q): got %+v, want %+v", c.raw, got, c.want)
		}
	}
}

// Not parallel: every case calls t.Setenv, which forbids it.
func TestDefault(t *testing.T) {
	cases := []struct {
		name    string
		stacks  string
		want    string
		wantErr bool
	}{
		{name: "FirstEntryWins", stacks: "main:/srv/main,fork:/srv/fork", want: "/srv/main"},
		{name: "UnsetFallsBackToImplicitDefault", stacks: "", want: "/home/tester/git-projects/a-novel"},
		{name: "MalformedEntry", stacks: "no-path-here", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv(EnvVar, c.stacks)
			t.Setenv("HOME", "/home/tester")
			got, err := Default()
			if (err != nil) != c.wantErr {
				t.Fatalf("Default() error = %v, wantErr %v", err, c.wantErr)
			}
			if got.Path != c.want {
				t.Errorf("Default().Path = %q, want %q", got.Path, c.want)
			}
		})
	}
}
