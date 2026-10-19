package stacks

import (
	"slices"
	"testing"
)

func TestParse(t *testing.T) {
	t.Setenv("HOME", "/home/me")

	testCases := []struct {
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
	for _, testCase := range testCases {
		got, err := Parse(testCase.raw)
		if (err != nil) != testCase.wantErr {
			t.Errorf("Parse(%q): got err %v, want error %v", testCase.raw, err, testCase.wantErr)
			continue
		}
		if !slices.Equal(got, testCase.want) {
			t.Errorf("Parse(%q): got %+v, want %+v", testCase.raw, got, testCase.want)
		}
	}
}

// Not parallel: every case calls t.Setenv, which forbids it.
func TestDefault(t *testing.T) {
	testCases := []struct {
		name    string
		stacks  string
		want    string
		wantErr bool
	}{
		{name: "FirstEntryWins", stacks: "main:/srv/main,fork:/srv/fork", want: "/srv/main"},
		{name: "UnsetFallsBackToImplicitDefault", stacks: "", want: "/home/tester/git-projects/a-novel"},
		{name: "MalformedEntry", stacks: "no-path-here", wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(EnvVar, testCase.stacks)
			t.Setenv("HOME", "/home/tester")
			got, err := Default()
			if (err != nil) != testCase.wantErr {
				t.Fatalf("Default() error = %v, wantErr %v", err, testCase.wantErr)
			}
			if got.Path != testCase.want {
				t.Errorf("Default().Path = %q, want %q", got.Path, testCase.want)
			}
		})
	}
}
