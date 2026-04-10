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

	// Layout geometry — computed during View(), used for mouse hit-testing.
	colWidth int // width of each rendered column (including border)
	gap      int // horizontal gap between columns

	// Drag state.
	dragging   bool   // true while mouse button is held on a card
	dragID     string // ticket ID being dragged
	dragFromCol int   // column the drag started in
}

func newBoardModel(s *ticket.Store) (*boardModel, error) {
	m := &boardModel{
		store:  s,
		stages: s.Config.Stages,
		gap:    1,
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
		return m.handleKey(msg)

	case tea.MouseClickMsg:
		return m.handleMouseClick(msg)

	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)
	}
	return m, nil
}

func (m *boardModel) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

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
	case "j", "down":
		if m.activeRow < len(m.columns[m.activeCol])-1 {
			m.activeRow++
		}
	case "k", "up":
		if m.activeRow > 0 {
			m.activeRow--
		}
	case "L", "shift+right":
		m.moveCard(1)
	case "H", "shift+left":
		m.moveCard(-1)
	case "r":
		m.err = m.reload()
		m.clampRow()
	}
	return m, nil
}

func (m *boardModel) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	col, row, ok := m.hitTest(msg.X, msg.Y)
	if !ok {
		return m, nil
	}
	m.activeCol = col
	m.activeRow = row
	m.clampRow()

	// Start drag if we clicked on an actual card.
	if row < len(m.columns[col]) {
		m.dragging = true
		m.dragID = m.columns[col][row].ID
		m.dragFromCol = col
	}
	return m, nil
}

func (m *boardModel) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if !m.dragging {
		return m, nil
	}
	m.dragging = false

	// Determine which column the mouse was released over.
	targetCol := m.xToCol(msg.X)
	if targetCol < 0 || targetCol >= len(m.stages) || targetCol == m.dragFromCol {
		return m, nil
	}

	// Move the dragged ticket to the target column.
	_, err := m.store.Move(m.dragID, m.stages[targetCol])
	if err != nil {
		m.err = err
		return m, nil
	}
	m.err = m.reload()
	m.activeCol = targetCol
	m.clampRow()
	// Select the moved card.
	for i, c := range m.columns[targetCol] {
		if c.ID == m.dragID {
			m.activeRow = i
			break
		}
	}
	return m, nil
}

// hitTest maps terminal coordinates to a (column, row) pair.
// row is the card index within the column (may be >= len(cards)
// if the click is in empty space below the last card).
func (m *boardModel) hitTest(x, y int) (col, row int, ok bool) {
	col = m.xToCol(x)
	if col < 0 || col >= len(m.stages) {
		return 0, 0, false
	}

	// Y layout within a column (approximate):
	// row 0: top border
	// row 1: header
	// row 2+: cards — each card is ~4 rows (border + content + margin)
	cardStartY := 2
	cardHeight := 4 // border-top + id line + title line + border-bottom
	if y < cardStartY {
		return col, 0, true
	}
	row = (y - cardStartY) / cardHeight
	maxRow := len(m.columns[col]) - 1
	if maxRow < 0 {
		maxRow = 0
	}
	if row > maxRow {
		row = maxRow
	}
	return col, row, true
}

// xToCol maps an X coordinate to a column index.
func (m *boardModel) xToCol(x int) int {
	if m.colWidth <= 0 {
		return -1
	}
	stride := m.colWidth + m.gap
	col := x / stride
	if col < 0 {
		return 0
	}
	if col >= len(m.stages) {
		return len(m.stages) - 1
	}
	return col
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
	m.activeCol = target
	m.clampRow()
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

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#555555")).
			Padding(0, 1).
			MarginBottom(1)

	cardActiveStyle = cardStyle.
			BorderForeground(lipgloss.Color("#FF5F87")).
			Bold(true)

	cardDraggingStyle = cardStyle.
				BorderForeground(lipgloss.Color("#FFD700")).
				Bold(true)

	cardIDStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	cardPriorityStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFD700"))

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

	colWidth := (m.width - m.gap*(numCols-1)) / numCols
	if colWidth < 16 {
		colWidth = 16
	}
	m.colWidth = colWidth // store for mouse hit-testing
	cardWidth := colWidth - 4

	var renderedCols []string
	for i, st := range m.stages {
		isActiveCol := i == m.activeCol

		hStyle := colHeaderStyle
		if isActiveCol {
			hStyle = colHeaderActiveStyle
		}
		count := len(m.columns[i])
		header := hStyle.Width(colWidth - 4).Render(
			fmt.Sprintf("%s (%d)", st, count),
		)

		var cards []string
		for j, t := range m.columns[i] {
			isActiveCard := isActiveCol && j == m.activeRow
			isDragged := m.dragging && t.ID == m.dragID

			cStyle := cardStyle
			if isDragged {
				cStyle = cardDraggingStyle
			} else if isActiveCard {
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

		cs := colStyle
		if isActiveCol {
			cs = colActiveStyle
		}
		colContent := header + "\n" + body
		contentHeight := m.height - 5
		cs = cs.Width(colWidth - 2).Height(contentHeight)

		renderedCols = append(renderedCols, cs.Render(colContent))
	}

	board := lipgloss.JoinHorizontal(lipgloss.Top, renderedCols...)

	helpText := "h/l: columns  j/k: cards  H/L: move card  mouse: drag & drop  r: reload  q: quit"
	if m.dragging {
		helpText = fmt.Sprintf("dragging %s — release over target column to move", m.dragID)
	}
	help := helpStyle.Render(helpText)

	errMsg := ""
	if m.err != nil {
		errMsg = errStyle.Render("error: " + m.err.Error())
		m.err = nil
	}

	v := tea.NewView(board + "\n" + help + errMsg)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

var _ tea.Model = (*boardModel)(nil)

// stripForView removes all leading/trailing whitespace lines that
// lipgloss sometimes adds.
func stripForView(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
