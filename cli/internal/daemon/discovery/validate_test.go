package discovery

import (
	"strings"
	"testing"
)

// TestValidateEnvRefs pins which env references the daemon fills: allocated
// ports with their derived hosts and URLs, the Postgres DSN, another service's
// variables and the service's own constants. Any other reference warns, naming
// the block that declares it.
func TestValidateEnvRefs(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		targetEnv map[string]string
		infraEnv  map[string]string
		// want is the block and the reference of each warning.
		want []string
	}{
		{
			name:      "Success/AllocatedAndDerived",
			targetEnv: map[string]string{"A": "${REST_PORT}", "B": "${REST_HOST}:${REST_URL}", "C": "${POSTGRES_DSN}"},
		},
		{name: "Success/CrossService", targetEnv: map[string]string{"DEP": "${SERVICE_Y_GRPC_URL}"}},
		{name: "Success/SiblingConstant", targetEnv: map[string]string{"DSN": "postgres://${POSTGRES_USER}@db"}, infraEnv: map[string]string{"POSTGRES_USER": "postgres"}},
		{name: "Error/TargetBlock", targetEnv: map[string]string{"KEY": "${OPENAI_API_KEY}"}, want: []string{"target rest", "${OPENAI_API_KEY}"}},
		{name: "Error/InfraBlock", infraEnv: map[string]string{"KEY": "${OPENAI_API_KEY}"}, want: []string{"infra postgres-x", "${OPENAI_API_KEY}"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			svc := &Service{
				Name:    "service-x",
				Targets: []*Target{{Name: "rest", Environment: testCase.targetEnv}},
				Infra:   []*Infra{{Name: "postgres-x", Environment: testCase.infraEnv}},
			}
			discovered := Stacks{{Name: "default", Services: []*Service{svc, {Name: "service-y"}}}}
			ValidateEnvRefs(discovered)

			errs := discovered[0].Errors
			if len(testCase.want) == 0 {
				if len(errs) != 0 {
					t.Fatalf("ValidateEnvRefs warned %v, want nothing", errs)
				}
				return
			}
			if len(errs) != 1 {
				t.Fatalf("ValidateEnvRefs warned %v, want one warning", errs)
			}
			for _, part := range testCase.want {
				if !strings.Contains(errs[0].Reason, part) {
					t.Errorf("warning %q does not name %q", errs[0].Reason, part)
				}
			}
		})
	}
}
