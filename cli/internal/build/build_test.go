package build_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/build"
	"github.com/a-novel-kit/stack/cli/internal/detect"
	"github.com/a-novel-kit/stack/cli/internal/secrets"
	"github.com/a-novel-kit/stack/cli/internal/shared/fsutil/fsutiltest"
)

// TestPrepareEnv verifies the test/build env-assembly seam: PrepareEnv appends
// the repo's locally-stored secrets (decrypted) to the child env. The value must
// appear only in the returned env, never in the progress written to `out`. A
// repo without a mapping injects nothing and does not fail, and a
// declared-but-unset secret is not injected, does not fail the build, and
// surfaces a value-free warning so the developer knows what to set.
func TestPrepareEnv(t *testing.T) {
	t.Parallel()

	const manifest = "secrets:\n  - env: OPENAI_API_KEY\n    id: openai-key\n    description: used by generation\n"
	testCases := []struct {
		name     string
		secret   string // stored as openai-key; empty: never set
		manifest string // empty: no .a-novel/secrets.yaml
		wantEnv  string // the OPENAI_API_KEY entry, empty when none
		outHas   []string
	}{
		{name: "Success/InjectsRepoSecrets", secret: "sk-prepare-env", manifest: manifest, wantEnv: "OPENAI_API_KEY=sk-prepare-env"},
		{name: "Success/NoMappingNoSecrets"},
		{
			name: "Success/MissingSecretWarns", manifest: manifest,
			outHas: []string{"OPENAI_API_KEY", "openai-key", "used by generation", "a-novel secrets set openai-key"},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			secretsRoot := t.TempDir()
			if testCase.secret != "" {
				st, err := secrets.Open(secretsRoot)
				require.NoError(t, err)
				st.Set("openai-key", testCase.secret)
				require.NoError(t, st.Save())
			}
			// With no compose Env, the target's repository root is its Dir.
			repoRoot := t.TempDir()
			if testCase.manifest != "" {
				fsutiltest.Write(t, repoRoot, map[string]string{".a-novel/secrets.yaml": testCase.manifest})
			}

			var out strings.Builder
			runEnv, err := build.PrepareEnv(t.Context(), detect.Target{Kind: detect.KindGo, Name: "x", Dir: repoRoot}, secretsRoot, &out)
			require.NoError(t, err)
			var got string
			for _, e := range runEnv {
				if strings.HasPrefix(e, "OPENAI_API_KEY=") {
					got = e
				}
			}
			require.Equal(t, testCase.wantEnv, got, "runEnv OPENAI_API_KEY entry")
			if testCase.secret != "" {
				require.NotContains(t, out.String(), testCase.secret, "PrepareEnv progress output leaked the secret value")
			}
			for _, want := range testCase.outHas {
				require.Contains(t, out.String(), want, "missing-secret warning")
			}
		})
	}
}

// TestClaimEnvs pins the claim that keeps two runs of one checkout off the same
// env. A claim takes each env once, however many targets share it, and fails
// while another run holds one of them. Release, and a failed claim, drop every
// lock taken, so the next run claims them all.
//
// Not parallel: each case points TMPDIR, and so the lock directory, at a
// directory of its own, leaving the machine's real locks alone.
func TestClaimEnvs(t *testing.T) {
	testCases := []struct {
		name    string
		envs    []string // the env of each target, empty for none
		held    bool     // another run holds the last env
		wantErr bool
	}{
		{name: "Success/NoEnv", envs: []string{""}},
		{name: "Success/SharedEnvClaimedOnce", envs: []string{"go", "go"}},
		{name: "Success/DistinctEnvs", envs: []string{"go.internal", "pnpm"}},
		{name: "Error/InUseByAnotherRun", envs: []string{"go.internal", "pnpm"}, held: true, wantErr: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("TMPDIR", t.TempDir())
			targets := make([]detect.Target, len(testCase.envs))
			for i, id := range testCase.envs {
				if id != "" {
					targets[i].Env = &detect.ComposeEnv{Project: "anovel-test-x-" + id, ID: id}
				}
			}

			releaseOther := func() {}
			if testCase.held {
				var err error
				releaseOther, err = build.ClaimEnvs(targets[len(targets)-1:])
				require.NoError(t, err, "other run's claim")
			}

			release, err := build.ClaimEnvs(targets)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				release()
			}
			releaseOther()

			again, err := build.ClaimEnvs(targets)
			require.NoError(t, err, "claim after release")
			again()
		})
	}
}

// TestJob verifies a job injects GOMAXPROCS from its maxProcs argument (the
// per-target CPU cap the parallel runner uses), and leaves it untouched when
// maxProcs is 0 (the sequential path).
//
//nolint:paralleltest // unsets GOMAXPROCS for the whole process
func TestJob(t *testing.T) {
	// The child echoes its inherited GOMAXPROCS; ensure it isn't already set in
	// this process, so we observe only what the job injects. t.Setenv restores
	// the original value on cleanup.
	t.Setenv("GOMAXPROCS", "")
	require.NoError(t, os.Unsetenv("GOMAXPROCS"))

	testCases := []struct {
		name     string
		maxProcs int
		want     string // expected value of $GOMAXPROCS in the child ("" = unset)
	}{
		{name: "Success/CapsWhenPositive", maxProcs: 3, want: "3"},
		{name: "Success/UntouchedWhenZero", maxProcs: 0, want: ""},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tgt := detect.Target{
				Kind: detect.KindGo,
				Name: "echo-gomaxprocs",
				Dir:  t.TempDir(),
				Cmd:  "sh",
				Args: []string{"-c", `printf 'GMP=[%s]' "$GOMAXPROCS"`},
			}
			var out strings.Builder
			_, err := build.Job(tgt, 0, testCase.maxProcs, false, t.TempDir()).Run(t.Context(), &out)
			require.NoError(t, err, out.String())
			require.Contains(t, out.String(), "GMP=["+testCase.want+"]")
		})
	}
}
