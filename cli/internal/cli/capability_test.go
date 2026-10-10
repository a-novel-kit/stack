package cli

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// withCoverage leaves a Go target's selectors alone and only adds -cover, so every
// test package runs and the exclusion happens when the mean is computed. Filtering the run
// list is what dropped a package's tests the moment its path held mocks/test/protogen.
func TestWithCoverage(t *testing.T) {
	t.Parallel()

	goTarget := func(args ...string) detect.Target { return detect.Target{Kind: detect.KindGo, Args: args} }
	cases := []struct {
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

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got := withCoverage([]detect.Target{c.in})
			if !slices.Equal(got[0].Args, c.want) {
				t.Errorf("args = %v, want %v", got[0].Args, c.want)
			}
		})
	}
}

func TestParseKinds(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    []detect.Kind
		wantErr bool
	}{
		{in: "", want: nil},
		{in: "go", want: []detect.Kind{detect.KindGo}},
		{in: " Go , podman,bogus", want: []detect.Kind{detect.KindGo, detect.KindPodman}},
		{in: "bogus", wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()

			got, err := parseKinds(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("parseKinds(%q) error = %v, wantErr %v", c.in, err, c.wantErr)
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("parseKinds(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestCapabilityFlags pins the test/build flag surface through the real root:
// pflag's attached short values, and exit status 2 for every bad invocation.
// Each case stops at the directory check, before any scan.
func TestCapabilityFlags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{name: "Error/MissingDir/AttachedShortValues", args: []string{"test", "-j1", "-tgo", "-T30s", "-C/nonexistent"}, wantMsg: "cannot scan"},
		{name: "Error/MissingDir/KeepAndNoCover", args: []string{"test", "--keep", "--no-cover", "--dir", "/nonexistent"}, wantMsg: "cannot scan"},
		{name: "Error/NonPositiveJobs", args: []string{"build", "-j", "0"}, wantMsg: "--jobs"},
		{name: "Error/NegativeTimeout", args: []string{"build", "-T", "-1s"}, wantMsg: "--timeout"},
		{name: "Error/UnknownKind", args: []string{"build", "-t", "rust"}, wantMsg: "--type"},
		{name: "Error/UnknownFlag", args: []string{"test", "--bogus"}, wantMsg: "unknown flag"},
		{name: "Error/BuildHasNoKeep", args: []string{"build", "--keep"}, wantMsg: "unknown flag"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var exitErr *ExitError
			_, err := runCmd(t, NewRoot(), c.args...)
			if !errors.As(err, &exitErr) || exitErr.Code != exitUsage {
				t.Fatalf("Execute(%v) = %v, want exit status %d", c.args, err, exitUsage)
			}
			if !strings.Contains(err.Error(), c.wantMsg) {
				t.Errorf("error %q does not mention %q", err, c.wantMsg)
			}
		})
	}
}
