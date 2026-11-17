package rpc_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"connectrpc.com/connect/v2"
	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/rpc"
)

type message struct{ n int }

// fakeStream replays msgs, then ends with err.
type fakeStream struct {
	msgs []*message
	err  error
}

func (s *fakeStream) Receive() (*message, error) {
	if len(s.msgs) == 0 {
		return nil, s.err
	}
	msg := s.msgs[0]
	s.msgs = s.msgs[1:]
	return msg, nil
}

func (s *fakeStream) Close() error { return nil }

func TestStream(t *testing.T) {
	t.Parallel()

	unavailable := connect.NewError(connect.CodeUnavailable, "dial unix a-novel.sock: connect: no such file or directory")
	errOther := errors.New("permission denied")

	testCases := []struct {
		name       string
		openErr    error
		stream     *fakeStream
		expect     []int
		notRunning bool
		expectErr  error
	}{
		{name: "Success", stream: &fakeStream{msgs: []*message{{1}, {2}}, err: io.EOF}, expect: []int{1, 2}},
		{name: "Error/OpenUnavailable", openErr: unavailable, notRunning: true},
		{
			// connect reports a dial failure on the first Receive, not when
			// the stream opens.
			name: "Error/ReceiveUnavailable", stream: &fakeStream{err: unavailable}, notRunning: true,
		},
		{name: "Error/ReceiveOther", stream: &fakeStream{msgs: []*message{{1}}, err: errOther}, expect: []int{1}, expectErr: errOther},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			open := func(context.Context, *struct{}) (*fakeStream, error) { return testCase.stream, testCase.openErr }

			var got []int
			var gotErr error
			for msg, err := range rpc.Stream(t.Context(), open, &struct{}{}) {
				if err != nil {
					gotErr = err
					break
				}
				got = append(got, msg.n)
			}

			require.Equal(t, testCase.expect, got)
			require.Equal(t, testCase.notRunning, rpc.IsNotRunning(gotErr))
			if testCase.expectErr != nil {
				require.ErrorIs(t, gotErr, testCase.expectErr)
			}
		})
	}
}
