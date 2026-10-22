package logs

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// TestWriter pins how a view splits output into log lines. A line comes out
// whole up to maxLineBytes, across writes too. A longer line comes out in
// maxLineBytes pieces, so output that never ends a line still reaches the log.
func TestWriter(t *testing.T) {
	long := strings.Repeat("x", 2*maxLineBytes+maxLineBytes/2)
	piece, rest := strings.Repeat("x", maxLineBytes), strings.Repeat("x", maxLineBytes/2)

	testCases := []struct {
		name  string
		write []string
		want  []string
	}{
		{name: "Success/Lines", write: []string{"one\ntwo\n"}, want: []string{"one", "two"}},
		{name: "Success/LineAcrossWrites", write: []string{"o", "ne\nt"}, want: []string{"one"}},
		{name: "Success/LineAtCap", write: []string{piece + "\n"}, want: []string{piece}},
		{name: "Success/LongLine", write: []string{long + "\nafter\n"}, want: []string{piece, piece, rest, "after"}},
		{name: "Success/NoNewline", write: []string{long[:maxLineBytes/2], long[maxLineBytes/2:]}, want: []string{piece, piece}},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			s, _, w := withStore(t)
			stdout := w.Stdout()
			for _, chunk := range testCase.write {
				if _, err := stdout.Write([]byte(chunk)); err != nil {
					t.Fatal(err)
				}
			}

			got := readLines(t, s.CurrentPath("default", "test", "x"))
			// The lines run to MiBs, so a mismatch reports their lengths.
			if !slices.Equal(got, testCase.want) {
				t.Errorf("line lengths = %v, want %v", lengths(got), lengths(testCase.want))
			}
		})
	}
}

// readLines decodes the text of every record in a log file.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for record := range bytes.Lines(data) {
		var ln Line
		if err := json.Unmarshal(record, &ln); err != nil {
			t.Fatalf("decode %q: %v", record, err)
		}
		out = append(out, ln.Line)
	}
	return out
}

// lengths returns the length of each line.
func lengths(lines []string) []int {
	out := make([]int, len(lines))
	for i, line := range lines {
		out[i] = len(line)
	}
	return out
}
