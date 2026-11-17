package cli

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// enumFlag is a string flag that takes one of its allowed words, or, as a
// list, a comma-separated list of them. Any other word fails the parse with a
// usage error that lists the allowed ones.
type enumFlag struct {
	value   *string
	allowed []string
	list    bool
}

// addEnumFlag defines f on cmd as the flag name, with shorthand when not
// empty, and completes its allowed words in the shell. The flag defaults to
// what f.value holds, which need not be an allowed word.
func addEnumFlag(cmd *cobra.Command, f *enumFlag, name, shorthand, usage string) {
	cmd.Flags().VarP(f, name, shorthand, usage)
	// The flag exists and has no completion yet, the only two failures.
	_ = cmd.RegisterFlagCompletionFunc(name, f.complete)
}

func (f *enumFlag) Set(v string) error {
	words := []string{v}
	if f.list {
		words = strings.Split(v, ",")
	}
	for _, word := range words {
		if !slices.Contains(f.allowed, word) {
			return fmt.Errorf("%q is not one of %s", word, strings.Join(f.allowed, ", "))
		}
	}
	*f.value = v
	return nil
}

func (f *enumFlag) String() string { return *f.value }

func (f *enumFlag) Type() string { return "string" }

// complete offers the allowed words. In a list, each follows the words
// already typed.
func (f *enumFlag) complete(_ *cobra.Command, _ []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
	var typed string
	if i := strings.LastIndexByte(toComplete, ','); f.list && i >= 0 {
		typed = toComplete[:i+1]
	}
	completions := make([]cobra.Completion, len(f.allowed))
	for i, word := range f.allowed {
		completions[i] = typed + word
	}
	return completions, cobra.ShellCompDirectiveNoFileComp
}

// countFlag is an int flag that takes only a positive count, as --jobs does.
// An unset flag keeps its default, which may be 0.
type countFlag struct{ value *int }

func (f countFlag) Set(v string) error {
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return fmt.Errorf("want a positive integer, got %q", v)
	}
	*f.value = n
	return nil
}

func (f countFlag) String() string { return strconv.Itoa(*f.value) }

func (f countFlag) Type() string { return "int" }

// addJSONAlias defines on cmd the deprecated --json flag of a command with a
// --format flag: --json selects json and --json=false text. An explicit
// --format wins, wherever it sits on the command line.
func addJSONAlias(cmd *cobra.Command, format *string) {
	flag := cmd.Flags().VarPF(jsonAlias{flags: cmd.Flags(), format: format}, "json", "", "print JSON lines")
	flag.NoOptDefVal = "true"
	// The flag was just defined, the only failure.
	_ = cmd.Flags().MarkDeprecated("json", "use --format=json or --format=text")
}

// jsonAlias is the value of the deprecated --json flag. It sets the format
// unless --format was parsed first: a --format parsed later overwrites it.
type jsonAlias struct {
	flags  *pflag.FlagSet
	format *string
}

func (a jsonAlias) Set(v string) error {
	on, err := strconv.ParseBool(v)
	if err != nil {
		return err
	}
	if a.flags.Changed("format") {
		return nil
	}
	*a.format = formatText
	if on {
		*a.format = formatJSON
	}
	return nil
}

func (a jsonAlias) String() string { return strconv.FormatBool(*a.format == formatJSON) }

func (a jsonAlias) Type() string { return "bool" }

// Output formats of the commands with a --format flag.
const (
	formatText = "text"
	formatJSON = "json"
)
