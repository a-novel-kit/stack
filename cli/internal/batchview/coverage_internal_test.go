package batchview

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/jobs"
)

// TestGoCoverage pins that goCoverage keeps generated and test-support packages
// out of the coverage report. Every package runs under coverage mode, so their
// zero-coverage lines appear in the output, and parsing drops them.
func TestGoCoverage(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name   string
		output string
		want   []covEntry
	}{
		{
			// Every excluded shape is dropped: a mocks tree at any depth, a
			// test-support tree, and generated protobuf.
			name: "Success/ExcludesGeneratedPackages",
			output: `ok  	x/internal/dao  0.10s  coverage: 80.0% of statements
ok  	x/internal/mocks  0.01s  coverage: 0.0% of statements
ok  	x/internal/handlers/mocks  0.01s  coverage: 0.0% of statements
ok  	x/internal/test  0.01s  coverage: 0.0% of statements
ok  	x/proto/gen/protogen  0.01s  coverage: 0.0% of statements
ok  	x/pkg/go  0.05s  coverage: 60.0% of statements`,
			want: []covEntry{{pkg: "x/internal/dao", pct: 80}, {pkg: "x/pkg/go", pct: 60}},
		},
		{
			// A package whose name merely contains "test" as a substring, not a
			// path segment, is not excluded.
			name: "Success/KeepsSubstringMatches",
			output: "ok  	x/internal/testutil  0.01s  coverage: 50.0% of statements\n" +
				"ok  	x/internal/attestation  0.01s  coverage: 70.0% of statements",
			want: []covEntry{{pkg: "x/internal/testutil", pct: 50}, {pkg: "x/internal/attestation", pct: 70}},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			results := []jobs.Result{{Name: "x", Group: "go", Output: testCase.output}}
			require.Equal(t, testCase.want, goCoverage(results))
		})
	}
}
