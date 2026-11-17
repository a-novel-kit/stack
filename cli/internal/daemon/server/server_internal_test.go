package server

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
)

// TestCanonicalID pins the shorthands the daemon resolves against its default
// stack, the first $A_NOVEL_STACKS entry, whatever the client's shell holds.
func TestCanonicalID(t *testing.T) {
	t.Parallel()

	registered := discovery.Stacks{{Name: "fork"}, {Name: "main", Default: true}}
	testCases := []struct {
		name   string
		stacks discovery.Stacks
		id     string
		want   string
	}{
		{name: "Success/TargetShorthand", stacks: registered, id: "svc/rest", want: "main/svc/rest"},
		{name: "Success/InfraShorthand", stacks: registered, id: "svc/infra/pg", want: "main/svc/infra/pg"},
		{name: "Success/TargetCanonical", stacks: registered, id: "fork/svc/rest", want: "fork/svc/rest"},
		{name: "Success/InfraCanonical", stacks: registered, id: "fork/svc/infra/pg", want: "fork/svc/infra/pg"},
		// "infra" anywhere but second-to-last is an ordinary target name.
		{name: "Success/TargetNamedInfra", stacks: registered, id: "svc/infra", want: "main/svc/infra"},
		{name: "Success/NoDefaultStack", stacks: discovery.Stacks{{Name: "fork"}}, id: "svc/rest", want: "svc/rest"},
		{name: "Success/Empty", stacks: registered, id: "", want: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			s := &Server{stacks: testCase.stacks}
			require.Equal(t, testCase.want, s.canonicalID(testCase.id))
		})
	}
}
