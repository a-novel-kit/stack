package poll_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/shared/poll"
)

var errProbe = errors.New("probe failed")

func TestUntil(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		timeout time.Duration
		probe   func(calls int, ctx context.Context) (bool, error)
		expect  error
	}{
		{
			name:    "Success/DoneAtOnce",
			timeout: time.Minute,
			probe:   func(int, context.Context) (bool, error) { return true, nil },
		},
		{
			name:    "Success/DoneAfterRetries",
			timeout: time.Minute,
			probe:   func(calls int, _ context.Context) (bool, error) { return calls == 3, nil },
		},
		{
			name:    "Error/Probe",
			timeout: time.Minute,
			probe:   func(int, context.Context) (bool, error) { return false, errProbe },
			expect:  errProbe,
		},
		{
			name:    "Error/Timeout",
			timeout: 20 * time.Millisecond,
			probe:   func(int, context.Context) (bool, error) { return false, nil },
			expect:  poll.ErrTimeout,
		},
		{
			// A probe the deadline cuts short fails for that reason, and the
			// wait reports the timeout, not the probe's error.
			name:    "Error/TimeoutDuringProbe",
			timeout: 20 * time.Millisecond,
			probe: func(_ int, ctx context.Context) (bool, error) {
				<-ctx.Done()
				return false, ctx.Err()
			},
			expect: poll.ErrTimeout,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			calls := 0
			err := poll.Until(t.Context(), time.Millisecond, testCase.timeout, func(ctx context.Context) (bool, error) {
				calls++
				return testCase.probe(calls, ctx)
			})
			require.ErrorIs(t, err, testCase.expect)
		})
	}

	t.Run("Error/Cancelled", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		err := poll.Until(ctx, time.Millisecond, time.Minute, func(context.Context) (bool, error) { return false, nil })
		require.ErrorIs(t, err, context.Canceled)
	})
}
