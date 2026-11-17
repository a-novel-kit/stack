package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseEntityID pins the ID shapes kill, restart, logs and start accept. A
// shorthand stays as it is for the daemon to resolve against its default
// stack, unless --stack names one.
func TestParseEntityID(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		arg, stack string
		want       entityRef
	}{
		{name: "Success/TargetShorthand", arg: "svc/rest", want: entityRef{ID: "svc/rest"}},
		{name: "Success/TargetShorthandOnStack", arg: "svc/rest", stack: "fork", want: entityRef{ID: "fork/svc/rest"}},
		{name: "Success/TargetCanonical", arg: "fork/svc/rest", stack: "main", want: entityRef{ID: "fork/svc/rest"}},
		{name: "Success/InfraShorthand", arg: "svc/infra/pg", want: entityRef{ID: "svc/infra/pg", IsInfra: true, Service: "svc", Infra: "pg"}},
		{
			name: "Success/InfraShorthandOnStack", arg: "svc/infra/pg", stack: "fork",
			want: entityRef{ID: "fork/svc/infra/pg", IsInfra: true, Stack: "fork", Service: "svc", Infra: "pg"},
		},
		{
			name: "Success/InfraCanonical", arg: "fork/svc/infra/pg", stack: "main",
			want: entityRef{ID: "fork/svc/infra/pg", IsInfra: true, Stack: "fork", Service: "svc", Infra: "pg"},
		},
		// "infra" anywhere but second-to-last is an ordinary target name.
		{name: "Success/TargetNamedInfra", arg: "svc/infra", stack: "main", want: entityRef{ID: "main/svc/infra"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, parseEntityID(testCase.arg, testCase.stack))
		})
	}
}
