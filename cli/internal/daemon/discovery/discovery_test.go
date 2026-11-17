package discovery_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
)

// TestDiscoverStacks pins the asymmetry that keeps the daemon startable. A
// scratch stack lives in a directory the OS reclaims, so its files can
// disappear while its $A_NOVEL_STACKS entry lives on in a shell config;
// discovery skips it, and a path that exists as a file, so a temp sweep never
// costs the operator their workspace. The default stack is the workspace, so
// its absence is a real misconfiguration and must be loud.
func TestDiscoverStacks(t *testing.T) {
	t.Parallel()

	present := t.TempDir()
	gone := filepath.Join(t.TempDir(), "swept")
	fileDir := t.TempDir()
	fsutiltest.Write(t, fileDir, map[string]string{"not-a-dir": ""})
	file := filepath.Join(fileDir, "not-a-dir")

	testCases := []struct {
		name    string
		stacks  []stacks.Stack
		wantErr bool
	}{
		{name: "Success/SkipsVanishedScratch", stacks: []stacks.Stack{{Name: "swept", Path: gone}}},
		{name: "Success/SkipsNonDirectory", stacks: []stacks.Stack{{Name: "bogus", Path: file}}},
		{name: "Error/VanishedDefault", stacks: []stacks.Stack{{Name: "default", Path: gone, IsDefault: true}}, wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			in := testCase.stacks
			if !testCase.wantErr {
				in = append([]stacks.Stack{{Name: "default", Path: present, IsDefault: true}}, in...)
			}
			got, err := discovery.DiscoverStacks(in)
			if testCase.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, "default", got[0].Name)
		})
	}
}

// TestStacks pins the name resolution every RPC goes through, where an empty
// stack name means the default stack.
func TestStacks(t *testing.T) {
	t.Parallel()

	rest := &discovery.Target{Name: "rest", Service: "svc", Stack: "scratch"}
	x := discovery.Stacks{
		{Name: "default", Default: true, Services: []*discovery.Service{{Name: "svc", Stack: "default"}}},
		{Name: "scratch", Services: []*discovery.Service{{Name: "svc", Stack: "scratch", Targets: []*discovery.Target{rest}}}},
	}

	st := x.Stack("")
	require.NotNil(t, st)
	require.Equal(t, "default", st.Name)
	require.Nil(t, x.Stack("missing"))

	svc := x.Service("", "svc")
	require.NotNil(t, svc)
	require.Equal(t, "default", svc.Stack)
	require.Nil(t, x.Service("scratch", "nope"))

	tgt, svc := x.Target("scratch/svc/rest")
	require.Same(t, rest, tgt)
	require.NotNil(t, svc)
	require.Equal(t, "scratch", svc.Stack)
	tgt, _ = x.Target("default/svc/rest")
	require.Nil(t, tgt)

	require.Equal(t, []string{"svc", "svc"}, x.ServiceNames())
}

// TestDiscoveryError pins the one-line text the daemon logs and sends to
// clients for each problem. A stack-level problem names no service, and its
// text starts with the reason.
func TestDiscoveryError(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		err  discovery.DiscoveryError
		want string
	}{
		{
			name: "Success",
			err:  discovery.DiscoveryError{Service: "service-x", Path: "/w/app/service-x/cmd/rest", Reason: "orphan cmd"},
			want: "service-x: orphan cmd (/w/app/service-x/cmd/rest)",
		},
		{name: "Success/NoPath", err: discovery.DiscoveryError{Service: "service-x", Reason: "orphan cmd"}, want: "service-x: orphan cmd"},
		{
			name: "Success/NoService",
			err:  discovery.DiscoveryError{Path: "/w/app", Reason: "no app/ directory under stack root"},
			want: "no app/ directory under stack root (/w/app)",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, testCase.err.Error())
		})
	}
}
