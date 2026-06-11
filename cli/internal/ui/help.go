package ui

import (
	"fmt"
	"strings"
)

// HelpView is the generic help: banner, usage, and the command list only.
// Per-command flags/examples are intentionally NOT here — they live behind
// `a-novel <command> --help` (see CommandHelpView).
func HelpView(version string) string {
	var b strings.Builder
	b.WriteString(Banner(version))
	b.WriteString("\n\n")

	b.WriteString(styleGroup.Render("USAGE") + "\n")
	b.WriteString("  " + rootUsage + "\n\n")

	b.WriteString(styleGroup.Render("COMMANDS") + "\n")
	for _, c := range commandDocs {
		fmt.Fprintf(&b, "  %s  %s\n",
			styleBrand.Render(pad(c.name, 10)), styleMuted.Render(c.summary))
	}
	b.WriteString("\n")
	b.WriteString(styleMuted.Render(helpHint) + "\n")
	return b.String()
}

// CommandHelpView is the per-command help: banner, that command's usage, a
// description, and only the flags/examples that apply to it. An unknown name
// falls back to the generic screen (the caller has already validated in most
// paths; this keeps it safe regardless).
func CommandHelpView(version, name string) string {
	c, ok := lookupCommand(name)
	if !ok {
		return HelpView(version)
	}

	var b strings.Builder
	b.WriteString(Banner(version))
	b.WriteString("\n\n")

	b.WriteString(styleGroup.Render("USAGE") + "\n")
	b.WriteString("  " + c.usage + "\n\n")

	if c.long != "" {
		b.WriteString(para(c.long, termWidth()) + "\n\n")
	}

	if len(c.flags) > 0 {
		b.WriteString(styleGroup.Render("FLAGS") + "\n")
		for _, f := range c.flags {
			fmt.Fprintf(&b, "  %s  %s\n",
				styleAccent.Render(pad(f.name, 20)), styleMuted.Render(f.desc))
		}
		b.WriteString("\n")
	}

	if len(c.examples) > 0 {
		b.WriteString(styleGroup.Render("EXAMPLES") + "\n")
		// Each line is rendered on its own: handing lipgloss a string with
		// embedded newlines makes it pad every line to the block width, which
		// shifts later lines off the left margin.
		for _, ex := range c.examples {
			b.WriteString(styleMuted.Render("  "+ex) + "\n")
		}
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

// clip truncates s to at most maxLen runes, appending an ellipsis when cut.
// Used for the single-line live log tail so it never wraps the run view.
func clip(s string, maxLen int) string {
	if maxLen < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxLen {
		return s
	}
	if maxLen == 1 {
		return "…"
	}
	return string(r[:maxLen-1]) + "…"
}
