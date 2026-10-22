package server

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
	"github.com/a-novel-kit/stack/cli/internal/daemon/runner"
)

// TestSiblingCommand pins that a cancelled Exec ends the command's whole
// process tree. A shell's background job inherits the output pipe, so killing
// the shell alone leaves the pipe, and the Exec handler reading it, open until
// the job ends.
func TestSiblingCommand(t *testing.T) {
	t.Parallel()

	cmdDir := filepath.Join(t.TempDir(), "service-svc", "cmd", "rest")
	if err := os.MkdirAll(cmdDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tgt := &discovery.Target{Name: "rest", Service: "svc", Stack: "default", CmdDir: cmdDir}
	stacks := discovery.Stacks{{
		Name:     "default",
		Default:  true,
		Services: []*discovery.Service{{Name: "svc", Stack: "default", Targets: []*discovery.Target{tgt}}},
	}}
	alloc := env.NewAllocator()
	alloc.SetServices([]string{"svc"})
	builder := env.NewBuilder(alloc)
	s := &Server{runner: runner.New(stacks, alloc, builder, logs.New()), envBuilder: builder}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	cmd, err := s.siblingCommand(ctx, tgt, []string{"sh", "-c", "sleep 30 & echo started; wait"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	reader := bufio.NewReader(out)
	if line, err := reader.ReadString('\n'); err != nil || line != "started\n" {
		t.Fatalf("first line = %q, %v; want started", line, err)
	}

	cancel()

	closed := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, reader)
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("the output pipe is still open 5s after the cancel: a background job outlived the Exec")
	}
}
