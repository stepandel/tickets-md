package cli

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Overlay is the interface every board overlay (picker, confirm,
// notice) implements. Overlays own key handling while active, and
// render a small floating box the board draws on top of its view.
type overlay interface {
	update(msg tea.KeyPressMsg) (overlay, tea.Cmd, overlayResult)
	view(width int) string
}

// overlayResult signals the outcome of the last Update.
type overlayResult int

const (
	overlayContinue overlayResult = iota // overlay still active
	overlayCancel                        // overlay dismissed without a value
	overlayDone                          // overlay produced a value (stored on the overlay itself)
)

// --- pickerOverlay: fuzzy-filtered list picker ---

type pickerItem struct {
	label string // rendered
	key   string // for display on the right (optional)
	value any    // arbitrary payload
}

type pickerOverlay struct {
	title    string
	items    []pickerItem
	filter   string
	filtered []int // indexes into items
	cursor   int
	choice   *pickerItem // set when overlayDone
}

func newPicker(title string, items []pickerItem) *pickerOverlay {
	p := &pickerOverlay{title: title, items: items}
	p.refilter()
	return p
}

func (p *pickerOverlay) refilter() {
	p.filtered = p.filtered[:0]
	f := strings.ToLower(p.filter)
	for i, it := range p.items {
		if f == "" || strings.Contains(strings.ToLower(it.label), f) {
			p.filtered = append(p.filtered, i)
		}
	}
	if p.cursor >= len(p.filtered) {
		p.cursor = len(p.filtered) - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
}

func (p *pickerOverlay) update(msg tea.KeyPressMsg) (overlay, tea.Cmd, overlayResult) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return p, nil, overlayCancel
	case "enter":
		if len(p.filtered) == 0 {
			return p, nil, overlayCancel
		}
		item := p.items[p.filtered[p.cursor]]
		p.choice = &item
		return p, nil, overlayDone
	case "down", "ctrl+n":
		if p.cursor < len(p.filtered)-1 {
			p.cursor++
		}
	case "up", "ctrl+p":
		if p.cursor > 0 {
			p.cursor--
		}
	case "backspace":
		if len(p.filter) > 0 {
			p.filter = p.filter[:len(p.filter)-1]
			p.refilter()
		}
	case "space":
		p.filter += " "
		p.refilter()
	default:
		s := msg.String()
		runes := []rune(s)
		if len(runes) == 1 && runes[0] >= 32 {
			p.filter += s
			p.refilter()
		}
	}
	return p, nil, overlayContinue
}

func (p *pickerOverlay) view(width int) string {
	boxWidth := width - 8
	if boxWidth < 30 {
		boxWidth = 30
	}
	if boxWidth > 70 {
		boxWidth = 70
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF"))
	filterStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA"))
	cursorStyle := lipgloss.NewStyle().Background(lipgloss.Color("#5A56E0")).Foreground(lipgloss.Color("#FFFFFF"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666"))

	var b strings.Builder
	b.WriteString(titleStyle.Render(p.title))
	b.WriteString("\n")
	b.WriteString(filterStyle.Render("> " + p.filter + "█"))
	b.WriteString("\n")

	maxRows := 10
	start := 0
	if p.cursor >= maxRows {
		start = p.cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(p.filtered) {
		end = len(p.filtered)
	}
	if len(p.filtered) == 0 {
		b.WriteString(hintStyle.Render("  (no matches)"))
		b.WriteString("\n")
	}
	for i := start; i < end; i++ {
		item := p.items[p.filtered[i]]
		line := "  " + item.label
		if item.key != "" {
			line += "  " + hintStyle.Render(item.key)
		}
		line = truncate(line, boxWidth-2)
		if i == p.cursor {
			b.WriteString(cursorStyle.Render("▸ " + truncate(item.label, boxWidth-4)))
		} else {
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	b.WriteString(hintStyle.Render("enter: select • esc: cancel"))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#5A56E0")).
		Padding(0, 1).
		Width(boxWidth).
		Render(b.String())
	return box
}

// --- confirmOverlay: y/N prompt ---

type confirmOverlay struct {
	prompt string
}

func newConfirm(prompt string) *confirmOverlay {
	return &confirmOverlay{prompt: prompt}
}

func (c *confirmOverlay) update(msg tea.KeyPressMsg) (overlay, tea.Cmd, overlayResult) {
	switch msg.String() {
	case "y", "Y":
		return c, nil, overlayDone
	case "n", "N", "esc", "ctrl+c", "enter":
		return c, nil, overlayCancel
	}
	return c, nil, overlayContinue
}

func (c *confirmOverlay) view(width int) string {
	boxWidth := width - 8
	if boxWidth < 30 {
		boxWidth = 30
	}
	if boxWidth > 60 {
		boxWidth = 60
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Render("y: confirm • n/esc: cancel")
	body := c.prompt + "\n" + hint
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#FF5F5F")).
		Padding(0, 1).
		Width(boxWidth).
		Render(body)
}

// --- noticeOverlay: transient message with a single dismiss key ---

type noticeOverlay struct {
	msg  string
	kind string // "info" | "error"
}

func newNotice(kind, msg string) *noticeOverlay { return &noticeOverlay{kind: kind, msg: msg} }

func (n *noticeOverlay) update(msg tea.KeyPressMsg) (overlay, tea.Cmd, overlayResult) {
	return n, nil, overlayCancel
}

func (n *noticeOverlay) view(width int) string {
	boxWidth := width - 8
	if boxWidth < 30 {
		boxWidth = 30
	}
	if boxWidth > 70 {
		boxWidth = 70
	}
	color := "#00D787"
	if n.kind == "error" {
		color = "#FF5F5F"
	}
	hint := lipgloss.NewStyle().Foreground(lipgloss.Color("#666666")).Render("press any key")
	body := n.msg + "\n" + hint
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(color)).
		Padding(0, 1).
		Width(boxWidth).
		Render(body)
}
