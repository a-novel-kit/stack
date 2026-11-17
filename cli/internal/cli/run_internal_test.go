package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseEntityID pins the ID shapes kill, restart, logs and start accept. A
// shorthand resolves against the first $A_NOVEL_STACKS entry, not a stack
// literally named "default".
//
//nolint:paralleltest // t.Setenv redirects the process-wide stack registry.
func TestParseEntityID(t *testing.T) {
	t.Setenv("A_NOVEL_STACKS", "main:/srv/main,fork:/srv/fork")

	testCases := []struct {
		name       string
		arg, stack string
		want       entityRef
	}{
		{name: "Success/TargetShorthand", arg: "svc/rest", want: entityRef{Stack: "main", Service: "svc", Target: "rest", ID: "main/svc/rest"}},
		{name: "Success/TargetShorthandOnStack", arg: "svc/rest", stack: "fork", want: entityRef{Stack: "fork", Service: "svc", Target: "rest", ID: "fork/svc/rest"}},
		{name: "Success/TargetCanonical", arg: "fork/svc/rest", want: entityRef{Stack: "fork", Service: "svc", Target: "rest", ID: "fork/svc/rest"}},
		{name: "Success/InfraShorthand", arg: "svc/infra/pg", want: entityRef{IsInfra: true, Stack: "main", Service: "svc", Infra: "pg", ID: "main/svc/infra/pg"}},
		{name: "Success/InfraCanonical", arg: "fork/svc/infra/pg", want: entityRef{IsInfra: true, Stack: "fork", Service: "svc", Infra: "pg", ID: "fork/svc/infra/pg"}},
		// "infra" anywhere but second-to-last is an ordinary target name.
		{name: "Success/TargetNamedInfra", arg: "svc/infra", want: entityRef{Stack: "main", Service: "svc", Target: "infra", ID: "main/svc/infra"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, parseEntityID(testCase.arg, testCase.stack))
		})
	}
}
