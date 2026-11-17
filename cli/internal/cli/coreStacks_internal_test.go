package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/volumes"
	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
	"github.com/a-novel-kit/stack/cli/internal/workspace/workspacetest"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// stack builds a registered stack fixture rooted at /tmp/<name>.
func stack(name string, isDefault bool) *anovelv1.Stack {
	return &anovelv1.Stack{Name: name, Path: "/tmp/" + name, IsDefault: isDefault}
}

// TestSelectPruneTargets pins the default-stack protections of prune: --all
// skips the default stack, and naming it is refused.
func TestSelectPruneTargets(t *testing.T) {
	t.Parallel()

	registered := []*anovelv1.Stack{stack(stacks.DefaultName, true), stack("agent-a", false), stack("agent-b", false)}

	testCases := []struct {
		name       string
		registered []*anovelv1.Stack
		args       []string
		all        bool
		want       []string
		wantErr    string
	}{
		{name: "Success/AllSkipsTheDefault", registered: registered, all: true, want: []string{"agent-a", "agent-b"}},
		// Only the default stack yields nothing to prune, so the sweep is safe
		// to run unconditionally.
		{name: "Success/AllWithNoScratch", registered: registered[:1], all: true, want: []string{}},
		{name: "Success/Named", registered: registered, args: []string{"agent-b"}, want: []string{"agent-b"}},
		{name: "Error/NamedDefault", registered: registered, args: []string{stacks.DefaultName}, wantErr: "is the default stack"},
		{name: "Error/Unknown", registered: registered, args: []string{"nope"}, wantErr: "no registered stack"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := selectPruneTargets(testCase.registered, testCase.args, testCase.all)
			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				return
			}
			require.NoError(t, err)
			names := make([]string, 0, len(got))
			for _, st := range got {
				names = append(names, st.GetName())
			}
			require.Equal(t, testCase.want, names)
		})
	}
}

func TestHoldingsOf(t *testing.T) {
	t.Parallel()

	target := func(id string, p anovelv1.Phase) *anovelv1.Target { return &anovelv1.Target{Id: id, Phase: p} }
	infra := func(p anovelv1.Phase) *anovelv1.Infra { return &anovelv1.Infra{Phase: p} }

	testCases := []struct {
		name     string
		services []*anovelv1.Service
		want     stackHoldings
	}{
		{
			// RUNNING, STARTING and PENDING are live; TERMINATED is not.
			name: "Success",
			services: []*anovelv1.Service{
				{
					Name: "service-a",
					Targets: []*anovelv1.Target{
						target("s/service-a/rest", anovelv1.Phase_PHASE_RUNNING),
						target("s/service-a/grpc", anovelv1.Phase_PHASE_TERMINATED),
						target("s/service-a/boot", anovelv1.Phase_PHASE_STARTING),
					},
					Infra:   []*anovelv1.Infra{infra(anovelv1.Phase_PHASE_RUNNING), infra(anovelv1.Phase_PHASE_TERMINATED)},
					Volumes: []*anovelv1.Volume{{Name: "pg-a"}},
				},
				{
					Name:    "service-b",
					Targets: []*anovelv1.Target{target("s/service-b/rest", anovelv1.Phase_PHASE_PENDING)},
					Volumes: []*anovelv1.Volume{{Name: "pg-b"}, {Name: "cache-b"}},
				},
			},
			want: stackHoldings{
				services:    []string{"service-a", "service-b"},
				liveTargets: []string{"s/service-a/rest", "s/service-a/boot", "s/service-b/rest"},
				liveInfra:   1,
				volumes:     []string{"pg-a", "pg-b", "cache-b"},
			},
		},
		{
			// An UNSPECIFIED phase means the daemon never started the entity,
			// so it holds nothing and the prune leaves it alone.
			name: "Success/UnknownPhase",
			services: []*anovelv1.Service{{
				Name:    "service-a",
				Targets: []*anovelv1.Target{{Id: "s/service-a/ghost"}},
				Infra:   []*anovelv1.Infra{{}},
			}},
			want: stackHoldings{services: []string{"service-a"}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, holdingsOf(testCase.services))
		})
	}
}

// TestDefaultStackRoot pins that an unrouted stack lands under the OS temp
// directory. os.TempDir() honors $TMPDIR, so this holds on macOS too, where it
// resolves to a per-user /var/folders/…/T.
func TestDefaultStackRoot(t *testing.T) {
	t.Parallel()

	got := defaultStackRoot("agent-7b46")

	require.True(t, strings.HasPrefix(got, os.TempDir()), "%q is not under %q", got, os.TempDir())
	require.True(t, strings.HasSuffix(got, "agent-7b46"), "%q does not end in the stack name", got)
	require.NotEqual(t, defaultStackRoot("other"), got, "two stacks resolved to the same root")
}

