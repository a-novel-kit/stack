package enumword_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/a-novel-kit/stack/cli/internal/shared/enumword"
	anovelv1 "github.com/a-novel-kit/stack/cli/proto/gen/anovel/v1"
)

func TestWord(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		value protoreflect.Enum
		want  string
	}{
		{name: "Success/Derived", value: anovelv1.Phase_PHASE_RUNNING, want: "running"},
		{name: "Success/DerivedWithUnderscore", value: anovelv1.Mode_MODE_GO_EXEC, want: "go-exec"},
		{name: "Success/MultiWordPrefix", value: anovelv1.ExitReason_EXIT_REASON_CRASHED, want: "crashed"},
		{name: "Success/Unspecified", value: anovelv1.Mode_MODE_UNSPECIFIED, want: "-"},
		{name: "Success/OverriddenUnspecified", value: anovelv1.Phase_PHASE_UNSPECIFIED, want: "idle"},
		{name: "Success/Overridden", value: anovelv1.TargetKind_TARGET_KIND_LONG_RUNNER, want: "longr"},
		{name: "Success/UnknownValue", value: anovelv1.Health(99), want: "?"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, enumword.Word(testCase.value))
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		word string
		want anovelv1.Mode
	}{
		{name: "Success", word: "container", want: anovelv1.Mode_MODE_CONTAINER},
		{name: "Success/Empty", word: "", want: anovelv1.Mode_MODE_UNSPECIFIED},
		{name: "Error/UnknownWord", word: "contaner", want: anovelv1.Mode_MODE_UNSPECIFIED},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, enumword.Parse[anovelv1.Mode](testCase.word))
		})
	}
}
