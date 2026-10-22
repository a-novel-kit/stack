# Correctness traps

Read this reference when routed here by [write-go](../SKILL.md). Its rules apply to the selected
work. Each rule names a pull request that fixed one instance of the trap.

Contents:

- [Shared state](#shared-state)
- [Subprocesses](#subprocesses)
- [Servers and sockets](#servers-and-sockets)
- [Values built from parts](#values-built-from-parts)

## Shared state

- **A struct copy shares its maps and slices.** `snapshot := *live` copies each map's header, not
  its entries. A caller that reads the copy without the lock then races the writer. A concurrent map
  read and write is a fatal error that `recover` cannot catch. Clone every map and slice the copy
  exposes, with `maps.Clone` and `slices.Clone`, while the lock is still held
  (a-novel-kit/stack#786).
- **A check and its act in two critical sections.** Two callers both pass the check before either
  acts, so both start the same work. Check and record the claim under one lock, so a concurrent
  caller finds the claim. When the work itself is long, such as a `compose up`, hold a per-key lock
  for its whole duration instead. Make that lock a one-slot channel, so a waiter whose context ends
  can give up (a-novel-kit/stack#794).
- **Two sides that each finish one action.** A kill that arrives before a start has its process
  finds nothing to stop. Give each side one critical section that records its own state and reads
  the other's. The kill marks the target stopping, and the start, once it holds its handle, sees the
  mark and stops what it started. Whichever side comes second finishes the action. Move a state
  forward only from the state you expect, so a late "running" never overwrites "stopping" or
  "terminated" (a-novel-kit/stack#796).
- **A caller's cancellation recorded as the outcome.** A wait that ends with `context.Canceled` or
  `context.DeadlineExceeded` says nothing about the work it waited on, which may still succeed.
  Record a result only from the work itself (a-novel-kit/stack#826).

## Subprocesses

- **`exec.CommandContext` kills only the direct child.** The child's own children inherit its stdout
  and stderr. After a cancel they keep the pipes open, and the reader waits for them. Run the
  command in its own process group and kill the group:

  ```go
  cmd := exec.CommandContext(ctx, name, args...)
  cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
  cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
  cmd.WaitDelay = 5 * time.Second
  ```

  `WaitDelay` bounds the wait for a descendant that left the group with `setsid`
  (a-novel-kit/stack#813, a-novel-kit/stack#830).

- **`bufio.Scanner` stops at its buffer cap.** A line longer than the cap ends the scan with
  `bufio.ErrTooLong`, silently when the loop never checks `Err`. A child process still writing to
  that pipe then blocks, and so does `cmd.Wait`. Hand an overlong line out in buffer-sized pieces
  with a `SplitFunc`, or check `Err` and drain the reader (a-novel-kit/stack#811).

## Servers and sockets

- **`http.Server.Shutdown` never cancels request contexts.** It closes the listeners and waits for
  active requests, so an open stream holds it for its whole timeout. Give each streaming handler a
  context that also ends when the server stops, from a stop channel its owner receives. Leave
  `BaseContext` unset for this: its context ends every request, an in-flight unary call included
  (a-novel-kit/stack#791).
- **Closing a unix listener unlinks its socket.** A listener from `net.Listen("unix", path)` removes
  `path` on `Close`, and `http.Server` closes it on both of `Serve`'s exits. An `os.Remove(path)`
  after shutdown can delete the socket a successor already bound there (a-novel-kit/stack#786).

## Values built from parts

- **Resolution in map order.** `range` over a map yields a different order on every run. A pass that
  resolves values depending on one another, such as a URL derived from a port, then gives a random
  result. Resolve in an explicit order, the values the others read first (a-novel-kit/stack#803).
- **A URL or DSN joined from strings.** A password holding `@`, `/`, `:` or `#` breaks
  `"postgres://" + user + ":" + pass + "@" + host`. Build the value with `url.URL` and
  `url.UserPassword`, which escape each part (a-novel-kit/stack#831, a-novel-kit/stack#849).
