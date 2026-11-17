package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestEnumFlag(t *testing.T) {
	t.Parallel()

	allowed := []string{"go", "pnpm", "podman"}
	testCases := []struct {
		name    string
		list    bool
		value   string
		wantErr string
	}{
		{name: "Success", value: "pnpm"},
		{name: "Success/List", list: true, value: "go,podman"},
		{name: "Error/Unknown", value: "rust", wantErr: `"rust" is not one of go, pnpm, podman`},
		// A single flag takes one word, commas included.
		{name: "Error/ListOnSingle", value: "go,pnpm", wantErr: `"go,pnpm" is not one of`},
		{name: "Error/UnknownInList", list: true, value: "go,bogus", wantErr: `"bogus" is not one of`},
		{name: "Error/EmptyInList", list: true, value: "go,", wantErr: `"" is not one of`},
		{name: "Error/Case", value: "Go", wantErr: `"Go" is not one of`},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			value := "default"
			err := (&enumFlag{value: &value, allowed: allowed, list: testCase.list}).Set(testCase.value)
			if testCase.wantErr != "" {
				require.ErrorContains(t, err, testCase.wantErr)
				require.Equal(t, "default", value, "a rejected word keeps the value")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.value, value)
		})
	}
}

// TestAddEnumFlag pins the shell completion of an enum flag through cobra's
// own completion command: the allowed words, after the words a list already
// holds, and never a file name.
func TestAddEnumFlag(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		list  bool
		typed string
		want  string
	}{
		{name: "Success", typed: "", want: "go\npnpm\npodman\n:4\n"},
		{name: "Success/ListAfterComma", list: true, typed: "go,p", want: "go,go\ngo,pnpm\ngo,podman\n:4\n"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var value string
			root := &cobra.Command{Use: "root"}
			child := &cobra.Command{Use: "child", Run: func(*cobra.Command, []string) {}}
			addEnumFlag(child, &enumFlag{value: &value, allowed: []string{"go", "pnpm", "podman"}, list: testCase.list}, "type", "t", "")
			root.AddCommand(child)

			out, err := runCmd(t, root, cobra.ShellCompNoDescRequestCmd, "child", "--type", testCase.typed)
			require.NoError(t, err)
			// Cobra notes the directive after the completions, on stderr.
			completions, _, _ := strings.Cut(out, "Completion ended")
			require.Equal(t, testCase.want, completions)
		})
	}
}

func TestCountFlag(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "Success", value: "4", want: 4},
		{name: "Error/Zero", value: "0", wantErr: true},
		{name: "Error/Negative", value: "-2", wantErr: true},
		{name: "Error/NotANumber", value: "all", wantErr: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			count := 7
			err := countFlag{&count}.Set(testCase.value)
			if testCase.wantErr {
				require.ErrorContains(t, err, "want a positive integer")
				require.Equal(t, 7, count, "a rejected count keeps the default")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.want, count)
		})
	}
}

// TestAddJSONAlias pins the deprecated --json of `run ps` and `run watch`:
// --json selects json and --json=false text, and an explicit --format wins on
// either side of it.
func TestAddJSONAlias(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		args []string
		want string
	}{
		{name: "Success/Unset", want: formatText},
		{name: "Success/JSON", args: []string{"--json"}, want: formatJSON},
		{name: "Success/JSONTrue", args: []string{"--json=true"}, want: formatJSON},
		{name: "Success/JSONFalse", args: []string{"--json=false"}, want: formatText},
		{name: "Success/FormatBefore", args: []string{"--format=text", "--json"}, want: formatText},
		{name: "Success/FormatAfter", args: []string{"--json", "--format=text"}, want: formatText},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			format := formatText
			cmd := &cobra.Command{Use: "ps", Run: func(*cobra.Command, []string) {}}
			addEnumFlag(cmd, &enumFlag{value: &format, allowed: []string{formatText, formatJSON}}, "format", "", "")
			addJSONAlias(cmd, &format)

			out, err := runCmd(t, cmd, testCase.args...)
			require.NoError(t, err)
			require.Equal(t, testCase.want, format)
			// Every case that passes flags passes --json.
			if len(testCase.args) > 0 {
				require.Contains(t, out, "--json has been deprecated")
			}
		})
	}
}
