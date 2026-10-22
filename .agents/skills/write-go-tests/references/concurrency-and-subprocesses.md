# Concurrency, subprocess and ordering tests

Read this reference when routed here by [write-go-tests](../SKILL.md). Its rules apply to the
selected work.

- **Fail on the old code.** Run a new regression test once against the production file as `master`
  has it, and see it fail. A test that passes either way pins nothing.
- **Race the whole operation.** A race between two critical sections of a helper spans a few
  instructions, and goroutines racing that helper alone rarely interleave. Release several
  goroutines at once through a closed channel, each running the full public operation, then assert
  that exactly one of them did the work (a-novel-kit/stack#794).
- **Fail without `-race` too.** Write the unsynchronized access so the race detector flags it, and
  also assert the observable outcome, such as a copy that must not see a later write. The test then
  fails in a run without `-race` (a-novel-kit/stack#786).
- **Stand-in binaries.** Drive a subprocess path with a stand-in script for a tool such as `podman`
  or `go`. Write it to a `t.TempDir()` and put that directory first on `PATH` with `t.Setenv`. A
  stand-in that waits for a file the test creates holds the code under test inside a window, such as
  a kill during `compose up`, with no hook in production code (a-novel-kit/stack#796). `t.Setenv`
  rules out `t.Parallel` for that test.
- **Results that depend on map order.** Run the operation many times in one case, 50 for example,
  since one passing run proves nothing (a-novel-kit/stack#803).
- **A wait with a bound.** A test that waits for a goroutine, a stream or a pipe to end selects on
  `time.After` with a bound of a few seconds, and fails with what is still open. A regression then
  fails the test instead of hanging the suite (a-novel-kit/stack#791).
