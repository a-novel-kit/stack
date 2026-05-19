package ui

import (
	"fmt"
	"strings"
)

// command documents one subcommand for the help screen.
type command struct{ name, desc string }

// flagDoc documents one `build` flag for the help screen.
type flagDoc struct{ name, desc string }

var commands = []command{
	{"build", "Detect and build Go modules, pnpm scripts, and Podman images"},
	{"version", "Print the CLI version"},
	{"help", "Show this help"},
}

var buildFlags = []flagDoc{
	{"-C, --dir <path>", "Directory to scan (default: current directory)"},
	{"-t, --type <kinds>", "Comma-separated filter: go,pnpm,podman (default: all)"},
	{"-j, --jobs <n>", "Max parallel builds, interactive only (default: CPU count)"},
	{"-y, --yes", "Skip the menu; build everything non-interactively (sequential)"},
	{"--dry-run", "List detected targets and exit without building"},
	{"-h, --help", "Show this help"},
}

// HelpView renders the branded help screen: banner, usage, commands, flags.
func HelpView(version string) string {
	var b strings.Builder
	b.WriteString(Banner(version))
	b.WriteString("\n\n")

	b.WriteString(styleGroup.Render("USAGE") + "\n")
	b.WriteString("  a-novel <command> [flags]\n\n")

	b.WriteString(styleGroup.Render("COMMANDS") + "\n")
	for _, c := range commands {
		fmt.Fprintf(&b, "  %s  %s\n",
			styleBrand.Render(pad(c.name, 10)), styleMuted.Render(c.desc))
	}
	b.WriteString("\n")

	b.WriteString(styleGroup.Render("BUILD FLAGS") + "\n")
	for _, f := range buildFlags {
		fmt.Fprintf(&b, "  %s  %s\n",
			styleAccent.Render(pad(f.name, 20)), styleMuted.Render(f.desc))
	}
	b.WriteString("\n")

	b.WriteString(styleGroup.Render("EXAMPLES") + "\n")
	// Each line is rendered on its own: handing lipgloss a string with
	// embedded newlines makes it pad every line to the block width, which
	// shifts later lines off the left margin.
	examples := []string{
		"  a-novel build                 # interactive menu, all selected",
		"  a-novel build -t go,podman    # only Go + Podman targets",
		"  a-novel build -j 4            # cap parallelism at 4 builds",
		"  a-novel build -y              # build everything, no prompt",
		"  a-novel build --dry-run       # just show what would build",
	}
	for _, ex := range examples {
		b.WriteString(styleMuted.Render(ex) + "\n")
	}
	return b.String()
}

// pad right-pads s with spaces to width w (no truncation — help labels are
// authored to fit).
func pad(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return s + strings.Repeat(" ", w-len(s))
}
