package ui

import (
	"testing"

	"github.com/a-novel-kit/stack/cli/internal/build"
	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// goCoverage keeps generated and test-support packages out of the mean. Under coverage
// mode every package now runs, so their zero-coverage lines appear in the output; dropping
// them at parse time is what stops them from dragging the reported figure down — and it is
// where the exclusion belongs, since the run no longer skips them.
func TestGoCoverageExcludesGeneratedPackages(t *testing.T) {
	t.Parallel()

	out := `ok  	x/internal/dao  0.10s  coverage: 80.0% of statements
ok  	x/internal/mocks  0.01s  coverage: 0.0% of statements
ok  	x/internal/handlers/mocks  0.01s  coverage: 0.0% of statements
ok  	x/internal/test  0.01s  coverage: 0.0% of statements
ok  	x/proto/gen/protogen  0.01s  coverage: 0.0% of statements
ok  	x/pkg/go  0.05s  coverage: 60.0% of statements`

	results := []build.Result{{
		Target: detect.Target{Kind: detect.KindGo, Name: "x"},
		Output: out,
	}}

	entries := goCoverage(results)

	got := map[string]float64{}
	for _, e := range entries {
		got[e.pkg] = e.pct
	}

	// The real packages are kept.
	if got["x/internal/dao"] != 80.0 || got["x/pkg/go"] != 60.0 {
		t.Errorf("real packages missing or wrong: %v", got)
	}

	// Every excluded shape is dropped — a mocks tree at any depth, a test-support tree,
	// and generated protobuf.
	for _, excluded := range []string{
		"x/internal/mocks", "x/internal/handlers/mocks", "x/internal/test", "x/proto/gen/protogen",
	} {
		if _, present := got[excluded]; present {
			t.Errorf("%s must be excluded from the coverage mean", excluded)
		}
	}

	if len(entries) != 2 {
		t.Errorf("got %d entries, want 2 (dao, pkg/go)", len(entries))
	}
}

// A package whose name merely contains "test" as a substring — not a path segment — is
// not excluded.
func TestGoCoverageKeepsSubstringMatches(t *testing.T) {
	t.Parallel()

	out := "ok  	x/internal/testutil  0.01s  coverage: 50.0% of statements\n" +
		"ok  	x/internal/attestation  0.01s  coverage: 70.0% of statements"

	results := []build.Result{{
		Target: detect.Target{Kind: detect.KindGo, Name: "x"},
		Output: out,
	}}

	if n := len(goCoverage(results)); n != 2 {
		t.Errorf("got %d entries, want 2 — 'testutil' and 'attestation' are not /test/ segments", n)
	}
}
