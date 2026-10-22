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

// ghFailure is a failed gh run as runGH reports it: gh's stderr, with
// GitHub's JSON body when stdout holds one.
type ghFailure struct {
	name, stderr, stdout string
	want                 bool
}

// rateLimitFailure is GitHub's secondary rate limit, whose message quotes a
// request ID holding digits such as 404 or 409.
func rateLimitFailure(name, requestID string) ghFailure {
	message := "You have exceeded a secondary rate limit. Please wait a few minutes before you try again. " +
		"If you reach out to GitHub Support for help, please include the request ID " + requestID + "."
	return ghFailure{
		name:   name,
		stderr: "gh: " + message + " (HTTP 403)\n",
		stdout: `{"message":"` + message + `","documentation_url":"https://docs.github.com/rest/overview/rate-limits-for-the-rest-api","status":"403"}`,
	}
}

func TestIsNotFound(t *testing.T) {
	t.Parallel()

	testCases := []ghFailure{
		{
			name:   "Success/MessageAndStatus",
			stderr: "gh: Not Found (HTTP 404)\n",
			stdout: `{"message":"Not Found","documentation_url":"https://docs.github.com/rest/repos/contents#get-repository-content","status":"404"}`,
			want:   true,
		},
		{name: "Success/StatusAlone", stderr: "gh: HTTP 404\n", stdout: "<html>Not Found</html>", want: true},
		{name: "Error/OtherStatus", stderr: "gh: Bad credentials (HTTP 401)\n"},
		rateLimitFailure("Error/RequestIDHolds404", "8F3E:2C1B:4049A1:5D1E2F:67170C2B"),
		{name: "Error/MessageQuotes404", stderr: "gh: No commit found for SHA: 404e7c1 (HTTP 422)\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := ghError(errors.New("exit status 1"), testCase.stdout, testCase.stderr)
			if got := isNotFound(err); got != testCase.want {
				t.Fatalf("isNotFound(%q) = %v, want %v", err, got, testCase.want)
			}
		})
	}
}

func TestIsAlreadyExists(t *testing.T) {
	t.Parallel()

	testCases := []ghFailure{
		{name: "Success/Conflict", stderr: "gh: GitHub Pages is already enabled. (HTTP 409)\n", want: true},
		{name: "Success/AlreadyExistsWording", stderr: "gh: Reference already exists (HTTP 422)\n", want: true},
		{name: "Error/OtherStatus", stderr: "gh: Not Found (HTTP 404)\n"},
		rateLimitFailure("Error/RequestIDHolds409", "4C09:3A7E:409B12:6E2F01:67170C2B"),
		{name: "Error/MessageQuotes409", stderr: "gh: No commit found for SHA: 409b2e1 (HTTP 422)\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := ghError(errors.New("exit status 1"), testCase.stdout, testCase.stderr)
			if got := isAlreadyExists(err); got != testCase.want {
				t.Fatalf("isAlreadyExists(%q) = %v, want %v", err, got, testCase.want)
			}
		})
	}
}
