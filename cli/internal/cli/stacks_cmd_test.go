package cli

import (
	"bytes"
	"os"
	"path/filepath"
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

	registered := []*anovelv1.Stack{
		stack(stacks.DefaultName, true),
		stack("agent-a", false),
		stack("agent-b", false),
	}

	cases := []struct {
		name     string
		args     []string
		all      bool
		want     []string
		wantErr  bool
		errMatch string
	}{
		{
			name: "all skips the default stack",
			all:  true,
			want: []string{"agent-a", "agent-b"},
		},
		{
			name: "named scratch stack resolves",
			args: []string{"agent-b"},
			want: []string{"agent-b"},
		},
		{
			name:     "named default stack is refused",
			args:     []string{stacks.DefaultName},
			wantErr:  true,
			errMatch: "is the default stack",
		},
		{
			name:     "unknown name is refused",
			args:     []string{"nope"},
			wantErr:  true,
			errMatch: "no registered stack",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := selectPruneTargets(registered, tc.args, tc.all)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				if !strings.Contains(err.Error(), tc.errMatch) {
					t.Fatalf("error %q does not mention %q", err, tc.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d stacks, want %d", len(got), len(tc.want))
			}
			for i, st := range got {
				if st.GetName() != tc.want[i] {
					t.Errorf("stack %d = %q, want %q", i, st.GetName(), tc.want[i])
				}
			}
		})
	}
}

// TestSelectPruneTargetsAllWithNoScratch pins the empty case: a machine with
// only the default stack yields nothing to prune rather than an error, so the
// sweep is safe to run unconditionally.
func TestSelectPruneTargetsAllWithNoScratch(t *testing.T) {
	t.Parallel()

	got, err := selectPruneTargets([]*anovelv1.Stack{stack(stacks.DefaultName, true)}, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d stacks, want none", len(got))
	}
}

func TestHoldingsOf(t *testing.T) {
	t.Parallel()

	target := func(id string, p anovelv1.Phase) *anovelv1.Target {
		return &anovelv1.Target{Id: id, Phase: p}
	}
	infra := func(p anovelv1.Phase) *anovelv1.Infra { return &anovelv1.Infra{Phase: p} }

	services := []*anovelv1.Service{
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
	}

	h := holdingsOf(services)

	if len(h.services) != 2 {
		t.Errorf("services = %d, want 2", len(h.services))
	}
	// RUNNING + STARTING + PENDING are live; TERMINATED is not.
	want := []string{"s/service-a/rest", "s/service-a/boot", "s/service-b/rest"}
	if len(h.liveTargets) != len(want) {
		t.Fatalf("liveTargets = %v, want %v", h.liveTargets, want)
	}
	for i, id := range want {
		if h.liveTargets[i] != id {
			t.Errorf("liveTargets[%d] = %q, want %q", i, h.liveTargets[i], id)
		}
	}
	if h.liveInfra != 1 {
		t.Errorf("liveInfra = %d, want 1", h.liveInfra)
	}
	if len(h.volumes) != 3 {
		t.Errorf("volumes = %d, want 3", len(h.volumes))
	}
}

// TestHoldingsOfUnknownPhase pins that an unset phase is not treated as live.
// A zero-valued Target is a decoding artefact, not a running process, and
// killing one would fail the whole prune on a target that never existed.
func TestHoldingsOfUnknownPhase(t *testing.T) {
	t.Parallel()

	h := holdingsOf([]*anovelv1.Service{{
		Name:    "service-a",
		Targets: []*anovelv1.Target{{Id: "s/service-a/ghost"}},
		Infra:   []*anovelv1.Infra{{}},
	}})

	if len(h.liveTargets) != 0 {
		t.Errorf("liveTargets = %v, want none", h.liveTargets)
	}
	if h.liveInfra != 0 {
		t.Errorf("liveInfra = %d, want 0", h.liveInfra)
	}
}

