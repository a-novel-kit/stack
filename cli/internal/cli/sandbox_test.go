package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/cli"
)

func TestSandboxArgs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		args    []string
		want    []string
		enabled bool
		wantErr bool
	}{
		{name: "Success/LeadingFlag", args: []string{"--sandbox", "alpha"}, want: []string{"alpha"}, enabled: true},
		{name: "Success/MissingFlag", args: []string{"beta"}, want: []string{"beta"}},
		{name: "Error/LaterFlag", args: []string{"gamma", "--sandbox"}, wantErr: true},
		{name: "Error/DuplicateFlag", args: []string{"--sandbox", "--sandbox", "gamma"}, wantErr: true},
		{name: "Success/ChildFlagAfterSeparator", args: []string{"gamma", "--", "--sandbox"}, want: []string{"gamma", "--", "--sandbox"}},
		{name: "Success/EmptyInvocation"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, enabled, err := cli.SandboxArgs(testCase.args)
			if testCase.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.enabled, enabled)
			require.Equal(t, testCase.want, got)
		})
	}
}
