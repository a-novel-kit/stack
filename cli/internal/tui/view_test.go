package tui

import "testing"

func TestTruncate(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string

		s      string
		maxLen int

		want string
	}{
		{name: "Fits", s: "abc", maxLen: 5, want: "abc"},
		{name: "CutASCII", s: "abcdef", maxLen: 4, want: "abc…"},
		{name: "CutKeepsMultiByteRunes", s: "a—b—c—d", maxLen: 4, want: "a—b…"},
		{name: "CutCountsWideCells", s: "日本語テキスト", maxLen: 5, want: "日本…"},
		{name: "CutKeepsANSISequences", s: "\x1b[31mabcdef\x1b[m", maxLen: 4, want: "\x1b[31mabc…\x1b[m"},
		{name: "NonPositiveKeepsWhole", s: "abcdef", maxLen: 0, want: "abcdef"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := truncate(testCase.s, testCase.maxLen); got != testCase.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", testCase.s, testCase.maxLen, got, testCase.want)
			}
		})
	}
}
