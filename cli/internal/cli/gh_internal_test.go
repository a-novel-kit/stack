package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGh pins the retry around GitHub's secondary rate limit: a rejected
// call is retried, up to three attempts, and any other failure is not.
//
//nolint:paralleltest // swaps the package-level ghExec seam.
func TestGh(t *testing.T) {
	rateLimited := errors.New("gh: You have exceeded a secondary rate limit (HTTP 403)")
	notFound := errors.New("gh: Not Found (HTTP 404)")

	testCases := []struct {
		name      string
		failures  []error // returned by successive attempts, then success
		wantCalls int
		wantErr   bool
	}{
		{name: "Success", wantCalls: 1},
		{name: "Success/AfterRateLimit", failures: []error{rateLimited, rateLimited}, wantCalls: 3},
		{name: "Error/RateLimitPersists", failures: []error{rateLimited, rateLimited, rateLimited}, wantCalls: 3, wantErr: true},
		{name: "Error/NotRetried", failures: []error{notFound}, wantCalls: 1, wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			attempts := 0
			stubGH(t, func(string) (string, error) {
				attempts++
				if attempts <= len(testCase.failures) {
					return "", testCase.failures[attempts-1]
				}
				return "ok", nil
			})

			_, err := gh(t.Context(), "api", "repos/o/r")
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, testCase.wantCalls, attempts)
		})
	}
}

func TestGhError(t *testing.T) {
	t.Parallel()

	base := errors.New("exit status 1")
	testCases := []struct {
		name, stdout, stderr, want string
	}{
		{
			name:   "Success/JSONBodyJoinsTheMessage",
			stdout: "{\n  \"message\": \"Validation Failed\",\n  \"errors\": [\"Actor Dependabot integration must be part of the ruleset source or owner organization\"]\n}\n",
			stderr: "gh: Validation Failed (HTTP 422)\n",
			want:   `exit status 1: gh: Validation Failed (HTTP 422) {"message":"Validation Failed","errors":["Actor Dependabot integration must be part of the ruleset source or owner organization"]}`,
		},
		{name: "Success/NonJSONStdoutIsDropped", stdout: "partial output", stderr: "gh: not found", want: "exit status 1: gh: not found"},
		{name: "Success/NothingToAdd", want: "exit status 1"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := ghError(base, testCase.stdout, testCase.stderr)
			require.ErrorIs(t, err, base)
			require.EqualError(t, err, testCase.want)
		})
	}
}
