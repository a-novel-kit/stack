package cli

import (
	"fmt"
	"os/exec"
	"syscall"
)

// Exit codes beyond the generic failure, distinct so scripts can react.
const (
	exitUsage   = 2   // bad invocation
	exitAborted = 130 // interrupted before completing (128+SIGINT)
)

// ExitError carries an exit status through Cobra's error path. main prints Err,
// when set, before exiting with Code; a nil Err exits silently, as when the
// status is a child's own.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string {
	if e.Err == nil {
		return fmt.Sprintf("exit status %d", e.Code)
	}
	return e.Err.Error()
}

func (e *ExitError) Unwrap() error { return e.Err }

// exitStatus is the status a shell reports for a finished child: its exit code,
// or 128 plus the signal number when a signal ended it. POSIX.1-2024 XCU 2.8.2
// "Exit Status for Commands" asks for a value above 128, and the common shells
// report 128 plus the signal. ExitCode reports -1 then, which os.Exit turns
// into 255.
func exitStatus(err *exec.ExitError) int {
	if ws, ok := err.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return err.ExitCode()
}

// usageError marks a bad invocation, exit status 2.
func usageError(err error) error {
	return &ExitError{Code: exitUsage, Err: err}
}
