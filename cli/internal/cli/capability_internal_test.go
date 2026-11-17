package cli

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// TestWithCoverage pins that withCoverage leaves a Go target's selectors alone
// and only adds -cover, so every test package runs, one under mocks/, test/ or
// protogen/ included, and the exclusion applies to the reported mean only.
func TestWithCoverage(t *testing.T) {
	t.Parallel()

	goTarget := func(args ...string) detect.Target { return detect.Target{Kind: detect.KindGo, Args: args} }
	testCases := []struct {
		name string
		in   detect.Target
		want []string
	}{
		{name: "Success/EnvLessWholeModule", in: goTarget("test", "./..."), want: []string{"test", "-cover", "./..."}},
		{name: "Success/EnvBackedKeepsCountAndScope", in: goTarget("test", "-count=1", "./internal/..."), want: []string{"test", "-cover", "-count=1", "./internal/..."}},
		// The catch-all carries several selectors; every one must survive.
		{name: "Success/KeepsEverySelector", in: goTarget("test", "./pkg/go", "./cmd/rest"), want: []string{"test", "-cover", "./pkg/go", "./cmd/rest"}},
		{
			name: "Success/PnpmForwardsToVitest",
			in:   detect.Target{Kind: detect.KindPnpm, Args: []string{"run", "test"}},
			want: []string{"run", "test", "--", "--coverage"},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := withCoverage([]detect.Target{testCase.in})
			require.Equal(t, testCase.want, got[0].Args)
		})
	}
}

// TestNewCapabilityCmd pins the test/build flag surface through the real root:
// pflag's attached short values, and exit status 2 for every bad invocation.
// Each case stops at the directory check, before any scan.
func TestNewCapabilityCmd(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{name: "Error/MissingDir/AttachedShortValues", args: []string{"test", "-j1", "-tgo", "-T30s", "-C/nonexistent"}, wantMsg: "cannot scan"},
		{name: "Error/MissingDir/KeepAndNoCover", args: []string{"test", "--keep", "--no-cover", "--dir", "/nonexistent"}, wantMsg: "cannot scan"},
		{name: "Error/NonPositiveJobs", args: []string{"build", "-j", "0"}, wantMsg: "--jobs"},
		{name: "Error/NegativeTimeout", args: []string{"build", "-T", "-1s"}, wantMsg: "--timeout"},
		{name: "Error/UnknownKind", args: []string{"build", "-t", "rust"}, wantMsg: "--type"},
		{name: "Error/UnknownKindInList", args: []string{"build", "--type=go,bogus"}, wantMsg: `"bogus" is not one of go, pnpm, podman`},
		{name: "Error/UnknownFlag", args: []string{"test", "--bogus"}, wantMsg: "unknown flag"},
		{name: "Error/BuildHasNoKeep", args: []string{"build", "--keep"}, wantMsg: "unknown flag"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := runCmd(t, NewRoot(), testCase.args...)
			var exitErr *ExitError
			require.ErrorAs(t, err, &exitErr)
			require.Equal(t, exitUsage, exitErr.Code)
			require.ErrorContains(t, err, testCase.wantMsg)
		})
	}
}
