package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/volumes"
	"github.com/a-novel-kit/stack/cli/internal/shared/paths"
	"github.com/a-novel-kit/stack/cli/internal/shared/stacks"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func stack(name string, isDefault bool) *anovelv1.Stack {
	return &anovelv1.Stack{Name: name, Path: "/tmp/" + name, IsDefault: isDefault}
}

func TestSelectPruneTargets(t *testing.T) {
	t.Parallel()

	registered := []*anovelv1.Stack{stack(stacks.DefaultName, true), stack("agent-a", false), stack("agent-b", false)}

	for _, tc := range []struct {
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
		{name: "Success/AllWithNoScratch", registered: registered[:1], all: true},
		{name: "Success/Named", registered: registered, args: []string{"agent-b"}, want: []string{"agent-b"}},
		{name: "Error/NamedDefault", registered: registered, args: []string{stacks.DefaultName}, wantErr: "is the default stack"},
		{name: "Error/Unknown", registered: registered, args: []string{"nope"}, wantErr: "no registered stack"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := selectPruneTargets(tc.registered, tc.args, tc.all)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want it to mention %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || !slices.EqualFunc(got, tc.want, func(s *anovelv1.Stack, name string) bool { return s.GetName() == name }) {
				t.Fatalf("selectPruneTargets = (%v, %v), want %v", got, err, tc.want)
			}
		})
	}
}

func TestHoldingsOf(t *testing.T) {
	t.Parallel()

	target := func(id string, p anovelv1.Phase) *anovelv1.Target { return &anovelv1.Target{Id: id, Phase: p} }
	infra := func(p anovelv1.Phase) *anovelv1.Infra { return &anovelv1.Infra{Phase: p} }

	for _, tc := range []struct {
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
			// A zero-valued phase is a decoding artifact, so it counts as not
			// running and the prune leaves it alone.
			name: "Success/UnknownPhase",
			services: []*anovelv1.Service{{
				Name:    "service-a",
				Targets: []*anovelv1.Target{{Id: "s/service-a/ghost"}},
				Infra:   []*anovelv1.Infra{{}},
			}},
			want: stackHoldings{services: []string{"service-a"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := holdingsOf(tc.services); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("holdingsOf = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestDefaultStackRoot pins that an unrouted stack lands under the OS temp
// directory. os.TempDir() honors $TMPDIR, so this holds on macOS too, where it
// resolves to a per-user /var/folders/…/T.
func TestDefaultStackRoot(t *testing.T) {
	t.Parallel()

	got := defaultStackRoot("agent-7b46")

	if !strings.HasPrefix(got, os.TempDir()) {
		t.Errorf("defaultStackRoot = %q, want it under %q", got, os.TempDir())
	}
	if !strings.HasSuffix(got, "agent-7b46") {
		t.Errorf("defaultStackRoot = %q, want it to end in the stack name", got)
	}
	if got == defaultStackRoot("other") {
		t.Error("two stacks resolved to the same root")
	}
}

// TestStackBackupDir pins that --purge-backups targets the whole stack, not one
// service: volumes.BackupDir nests <stack>/<service>/<volume>, so the stack
// directory is the parent that takes all of them.
func TestStackBackupDir(t *testing.T) {
	t.Parallel()

	got := stackBackupDir("agent-7b46")

	if !strings.HasPrefix(got, paths.BackupsRoot()) {
		t.Errorf("stackBackupDir = %q, want it under %q", got, paths.BackupsRoot())
	}
	if filepath.Base(got) != "agent-7b46" {
		t.Errorf("stackBackupDir = %q, want it to end in the stack name", got)
	}
	// The per-volume path must nest inside it, or the purge misses backups.
	perVolume := volumes.BackupDir("agent-7b46", "service-json-keys", "pg-data")
	if !strings.HasPrefix(perVolume, got+string(filepath.Separator)) {
		t.Errorf("volumes.BackupDir = %q is not inside %q", perVolume, got)
	}
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
	if err := reportUnmanaged(&buf, map[string]bool{"default": true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := buf.String()

	if strings.Contains(got, "default") {
		t.Errorf("managed stack was reported as unmanaged: %q", got)
	}
	if !strings.Contains(got, "alive") || !strings.Contains(got, "restart the daemon") {
		t.Errorf("an intact but unmanaged stack should suggest a restart: %q", got)
	}
	if !strings.Contains(got, "swept") || !strings.Contains(got, "files are gone") {
		t.Errorf("a vanished stack should say so: %q", got)
	}
}

// TestDryRunVerdict pins that a dry run always reports. A blocked stack gets a
// verdict and the reason behind it, which is what the question asked for.
func TestDryRunVerdict(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		blockers []string
		force    bool
		want     string
	}{
		{name: "clean stack", want: "nothing changed"},
		{name: "blocked stack reports the refusal", blockers: []string{"golib: uncommitted changes"}, want: "would refuse"},
		{name: "force overrides the refusal", blockers: []string{"golib: uncommitted changes"}, force: true, want: "nothing changed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := dryRunVerdict(tc.blockers, tc.force); !strings.Contains(got, tc.want) {
				t.Errorf("dryRunVerdict = %q, want it to mention %q", got, tc.want)
			}
		})
	}
}

func TestPruneBlockers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		setup func(t *testing.T, local string) // nil: the root does not exist
		want  string                           // in the lone blocker; "" for none
	}{
		{name: "Success/CleanPushedCheckout", setup: func(*testing.T, string) {}},
		{name: "Success/MissingRoot"},
		{
			name:  "Success/UncommittedChanges",
			setup: func(t *testing.T, local string) { writeFixture(t, local, "a.txt", "edited\n") },
			want:  "uncommitted changes",
		},
		{
			name:  "Success/FeatureBranch",
			setup: func(t *testing.T, local string) { mustGit(t, local, "checkout", "--quiet", "-b", "feat/dao/thing") },
			want:  "feat/dao/thing",
		},
		{
			name:  "Success/UnpushedCommits",
			setup: func(t *testing.T, local string) { commitFixture(t, local, "c.txt", "c0\n") },
			want:  "unpushed",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := filepath.Join(t.TempDir(), "gone")
			if tc.setup != nil {
				root, _ = initSyncRepo(t)
				tc.setup(t, root)
			}
			got := pruneBlockers(root)
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("blockers = %v, want none", got)
				}
				return
			}
			if len(got) != 1 || !strings.Contains(got[0], tc.want) {
				t.Fatalf("blockers = %v, want one mentioning %q", got, tc.want)
			}
		})
	}
}
