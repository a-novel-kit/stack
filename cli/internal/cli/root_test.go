package cli

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// commandCase runs the real root with args.
type commandCase struct {
	name    string
	args    []string
	wantOut string // printed on success
	wantErr string // contained in the error; empty for success
}

// TestRejectUnknownSubcommands drives the real root: every command group fails
// on an argument that names none of its subcommands, and still prints its help
// and completes when used as intended.
func TestRejectUnknownSubcommands(t *testing.T) {
	t.Parallel()

	testCases := []commandCase{
		{name: "Success/GroupHelp", args: []string{"core", "stacks"}, wantOut: "a-novel core stacks [command]"},
		{name: "Success/HelpFlag", args: []string{"run", "--help"}, wantOut: "a-novel run [command]"},
		{name: "Success/HelpCommand", args: []string{"help", "run", "service"}, wantOut: "a-novel run service [command]"},
		{name: "Success/CompletionScript", args: []string{"completion", "zsh"}, wantOut: "#compdef a-novel"},
		{name: "Success/CompleteSubcommand", args: []string{"__complete", "core", "stacks", "p"}, wantOut: "prune"},
		{name: "Error/RemovedCommand", args: []string{"run", "stacks"}, wantErr: `unknown command "stacks" for "a-novel run"`},
		{name: "Error/RemovedNestedCommand", args: []string{"run", "service", "status", "service-json-keys"}, wantErr: `unknown command "status" for "a-novel run service"`},
		{name: "Error/Suggestion", args: []string{"core", "stakcs"}, wantErr: "Did you mean this?\n\tstacks"},
	}
	testCases = slices.Concat(testCases, unknownSubcommandCases(NewRoot()))

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			out, err := runCmd(t, NewRoot(), testCase.args...)
			if testCase.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
					t.Fatalf("Execute(%v) error = %v, want %q", testCase.args, err, testCase.wantErr)
				}
				return
			}
			if err != nil || !strings.Contains(out, testCase.wantOut) {
				t.Fatalf("Execute(%v) = %v, output %q; want success printing %q", testCase.args, err, out, testCase.wantOut)
			}
		})
	}
}

// unknownSubcommandCases returns a case passing an unknown subcommand to cmd and
// to every command group below it.
func unknownSubcommandCases(cmd *cobra.Command) []commandCase {
	var cases []commandCase
	if cmd.HasSubCommands() {
		path := cmd.CommandPath()
		cases = append(cases, commandCase{
			name:    "Error/Unknown/" + strings.ReplaceAll(path, " ", "/"),
			args:    append(strings.Fields(path)[1:], "bogus"),
			wantErr: fmt.Sprintf("unknown command %q for %q", "bogus", path),
		})
	}
	for _, child := range cmd.Commands() {
		cases = append(cases, unknownSubcommandCases(child)...)
	}
	return cases
}
