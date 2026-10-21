package cli

import "testing"

// TestParseEntityID pins the ID shapes kill, restart, logs and start accept. A
// shorthand resolves against the first $A_NOVEL_STACKS entry, not a stack
// literally named "default".
func TestParseEntityID(t *testing.T) {
	// Not parallel: t.Setenv redirects the stack registry.
	t.Setenv("A_NOVEL_STACKS", "main:/srv/main,fork:/srv/fork")

	testCases := []struct {
		arg, stack string
		want       entityRef
	}{
		{arg: "svc/rest", want: entityRef{Stack: "main", Service: "svc", Target: "rest", ID: "main/svc/rest"}},
		{arg: "svc/rest", stack: "fork", want: entityRef{Stack: "fork", Service: "svc", Target: "rest", ID: "fork/svc/rest"}},
		{arg: "fork/svc/rest", want: entityRef{Stack: "fork", Service: "svc", Target: "rest", ID: "fork/svc/rest"}},
		{arg: "svc/infra/pg", want: entityRef{IsInfra: true, Stack: "main", Service: "svc", Infra: "pg", ID: "main/svc/infra/pg"}},
		{arg: "fork/svc/infra/pg", want: entityRef{IsInfra: true, Stack: "fork", Service: "svc", Infra: "pg", ID: "fork/svc/infra/pg"}},
		// "infra" anywhere but second-to-last is an ordinary target name.
		{arg: "svc/infra", want: entityRef{Stack: "main", Service: "svc", Target: "infra", ID: "main/svc/infra"}},
	}
	for _, testCase := range testCases {
		if got := parseEntityID(testCase.arg, testCase.stack); got != testCase.want {
			t.Errorf("parseEntityID(%q, %q) = %+v, want %+v", testCase.arg, testCase.stack, got, testCase.want)
		}
	}
}
