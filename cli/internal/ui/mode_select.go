package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// ModeOption is one entry in the run-mode prompt: the wire value the caller
// receives (used as buildOpts.runMode), a short label and one-line summary.
type ModeOption struct {
	Value, Label, Detail string
}

// ModeSelectModel is the tiny TUI shown before the targets picker when the
// user did not pass --container / --live. It is deliberately minimal — just
// a vertical list with ↑/↓/enter — so an idle frame is byte-identical and
// no animation is needed (the anti-flicker invariant from the dashboard
// applies here too).
type ModeSelectModel struct {
	version  string
	options  []ModeOption
	cursor   int
	chosen   string
	aborted  bool
	quitting bool
	width    int
}

// NewModeSelect builds the mode prompt with the given options (caller passes
// container/live in the order they want them shown). defaultValue picks the
// initial cursor position — typically the per-mode default for the scope.
func NewModeSelect(version, defaultValue string, options []ModeOption) ModeSelectModel {
	m := ModeSelectModel{version: version, options: options}
	for i, o := range options {
		if o.Value == defaultValue {
			m.cursor = i
			break
		}
	}
	return m
}

func (m ModeSelectModel) Init() tea.Cmd { return nil }

// Chosen returns the selected value, or "" if the user aborted.
func (m ModeSelectModel) Chosen() string { return m.chosen }

// Aborted reports whether the user backed out (ctrl+c / esc / q) without
// confirming a choice.
func (m ModeSelectModel) Aborted() bool { return m.aborted }

func (m ModeSelectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			m.aborted = true
			m.quitting = true
			return m, tea.Quit
		case keyUp, "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case keyDown, "j":
			if m.cursor < len(m.options)-1 {
				m.cursor++
			}
			return m, nil
		case "enter":
			if m.cursor >= 0 && m.cursor < len(m.options) {
				m.chosen = m.options[m.cursor].Value
			}
			m.quitting = true
			return m, tea.Quit
		}
		// Number-key jump (1..N) matches the targets picker's UX.
		if k := msg.String(); len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
			if idx := int(k[0] - '1'); idx < len(m.options) {
				m.cursor = idx
			}
			return m, nil
		}
	}
	return m, nil
}

func (m ModeSelectModel) View() string {
	w := m.width
	if w <= 0 {
		w = termWidth()
	}
	var b strings.Builder
	b.WriteString(Banner(m.version) + "\n")
	b.WriteString(section("run mode", colGold, w) + "\n")
	b.WriteString(para(
		"Pick how each selected target should be launched. You can override "+
			"this default with --container or --live on the command line so "+
			"this prompt is skipped.", w) + "\n\n")

	for i, o := range m.options {
		cursor := "  "
		label := styleBrand.Render(o.Label)
		mark := styleMuted.Render(glyphUnchecked)
		if i == m.cursor {
			cursor = styleSel.Render(glyphCursor) + " "
			mark = styleSel.Render(glyphChecked)
		}
		b.WriteString(ansi.Truncate(cursor+mark+" "+label, w, "…") + "\n")
		b.WriteString(ansi.Truncate("      "+styleMuted.Render(o.Detail), w, "…") + "\n\n")
	}
	b.WriteString(styleHelp.Render("↑/↓ select · 1-" +
		string(rune('0'+len(m.options))) + " jump · enter confirm · q quit"))
	return b.String()
}
