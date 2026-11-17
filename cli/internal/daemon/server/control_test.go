package server_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/daemon/reinstall"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
	"github.com/a-novel-kit/stack/cli/internal/daemon/server"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestPrepareReinstall covers the reinstall handoff. A go-exec process outlives
// its daemon, so a target left running keeps the ports the next daemon
// relaunches it on. The checkpoint records those ports and nothing else, so no
// env value, secrets included, reaches the file. It sets XDG_STATE_HOME and
// PATH, so it cannot run in parallel.
func TestPrepareReinstall(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	// The runner spawns `go run`. A stand-in go that only sleeps keeps the
	// toolchain out of the test.
	bin := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(bin, "go"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o700)) //nolint:gosec // an executable fixture
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	tgt := &discovery.Target{
		Name: "rest", Service: "svc", Stack: "default", CmdDir: filepath.Join(t.TempDir(), "cmd", "rest"),
		Ports: []string{"${REST_PORT}:8080"},
	}
	stacks := discovery.Stacks{{
		Name:     "default",
		Default:  true,
		Services: []*discovery.Service{{Name: "svc", Stack: "default", Targets: []*discovery.Target{tgt}}},
	}}
	alloc := env.NewAllocator()
	alloc.SetServices([]string{"svc"})
	builder := env.NewBuilder(alloc)
	logStore := logs.New()
	run := runner.New(stacks, alloc, builder, logStore)
	stopped := make(chan struct{})
	srv := server.New("test", "", stacks, run, builder, logStore, func() { close(stopped) }, stopped)

	require.NoError(t, run.Relaunch(t.Context(), tgt.ID(), []string{"REST_PORT=41235"}))
	t.Cleanup(func() { _ = run.Kill(context.Background(), tgt.ID(), 0) })

	resp, err := srv.PrepareReinstall(t.Context(), &anovelv1.PrepareReinstallRequest{})
	require.NoError(t, err)
	require.Equal(t, int32(1), resp.GetGoExecTargetCount())

	cp, err := reinstall.Read()
	require.NoError(t, err)
	require.NotNil(t, cp)
	require.Equal(t, []reinstall.GoExecCheckpoint{{TargetID: tgt.ID(), Env: []string{"REST_PORT=41235"}}}, cp.GoExec)

	inst, _ := run.Instance(tgt.ID())
	require.Equal(t, anovelv1.Phase_PHASE_TERMINATED, inst.Phase)

	select {
	case <-stopped:
	case <-time.After(time.Second):
		require.Fail(t, "PrepareReinstall never stopped the daemon")
	}
}
