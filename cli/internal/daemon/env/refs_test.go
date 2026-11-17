package env_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/daemon/env"
)

// TestServicePrefix pins the prefix a cross-service reference names a service
// by, which the env builder resolves on every Acquire.
func TestServicePrefix(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		in   string
		want string
	}{
		{name: "Success/JSONKeys", in: "service-json-keys", want: "SERVICE_JSON_KEYS"},
		{name: "Success/Authentication", in: "service-authentication", want: "SERVICE_AUTHENTICATION"},
		// A bare name without hyphens is still uppercased, covering a
		// service that skips the service-X convention.
		{name: "Success/NoHyphen", in: "plain", want: "PLAIN"},
		{name: "Success/Empty", in: "", want: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, testCase.want, env.ServicePrefix(testCase.in))
		})
	}
}
