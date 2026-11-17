package stacks_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

//nolint:paralleltest // t.Setenv("HOME") pins the paths Parse expands, and forbids t.Parallel.
func TestParse(t *testing.T) {
	t.Setenv("HOME", "/home/me")

	testCases := []struct {
		name    string
		raw     string
		want    []stacks.Stack
		wantErr bool
	}{
		{
			name: "Success/ImplicitDefault",
			want: []stacks.Stack{{Name: stacks.DefaultName, Path: "/home/me/git-projects/a-novel", IsDefault: true}},
		},
		{
			name: "Success/TrimsAndExpandsHome",
			raw:  " main : ~/work , scratch:/tmp/x ,",
			want: []stacks.Stack{{Name: "main", Path: "/home/me/work", IsDefault: true}, {Name: "scratch", Path: "/tmp/x"}},
		},
		{name: "Error/NoPath", raw: "main", wantErr: true},
		{name: "Error/EmptyName", raw: ":/path", wantErr: true},
		{name: "Error/EmptyPath", raw: "main:", wantErr: true},
		{name: "Error/DuplicateName", raw: "a:/x,a:/y", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := stacks.Parse(testCase.raw)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.want, got)
		})
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
		{name: "Success/FirstEntryWins", stacks: "main:/srv/main,fork:/srv/fork", want: "/srv/main"},
		{name: "Success/UnsetFallsBackToImplicitDefault", stacks: "", want: "/home/tester/git-projects/a-novel"},
		{name: "Error/MalformedEntry", stacks: "no-path-here", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv(stacks.EnvVar, testCase.stacks)
			t.Setenv("HOME", "/home/tester")

			got, err := stacks.Default()
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.want, got.Path)
		})
	}
}
