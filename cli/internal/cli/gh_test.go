package cli

import (
	"errors"
	"testing"
)

// TestGhRun pins the retry around GitHub's secondary rate limit: a rejected
// call is retried, up to three attempts, and any other failure is not.
func TestGhRun(t *testing.T) {
	// Not parallel: swaps the package-level ghStdin seam.
	rateLimited := errors.New("gh: You have exceeded a secondary rate limit (HTTP 403)")
	testCases := []struct {
		name      string
		failures  []error // returned by successive attempts, then success
		wantCalls int
		wantErr   bool
	}{
		{name: "Success", wantCalls: 1},
		{name: "Success/AfterRateLimit", failures: []error{rateLimited, rateLimited}, wantCalls: 3},
		{name: "Error/RateLimitPersists", failures: []error{rateLimited, rateLimited, rateLimited}, wantCalls: 3, wantErr: true},
		{name: "Error/NotRetried", failures: []error{errGHNotFound}, wantCalls: 1, wantErr: true},
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
			_, err := ghRun("", "api", "repos/o/r")
			if (err != nil) != testCase.wantErr {
				t.Errorf("err = %v, wantErr %v", err, testCase.wantErr)
			}
			if attempts != testCase.wantCalls {
				t.Errorf("calls = %d, want %d", attempts, testCase.wantCalls)
			}
		})
	}
}

func TestGhError(t *testing.T) {
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
			err := ghError(base, testCase.stdout, testCase.stderr)
			if !errors.Is(err, base) {
				t.Fatalf("ghError dropped the wrapped error: %v", err)
			}
			if err.Error() != testCase.want {
				t.Fatalf("ghError = %q, want %q", err.Error(), testCase.want)
			}
		})
	}
}
