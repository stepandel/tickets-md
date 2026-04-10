package cli

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"tickets-md/internal/ticket"
)

func newBoardCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "board",
		Aliases: []string{"tui"},
		Short:   "Interactive kanban board (TUI)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := openStore()
			if err != nil {
				return err
			}
			return runBoard(s)
		},
	}
}

func runBoard(s *ticket.Store) error {
	m, err := newBoardModel(s)
	if err != nil {
		return err
	}
	p := tea.NewProgram(m)
	_, err = p.Run()
	return err
}

// --- model ---

type boardModel struct {
	store     *ticket.Store
	stages    []string
	columns   [][]ticket.Ticket // columns[stageIdx][cardIdx]
	activeCol int
	activeRow int
	width     int
	height    int
	err       error
}

func newBoardModel(s *ticket.Store) (*boardModel, error) {
	m := &boardModel{
		store:  s,
		stages: s.Config.Stages,
	}
	if err := m.reload(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *boardModel) reload() error {
	grouped, err := m.store.ListAll()
	if err != nil {
		return err
	}
	m.columns = make([][]ticket.Ticket, len(m.stages))
	for i, st := range m.stages {
		m.columns[i] = grouped[st]
	}
	return nil
}

func (m *boardModel) Init() tea.Cmd { return nil }

func (m *boardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		// Navigate between columns.
		case "h", "left":
			if m.activeCol > 0 {
				m.activeCol--
				m.clampRow()
			}
		case "l", "right":
			if m.activeCol < len(m.stages)-1 {
				m.activeCol++
				m.clampRow()
			}

		// Navigate within a column.
		case "j", "down":
			if m.activeRow < len(m.columns[m.activeCol])-1 {
				m.activeRow++
			}
		case "k", "up":
			if m.activeRow > 0 {
				m.activeRow--
			}

		// Move card to the right (next stage).
		case "L", "shift+right":
			m.moveCard(1)
		// Move card to the left (prev stage).
		case "H", "shift+left":
			m.moveCard(-1)

		// Reload from disk.
		case "r":
			m.err = m.reload()
			m.clampRow()
		}
	}
	return m, nil
}

func (m *boardModel) moveCard(dir int) {
	col := m.columns[m.activeCol]
	if len(col) == 0 {
		return
	}
	target := m.activeCol + dir
	if target < 0 || target >= len(m.stages) {
		return
	}
	t := col[m.activeRow]
	_, err := m.store.Move(t.ID, m.stages[target])
	if err != nil {
		m.err = err
		return
	}
	m.err = m.reload()
	// Follow the card to the target column.
	m.activeCol = target
	m.clampRow()
	// Try to keep the same card selected.
	for i, c := range m.columns[target] {
		if c.ID == t.ID {
			m.activeRow = i
			break
		}
	}
}

func (m *boardModel) clampRow() {
	max := len(m.columns[m.activeCol]) - 1
	if max < 0 {
		max = 0
	}
	if m.activeRow > max {
		m.activeRow = max
	}
}

// --- view ---

var (
	// Column styles.
	colHeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#5A56E0")).
			Padding(0, 1).
			Align(lipgloss.Center)

	colHeaderActiveStyle = colHeaderStyle.
				Background(lipgloss.Color("#FF5F87"))

	colStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#555555")).
			Padding(0, 1)

	colActiveStyle = colStyle.
			BorderForeground(lipgloss.Color("#FF5F87"))

	// Card styles.
	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#555555")).
			Padding(0, 1).
			MarginBottom(1)

	cardActiveStyle = cardStyle.
			BorderForeground(lipgloss.Color("#FF5F87")).
			Bold(true)

	cardIDStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	cardPriorityStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFD700"))

	// Help bar.
	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666")).
			Padding(1, 0, 0, 1)

	errStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true)
)

func (m *boardModel) View() tea.View {
	if m.width == 0 {
		return tea.NewView("loading...")
	}

	numCols := len(m.stages)
	if numCols == 0 {
		return tea.NewView("no stages configured")
	}

	// Calculate column width: distribute evenly, leave room for gaps.
	gap := 1
	colWidth := (m.width - gap*(numCols-1)) / numCols
	if colWidth < 16 {
		colWidth = 16
	}
	cardWidth := colWidth - 4 // account for column border + padding

	var renderedCols []string
	for i, st := range m.stages {
		isActiveCol := i == m.activeCol

		// Header.
		hStyle := colHeaderStyle
		if isActiveCol {
			hStyle = colHeaderActiveStyle
		}
		count := len(m.columns[i])
		header := hStyle.Width(colWidth - 4).Render(
			fmt.Sprintf("%s (%d)", st, count),
		)

		// Cards.
		var cards []string
		for j, t := range m.columns[i] {
			isActiveCard := isActiveCol && j == m.activeRow
			cStyle := cardStyle
			if isActiveCard {
				cStyle = cardActiveStyle
			}
			cStyle = cStyle.Width(cardWidth)

			id := cardIDStyle.Render(t.ID)
			title := truncate(t.Title, cardWidth-2)
			priority := ""
			if t.Priority != "" {
				priority = " " + cardPriorityStyle.Render(t.Priority)
			}
			cards = append(cards, cStyle.Render(id+priority+"\n"+title))
		}
		if len(cards) == 0 {
			empty := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#555555")).
				Width(cardWidth).
				Align(lipgloss.Center).
				Render("empty")
			cards = append(cards, empty)
		}

		body := lipgloss.JoinVertical(lipgloss.Left, cards...)

		// Full column.
		cs := colStyle
		if isActiveCol {
			cs = colActiveStyle
		}
		colContent := header + "\n" + body

		// Set height so all columns are equal.
		contentHeight := m.height - 5 // room for help bar + padding
		cs = cs.Width(colWidth - 2).Height(contentHeight)

		renderedCols = append(renderedCols, cs.Render(colContent))
	}

	board := lipgloss.JoinHorizontal(lipgloss.Top, renderedCols...)

	// Help bar.
	help := helpStyle.Render(
		"h/l: columns  j/k: cards  H/L: move card  r: reload  q: quit",
	)

	// Error display.
	errMsg := ""
	if m.err != nil {
		errMsg = errStyle.Render("error: " + m.err.Error())
		m.err = nil
	}

	v := tea.NewView(board + "\n" + help + errMsg)
	v.AltScreen = true
	return v
}

func truncate(s string, max int) string {
	// Truncate by runes, not bytes.
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

// viewTicketDetail could be added later for a detail pane on Enter.
// For now we keep the board view simple.

// Ensure boardModel satisfies the tea.Model interface.
var _ tea.Model = (*boardModel)(nil)

// stripForView removes all leading/trailing whitespace lines that
// lipgloss sometimes adds.
func stripForView(s string) string {
	lines := strings.Split(s, "\n")
	// Trim trailing empty lines.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
