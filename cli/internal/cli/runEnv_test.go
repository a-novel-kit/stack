package cli

import (
	"testing"

	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

// TestPrintableEnv pins which entries `run env` prints: a key `export` takes,
// under a stack and service that keep their comment header on one line.
func TestPrintableEnv(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		entry *anovelv1.EnvEntry
		want  bool
	}{
		{name: "Success", entry: &anovelv1.EnvEntry{Key: "REST_URL", Stack: "default", Service: "service-json-keys"}, want: true},
		{name: "Success/LeadingUnderscore", entry: &anovelv1.EnvEntry{Key: "_PORT_0"}, want: true},
		{name: "Error/LeadingDigit", entry: &anovelv1.EnvEntry{Key: "0PORT"}},
		{name: "Error/Command", entry: &anovelv1.EnvEntry{Key: "X=1; touch /tmp/pwned; Y"}},
		{name: "Error/Empty", entry: &anovelv1.EnvEntry{}},
		{name: "Error/LineBreakInService", entry: &anovelv1.EnvEntry{Key: "A", Service: "svc\ntouch /tmp/pwned"}},
		{name: "Error/CarriageReturnInStack", entry: &anovelv1.EnvEntry{Key: "A", Stack: "stack\r"}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := printableEnv(testCase.entry); got != testCase.want {
				t.Fatalf("printableEnv(%q) = %v, want %v", testCase.entry.GetKey(), got, testCase.want)
			}
		})
	}
}
