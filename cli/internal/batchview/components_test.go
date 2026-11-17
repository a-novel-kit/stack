package batchview_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/a-novel-kit/stack/cli/internal/batchview"
)

func TestTruncate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		s     string
		width int

		want string
	}{
		{name: "Success/Fits", s: "abc", width: 5, want: "abc"},
		{name: "Success/CutASCII", s: "abcdef", width: 4, want: "abc…"},
		{name: "Success/CutKeepsMultiByteRunes", s: "a→b→c→d", width: 4, want: "a→b…"},
		{name: "Success/CutCountsWideCells", s: "日本語テキスト", width: 5, want: "日本…"},
		{name: "Success/CutKeepsANSISequences", s: "\x1b[31mabcdef\x1b[m", width: 4, want: "\x1b[31mabc…\x1b[m"},
		// No cell left, so nothing overflows the space it was given.
		{name: "Success/NonPositiveWidth", s: "abcdef", width: 0, want: ""},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, testCase.want, batchview.Truncate(testCase.s, testCase.width))
		})
	}
}
