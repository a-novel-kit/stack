package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/a-novel-kit/stack/cli/internal/detect"
)

// Pick lets the user choose which targets to run, every one selected at first.
// It returns the chosen targets in display order, or false when the user quit.
func Pick(verb string, targets []detect.Target) ([]detect.Target, bool, error) {
	final, err := tea.NewProgram(newPicker(verb, targets)).Run()
	if err != nil {
		return nil, false, err
	}
	p, _ := final.(picker)
	return p.chosen, p.chosen != nil, nil
}

// pickRow is a picker line: a kind heading (target < 0) or a target.
type pickRow struct {
	kind   detect.Kind
	target int
}

type picker struct {
	verb     string
	targets  []detect.Target
	selected []bool
	rows     []pickRow
	cursor   int
	width    int
	chosen   []detect.Target
}

func newPicker(verb string, targets []detect.Target) picker {
	p := picker{verb: verb, targets: targets, selected: make([]bool, len(targets))}
	for i, t := range targets {
		// Targets arrive sorted by kind, so a heading opens each run of them.
		if i == 0 || targets[i-1].Kind != t.Kind {
			p.rows = append(p.rows, pickRow{kind: t.Kind, target: -1})
		}
		p.rows = append(p.rows, pickRow{kind: t.Kind, target: i})
		p.selected[i] = true
	}
	return p
}

func (p picker) Init() tea.Cmd { return nil }

func (p picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width = msg.Width
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc", "q":
			return p, tea.Quit
		case "up", "k":
			p.cursor = max(p.cursor-1, 0)
		case "down", "j":
			p.cursor = min(p.cursor+1, len(p.rows)-1)
		case "space", "x":
			if row := p.rows[p.cursor]; row.target >= 0 {
				p.selected[row.target] = !p.selected[row.target]
			} else {
				p.toggle(func(t detect.Target) bool { return t.Kind == row.kind })
			}
		case "g":
			kind := p.rows[p.cursor].kind
			p.toggle(func(t detect.Target) bool { return t.Kind == kind })
		case "a":
			p.toggle(func(detect.Target) bool { return true })
		case "enter":
			for i, t := range p.targets {
				if p.selected[i] {
					p.chosen = append(p.chosen, t)
				}
			}
			if p.chosen != nil {
				return p, tea.Quit
			}
		}
	}
	return p, nil
}

// toggle flips the targets matching in: it clears them when every one is
// selected and selects them all otherwise, which stays predictable under a
// mixed state.
func (p *picker) toggle(in func(detect.Target) bool) {
	all := true
	for i, t := range p.targets {
		all = all && (!in(t) || p.selected[i])
	}
	for i, t := range p.targets {
		if in(t) {
			p.selected[i] = !all
		}
	}
}

func (p picker) View() tea.View {
	w := p.width
	if w <= 0 {
		w = termWidth()
	}
	var b strings.Builder
	b.WriteString(Banner() + "\n\n")
	b.WriteString(section("select targets", colGold, w) + "\n")
	b.WriteString(para("Everything is selected by default. Toggle what to "+p.verb+
		", then press enter. Group headings toggle the whole kind.", w) + "\n\n")

	count := 0
	for i, row := range p.rows {
		cursor := "  "
		if i == p.cursor {
			cursor = Brand.Render(glyphCursor) + " "
		}
		if row.target < 0 {
			box, n := p.groupBox(row.kind)
			fmt.Fprintf(&b, "%s%s %s %s\n", cursor, box, Gold.Render(kindLabel(row.kind)), Muted.Render(fmt.Sprintf("(%d)", n)))
			continue
		}
		t := p.targets[row.target]
		box, name := Muted.Render(glyphUnchecked), Muted.Render(t.Name)
		if p.selected[row.target] {
			count++
			box, name = Brand.Render(glyphChecked), lipgloss.NewStyle().Bold(true).Render(t.Name)
		}
		// The detail line indents under the name: cursor, nesting, box, space.
		fmt.Fprintf(&b, "%s  %s %s %s\n", cursor, box, name, Muted.Render(relLabel(t.RelDir)))
		fmt.Fprintf(&b, "      %s\n", Muted.Render(Truncate(t.Detail, w-6)))
	}

	b.WriteString("\n" + rule(w) + "\n")
	b.WriteString(Gold.Render(fmt.Sprintf("%d of %d selected", count, len(p.targets))) + "\n")
	b.WriteString(Muted.Render("↑/↓ move · space toggle · g toggle group · a toggle all · enter "+p.verb+" · q quit") + "\n")
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

// groupBox is a heading's tri-state checkbox, filled when every member is
// selected, empty when none is and half-filled in gold otherwise, and the
// group's size.
func (p picker) groupBox(kind detect.Kind) (string, int) {
	on, total := 0, 0
	for i, t := range p.targets {
		if t.Kind == kind {
			total++
			if p.selected[i] {
				on++
			}
		}
	}
	switch on {
	case total:
		return Brand.Render(glyphChecked), total
	case 0:
		return Muted.Render(glyphUnchecked), total
	default:
		return Gold.Render(glyphPartial), total
	}
}
