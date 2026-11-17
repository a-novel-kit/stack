package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
	"github.com/a-novel-kit/stack/cli/internal/daemon/logs"
)

// newRunnerForLaunch builds a runner over one discovered target whose ports
// block makes every env build claim REST_PORT.
func newRunnerForLaunch() (*Runner, *env.Allocator, *discovery.Target) {
	tgt := &discovery.Target{Name: "rest", Service: "svc", Stack: "default", Ports: []string{"${REST_PORT}:8080"}}
	stacks := discovery.Stacks{{
		Name:     "default",
		Default:  true,
		Services: []*discovery.Service{{Name: "svc", Stack: "default", Targets: []*discovery.Target{tgt}}},
	}}
	alloc := env.NewAllocator()
	alloc.SetServices([]string{"svc"})
	return New(stacks, alloc, env.NewBuilder(alloc), logs.New()), alloc, tgt
}

// stubOnPath puts first on PATH a stand-in for the tool name, a shell script
// that keeps the real tool out of the test. It sets PATH with t.Setenv, so the
// calling test cannot run in parallel.
func stubOnPath(t *testing.T, name, script string) {
	t.Helper()

	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, name), []byte(script), 0o700); err != nil { //nolint:gosec // an executable fixture
		panic(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// fakePodmanInspect puts on PATH a stand-in podman whose inspect prints, as
// one JSON array, the object held in the file named after each container ID
// under the returned directory. An ID with no file fails the call with exit
// 125 after printing the others, as podman does. Every call appends its
// arguments to the directory's calls file.
func fakePodmanInspect(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	stubOnPath(t, "podman", fmt.Sprintf(`#!/bin/sh
echo "$@" >> %[1]s/calls
shift 3
status=0
sep=""
printf '['
for cid in "$@"; do
	if [ -f %[1]s/"$cid" ]; then
		printf '%%s' "$sep"
		cat %[1]s/"$cid"
		sep=","
	else
		status=125
	fi
done
printf ']'
exit $status
`, dir))
	return dir
}

// inspectState is the `podman inspect` object of container cid in state,
// written "<status>|<health>|<exit code>" with "-" for no healthcheck.
func inspectState(cid, state string) string {
	parts := strings.Split(state, "|")
	health := ""
	if parts[1] != "-" {
		health = fmt.Sprintf(`, "Health": {"Status": %q}`, parts[1])
	}
	return fmt.Sprintf(`{"Id": %q, "State": {"Status": %q, "ExitCode": %s%s}}`, cid, parts[0], parts[2], health)
}

// podmanCalls counts the calls a fakePodmanInspect directory recorded.
func podmanCalls(t *testing.T, dir string) int {
	t.Helper()

	calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
	return strings.Count(string(calls), "\n")
}