func TestUnpushedCommits(t *testing.T) {
	t.Parallel()

	local, _ := initSyncRepo(t)

	if n := unpushedCommits(local); n != 0 {
		t.Fatalf("fresh clone has %d unpushed commits, want 0", n)
	}

	writeFixture(t, local, "c.txt", "c0\n")
	mustGit(t, local, "add", "-A")
	mustGit(t, local, "commit", "--quiet", "-m", "local only")

	if n := unpushedCommits(local); n != 1 {
		t.Fatalf("after one local commit: %d unpushed, want 1", n)
	}

	mustGit(t, local, "push", "--quiet")

	if n := unpushedCommits(local); n != 0 {
		t.Fatalf("after push: %d unpushed, want 0", n)
	}
}

// TestUnpushedCommitsNoUpstream covers a checkout with no upstream at all. It
// counts as zero rather than erroring the prune — see the doc comment.
func TestUnpushedCommitsNoUpstream(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustGit(t, dir, "init", "--quiet", "--initial-branch=master")
	mustGit(t, dir, "config", "user.email", "test@a-novel.dev")
	mustGit(t, dir, "config", "user.name", "test")
	writeFixture(t, dir, "a.txt", "a0\n")
	mustGit(t, dir, "add", "-A")
	mustGit(t, dir, "commit", "--quiet", "-m", "init")

	if n := unpushedCommits(dir); n != 0 {
		t.Fatalf("no-upstream checkout reported %d unpushed, want 0", n)
	}
}

// TestDefaultStackRoot pins that an unrouted stack lands under the OS temp
// directory. os.TempDir() honours $TMPDIR, which is what makes this correct on
// macOS (a per-user /var/folders/…/T) rather than a Linux-only /tmp assumption.
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
// named rather than silently absent, and that a vanished root is distinguished
// from one the daemon simply has not picked up yet — the two need different
// fixes (drop the entry vs restart).
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

// TestDryRunVerdict pins that a dry run reports rather than refuses. An earlier
// revision errored out on a blocked stack, which answered "what would this do?"
// with neither the plan nor the reason.
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

	t.Run("clean pushed checkout blocks nothing", func(t *testing.T) {
		t.Parallel()
		local, _ := initSyncRepo(t)
		if got := pruneBlockers(local); len(got) != 0 {
			t.Fatalf("blockers = %v, want none", got)
		}
	})

	t.Run("uncommitted changes block the prune", func(t *testing.T) {
		t.Parallel()
		local, _ := initSyncRepo(t)
		writeFixture(t, local, "a.txt", "edited\n")
		got := pruneBlockers(local)
		if len(got) != 1 || !strings.Contains(got[0], "uncommitted changes") {
			t.Fatalf("blockers = %v, want an uncommitted-changes entry", got)
		}
	})

	t.Run("a feature branch blocks the prune", func(t *testing.T) {
		t.Parallel()
		local, _ := initSyncRepo(t)
		mustGit(t, local, "checkout", "--quiet", "-b", "feat/dao/thing")
		got := pruneBlockers(local)
		if len(got) != 1 || !strings.Contains(got[0], "feat/dao/thing") {
			t.Fatalf("blockers = %v, want an on-branch entry", got)
		}
	})

	t.Run("unpushed commits block the prune", func(t *testing.T) {
		t.Parallel()
		local, _ := initSyncRepo(t)
		writeFixture(t, local, "c.txt", "c0\n")
		mustGit(t, local, "add", "-A")
		mustGit(t, local, "commit", "--quiet", "-m", "local only")
		got := pruneBlockers(local)
		if len(got) != 1 || !strings.Contains(got[0], "unpushed") {
			t.Fatalf("blockers = %v, want an unpushed-commits entry", got)
		}
	})

	t.Run("a missing root blocks nothing", func(t *testing.T) {
		t.Parallel()
		if got := pruneBlockers(t.TempDir() + "/gone"); len(got) != 0 {
			t.Fatalf("blockers = %v, want none", got)
		}
	})
}