// TestStackBackupDir pins that --purge-backups targets the whole stack, not one
// service: volumes.BackupDir nests <stack>/<service>/<volume>, so the stack
// directory is the parent that takes all of them.
func TestStackBackupDir(t *testing.T) {
	t.Parallel()

	got := stackBackupDir("agent-7b46")

	require.True(t, strings.HasPrefix(got, paths.BackupsRoot()), "%q is not under %q", got, paths.BackupsRoot())
	require.Equal(t, "agent-7b46", filepath.Base(got))
	// The per-volume path must nest inside it, or the purge misses backups.
	perVolume := volumes.BackupDir("agent-7b46", "service-json-keys", "pg-data")
	require.True(t, strings.HasPrefix(perVolume, got+string(filepath.Separator)), "volumes.BackupDir = %q is not inside %q", perVolume, got)
}

// TestReportUnmanaged pins that a registration the daemon is not serving is
// named in the output, and that a vanished root reads differently from one the
// daemon simply has not picked up yet. The two need different fixes: drop the
// entry, or restart.
func TestReportUnmanaged(t *testing.T) {
	present := t.TempDir()
	gone := filepath.Join(t.TempDir(), "swept")
	t.Setenv(stacks.EnvVar, "default:"+present+",alive:"+present+",swept:"+gone)

	var buf bytes.Buffer
	reportUnmanaged(&buf, map[string]bool{"default": true})
	got := buf.String()

	require.NotContains(t, got, "default", "a managed stack was reported as unmanaged")
	// An intact but unmanaged stack suggests a restart.
	require.Contains(t, got, "alive")
	require.Contains(t, got, "restart the daemon")
	// A vanished stack says so.
	require.Contains(t, got, "swept")
	require.Contains(t, got, "files are gone")
}

// TestPrintDiscoveryErrors pins the lines `core status` and `core stacks list`
// print under a stack's row: one per problem, and none for a healthy stack, so
// its listing reads as before.
func TestPrintDiscoveryErrors(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		problems []string
		want     string
	}{
		{name: "Success/NoProblems"},
		{
			name:     "Success",
			problems: []string{"service-a: orphan cmd (/w/app/service-a/cmd/rest)", "no app/ directory under stack root (/w/app)"},
			want:     "  ! service-a: orphan cmd (/w/app/service-a/cmd/rest)\n  ! no app/ directory under stack root (/w/app)\n",
		},
		{
			name:     "Success/MultiLineProblem",
			problems: []string{"service-a: parse compose: yaml: unmarshal errors:\n  line 3: cannot unmarshal"},
			want:     "  ! service-a: parse compose: yaml: unmarshal errors: line 3: cannot unmarshal\n",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			st := stack(stacks.DefaultName, true)
			st.DiscoveryErrors = testCase.problems

			var buf bytes.Buffer
			printDiscoveryErrors(&buf, "  ", st)
			require.Equal(t, testCase.want, buf.String())
		})
	}
}

// TestDryRunVerdict pins that a dry run always prints a verdict, and that a
// blocked stack's verdict names the refusal.
func TestDryRunVerdict(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		blockers []string
		force    bool
		want     string
	}{
		{name: "Success/CleanStack", want: "nothing changed"},
		{name: "Success/BlockedStackReportsTheRefusal", blockers: []string{"golib: uncommitted changes"}, want: "would refuse"},
		{name: "Success/ForceOverridesTheRefusal", blockers: []string{"golib: uncommitted changes"}, force: true, want: "nothing changed"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Contains(t, dryRunVerdict(testCase.blockers, testCase.force), testCase.want)
		})
	}
}

func TestPruneBlockers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		setup func(t *testing.T, local string) // nil: the root does not exist
		want  string                           // in the lone blocker; "" for none
	}{
		{name: "Success/CleanPushedCheckout", setup: func(*testing.T, string) {}},
		{name: "Success/MissingRoot"},
		{
			name: "Success/UncommittedChanges",
			setup: func(t *testing.T, local string) {
				t.Helper()
				fsutiltest.Write(t, local, map[string]string{"a.txt": "edited\n"})
			},
			want: "uncommitted changes",
		},
		{
			name: "Success/FeatureBranch",
			setup: func(t *testing.T, local string) {
				t.Helper()
				workspacetest.Git(t, local, "checkout", "--quiet", "-b", "feat/dao/thing")
			},
			want: "feat/dao/thing",
		},
		{
			name: "Success/UnpushedCommits",
			setup: func(t *testing.T, local string) {
				t.Helper()
				workspacetest.Commit(t, local, "c.txt", "c0\n")
			},
			want: "unpushed",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := filepath.Join(t.TempDir(), "gone")
			if testCase.setup != nil {
				root, _ = workspacetest.InitSync(t)
				testCase.setup(t, root)
			}

			got := pruneBlockers(root)
			if testCase.want == "" {
				require.Empty(t, got)
				return
			}
			require.Len(t, got, 1)
			require.Contains(t, got[0], testCase.want)
		})
	}
}
