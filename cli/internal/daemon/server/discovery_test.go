package server_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/discovery"
	"github.com/a-novel-kit/stack/cli/internal/daemon/server"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestListStacks pins that ListStacks and Status advertise the stacks discovery
// kept, in registration order, so they match what every other RPC accepts. Each
// stack carries its discovery problems in the text the daemon log prints.
func TestListStacks(t *testing.T) {
	t.Parallel()

	problems := []discovery.DiscoveryError{
		{Service: "service-a", Reason: "compose service has no profile"},
		{Service: "service-b", Path: "/w/app/service-b/cmd/rest", Reason: "cmd/rest/ has no compose service"},
	}
	srv := server.New("test", "", discovery.Stacks{
		{Name: "default", Path: "/w", Default: true, Errors: problems},
		{Name: "alive", Path: "/tmp/here"},
	}, nil, nil, nil, nil, nil)

	resp, err := srv.ListStacks(t.Context(), &anovelv1.ListStacksRequest{})
	require.NoError(t, err)
	got := resp.GetStacks()
	require.Len(t, got, 2)

	require.Equal(t, "default", got[0].GetName())
	require.True(t, got[0].GetIsDefault())
	require.Equal(t, "/w", got[0].GetPath())
	require.Equal(t, []string{problems[0].Error(), problems[1].Error()}, got[0].GetDiscoveryErrors())

	require.Equal(t, "alive", got[1].GetName())
	require.False(t, got[1].GetIsDefault())
	require.Empty(t, got[1].GetDiscoveryErrors())
}
