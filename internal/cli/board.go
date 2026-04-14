package cli

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"

	"tickets-md/internal/agent"
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

// --- tick message for periodic refresh ---

type boardTickMsg time.Time

func boardTickCmd() tea.Cmd {
	return tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
		return boardTickMsg(t)
	})
}

// --- model ---

type boardModel struct {
	store     *ticket.Store
	stages    []string
	columns   [][]ticket.Ticket
	activeCol int
	activeRow int
	width     int
	height    int
	err       error

	// Layout geometry for mouse hit-testing.
	colWidth int
	gap      int

	// Drag state.
	dragging    bool
	dragID      string
	dragFromCol int

	// Live status.
	watcherRunning bool
	agentStatuses  map[string]agent.AgentStatus // ticketID → status

	// New ticket input mode.
	// inputStep: 0=off, 1=typing title, 2=typing description
	inputStep  int
	inputTitle string
	descInput  textarea.Model

	// Overlay (picker / confirm / notice). When non-nil, captures all keys.
	overlay     overlay
	overlayKind string // discriminator so handleOverlayDone knows what to do
	overlayCtx  any    // optional context data (e.g. link kind)
}

func newBoardModel(s *ticket.Store) (*boardModel, error) {
	m := &boardModel{
		store:         s,
		stages:        s.Config.Stages,
		gap:           1,
		agentStatuses: make(map[string]agent.AgentStatus),
	}
	if err := m.reload(); err != nil {
		return nil, err
	}
	m.refreshStatus()
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

func (m *boardModel) refreshStatus() {
	m.watcherRunning = isWatcherRunning()
	statuses, err := agent.List(m.store.Root)
	if err != nil {
		return
	}
	m.agentStatuses = make(map[string]agent.AgentStatus, len(statuses))
	for _, as := range statuses {
		m.agentStatuses[as.TicketID] = as
	}
}

func isWatcherRunning() bool {
	return exec.Command("pgrep", "-f", "tickets watch").Run() == nil
}

func (m *boardModel) Init() tea.Cmd { return boardTickCmd() }

func (m *boardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case boardTickMsg:
		m.err = m.reload()
		m.refreshStatus()
		m.clampRow()
		return m, boardTickCmd()

	case tea.KeyPressMsg:
		if m.overlay != nil {
			return m.handleOverlayKey(msg)
		}
		if m.inputStep > 0 {
			return m.handleInput(msg)
		}
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
		m.refreshStatus()
		m.clampRow()
	case "enter", "o":
		m.openSelected()
	case "n", "+":
		m.inputStep = 1
		m.inputTitle = ""
	case "p":
		m.startSetPriority()
	case "D":
		m.startDelete()
	case "y":
		m.copySelectedID()
	case "R":
		m.startLink("related")
	case "b":
		m.startLink("blocked_by")
	case "u":
		m.startUnlink()
	case "A":
		m.startAdhocAgent()
	case "S":
		m.startRerunStageAgent()
	case "g":
		m.openAgentLog()
	case "f":
		m.startFollowup()
	case "d":
		m.viewDiff()
	}
	return m, nil
}

// finishOverlay runs the action associated with the current overlay's
// final value, then clears overlay state. Dispatch is by m.overlayKind.
func (m *boardModel) finishOverlay() {
	kind := m.overlayKind
	ov := m.overlay
	m.overlay = nil
	m.overlayKind = ""
	ctx := m.overlayCtx
	m.overlayCtx = nil

	switch kind {
	case "priority":
		m.applyPriorityChoice(ov)
	case "delete":
		m.applyDeleteConfirm()
	case "link":
		m.applyLinkChoice(ov, ctx)
	case "unlink":
		m.applyUnlinkChoice(ov)
	}
}

func (m *boardModel) handleOverlayKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	next, cmd, result := m.overlay.update(msg)
	m.overlay = next
	switch result {
	case overlayCancel:
		m.overlay = nil
		m.overlayKind = ""
		m.overlayCtx = nil
	case overlayDone:
		m.finishOverlay()
	}
	return m, cmd
}

func (m *boardModel) selectedTicket() (ticket.Ticket, bool) {
	if m.activeCol < 0 || m.activeCol >= len(m.columns) {
		return ticket.Ticket{}, false
	}
	col := m.columns[m.activeCol]
	if m.activeRow < 0 || m.activeRow >= len(col) {
		return ticket.Ticket{}, false
	}
	return col[m.activeRow], true
}

func (m *boardModel) handleInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "escape":
		m.inputStep = 0
		m.inputTitle = ""
		return m, nil
	}

	if m.inputStep == 1 {
		return m.handleTitleInput(msg)
	}
	return m.handleDescInput(msg)
}

func (m *boardModel) handleTitleInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		title := strings.TrimSpace(m.inputTitle)
		if title == "" {
			return m, nil
		}
		// Move to description step.
		m.inputStep = 2
		ta := textarea.New()
		ta.Placeholder = "Describe the ticket... (ctrl+s to save, esc to cancel)"
		ta.SetWidth(40)
		ta.SetHeight(6)
		ta.MaxHeight = 12
		m.descInput = ta
		return m, m.descInput.Focus()
	case "backspace":
		if len(m.inputTitle) > 0 {
			m.inputTitle = m.inputTitle[:len(m.inputTitle)-1]
		}
	case "space":
		m.inputTitle += " "
	default:
		s := msg.String()
		runes := []rune(s)
		if len(runes) == 1 && runes[0] >= 32 {
			m.inputTitle += s
		}
	}
	return m, nil
}

func (m *boardModel) handleDescInput(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Ctrl+S submits.
	if msg.String() == "ctrl+s" {
		return m.submitTicket()
	}

	// Forward everything else to the textarea.
	var cmd tea.Cmd
	m.descInput, cmd = m.descInput.Update(msg)
	return m, cmd
}

func (m *boardModel) submitTicket() (tea.Model, tea.Cmd) {
	title := strings.TrimSpace(m.inputTitle)
	desc := strings.TrimSpace(m.descInput.Value())

	m.inputStep = 0
	m.inputTitle = ""

	if title == "" {
		return m, nil
	}

	t, err := m.store.Create(title)
	if err != nil {
		m.err = err
		return m, nil
	}

	// If description was provided, update the ticket body.
	if desc != "" {
		t.Body = "## Description\n\n" + desc + "\n"
		if serr := m.store.Save(t); serr != nil {
			m.err = serr
		}
	}

	m.err = m.reload()
	m.activeCol = 0
	for i, c := range m.columns[0] {
		if c.ID == t.ID {
			m.activeRow = i
			break
		}
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

	targetCol := m.xToCol(msg.X)
	if targetCol < 0 || targetCol >= len(m.stages) || targetCol == m.dragFromCol {
		return m, nil
	}

	_, err := m.store.Move(m.dragID, m.stages[targetCol])
	if err != nil {
		m.err = err
		return m, nil
	}
	m.err = m.reload()
	m.activeCol = targetCol
	m.clampRow()
	for i, c := range m.columns[targetCol] {
		if c.ID == m.dragID {
			m.activeRow = i
			break
		}
	}
	return m, nil
}

func (m *boardModel) hitTest(x, y int) (col, row int, ok bool) {
	col = m.xToCol(x)
	if col < 0 || col >= len(m.stages) {
		return 0, 0, false
	}
	// Y layout: row 0 = status bar, row 1 = column top border,
	// row 2 = stage header, row 3+ = cards. Each card is 5 rows
	// (border-top + 2 content lines + border-bottom + margin-bottom).
	cardStartY := 3
	cardHeight := 5
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

func (m *boardModel) openSelected() {
	col := m.columns[m.activeCol]
	if len(col) == 0 {
		return
	}
	t := col[m.activeRow]
	name, editorArgs, err := resolveEditor()
	if err != nil {
		m.err = err
		return
	}
	argv := make([]string, 0, len(editorArgs)+1)
	argv = append(argv, editorArgs...)
	argv = append(argv, t.Path)
	cmd := exec.Command(name, argv...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		m.err = err
	}
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

// accentColor returns green when the watcher is running, pink otherwise.
func (m *boardModel) accentColor() string {
	if m.watcherRunning {
		return "#00D787"
	}
	return "#FF5F87"
}

func (m *boardModel) View() tea.View {
	if m.width == 0 {
		return tea.NewView("loading...")
	}

	numCols := len(m.stages)
	if numCols == 0 {
		return tea.NewView("no stages configured")
	}

	accentHex := m.accentColor()
	accent := lipgloss.Color(accentHex)

	// --- Status bar ---
	var statusBar string
	if m.watcherRunning {
		statusBar = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#00D787")).
			Padding(0, 1).
			Width(m.width).
			Render("● WATCHER RUNNING — agents will be spawned on ticket moves")
	} else {
		statusBar = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#555555")).
			Padding(0, 1).
			Width(m.width).
			Render("○ WATCHER STOPPED — run `tickets watch` in another terminal")
	}

	// --- Columns ---
	colWidth := (m.width - m.gap*(numCols-1)) / numCols
	if colWidth < 16 {
		colWidth = 16
	}
	m.colWidth = colWidth
	cardWidth := colWidth - 4

	var renderedCols []string
	for i, st := range m.stages {
		isActiveCol := i == m.activeCol

		// Header.
		hStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#5A56E0")).
			Padding(0, 1).
			Align(lipgloss.Center)
		if isActiveCol {
			hStyle = hStyle.Background(accent)
		}
		count := len(m.columns[i])
		header := hStyle.Width(colWidth - 4).Render(
			fmt.Sprintf("%s (%d)", st, count),
		)

		// Cards.
		var cards []string
		for j, t := range m.columns[i] {
			isActiveCard := isActiveCol && j == m.activeRow
			isDragged := m.dragging && t.ID == m.dragID

			cStyle := lipgloss.NewStyle().
				Border(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("#555555")).
				Padding(0, 1).
				MarginBottom(1)

			if isDragged {
				cStyle = cStyle.BorderForeground(lipgloss.Color("#FFD700")).Bold(true)
			} else if isActiveCard {
				cStyle = cStyle.BorderForeground(accent).Bold(true)
			}
			cStyle = cStyle.Width(cardWidth)

			// ID line.
			id := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(t.ID)
			priority := ""
			if t.Priority != "" {
				priority = " " + priorityStyle(t.Priority).Render("● "+t.Priority)
			}

			// Agent status badge.
			badge := m.agentBadge(t.ID)

			// Link count + blocked indicator.
			linkInfo := ""
			if n := t.LinkCount(); n > 0 {
				linkInfo = " " + lipgloss.NewStyle().Foreground(lipgloss.Color("#87CEEB")).Render(fmt.Sprintf("[%d]", n))
			}
			if len(t.BlockedBy) > 0 {
				linkInfo += " " + lipgloss.NewStyle().Foreground(lipgloss.Color("#FF8C00")).Bold(true).Render("!")
			}

			// Title.
			title := truncate(t.Title, cardWidth-2)

			cardContent := id + priority + badge + linkInfo + "\n" + title
			cards = append(cards, cStyle.Render(cardContent))
		}
		// Show inline input or [+] button at the bottom of the first column.
		if i == 0 {
			if m.inputStep == 1 {
				// Title input.
				cursor := "█"
				inputStyle := lipgloss.NewStyle().
					Border(lipgloss.NormalBorder()).
					BorderForeground(accent).
					Padding(0, 1).
					Width(cardWidth)
				inputLabel := lipgloss.NewStyle().
					Foreground(lipgloss.Color("#888888")).Render("title")
				inputText := m.inputTitle + cursor
				cards = append(cards, inputStyle.Render(inputLabel+"\n"+inputText))
			} else if m.inputStep == 2 {
				// Title (locked) + description textarea.
				formStyle := lipgloss.NewStyle().
					Border(lipgloss.NormalBorder()).
					BorderForeground(accent).
					Padding(0, 1).
					Width(cardWidth)
				titleLine := lipgloss.NewStyle().Bold(true).Render(m.inputTitle)
				descView := m.descInput.View()
				cards = append(cards, formStyle.Render(titleLine+"\n"+descView))
			} else {
				addBtn := lipgloss.NewStyle().
					Foreground(lipgloss.Color("#888888")).
					Width(cardWidth).
					Align(lipgloss.Center).
					Render("[+] new ticket (n)")
				cards = append(cards, addBtn)
			}
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

		// Column frame.
		cs := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#555555")).
			Padding(0, 1)
		if isActiveCol {
			cs = cs.BorderForeground(accent)
		}
		colContent := header + "\n" + body
		contentHeight := m.height - 6 // room for status bar + help bar
		cs = cs.Width(colWidth - 2).Height(contentHeight)

		renderedCols = append(renderedCols, cs.Render(colContent))
	}

	board := lipgloss.JoinHorizontal(lipgloss.Top, renderedCols...)

	// --- Help bar ---
	helpText := "h/l·j/k move  H/L shift  enter open  n new  p prio  D del  y copy  R/b link  u unlink  A/S agent  g log  f follow  d diff  q"
	if m.inputStep == 1 {
		helpText = "type a title, then enter to continue • esc to cancel"
	} else if m.inputStep == 2 {
		helpText = "type a description • ctrl+s to save • esc to cancel"
	} else if m.dragging {
		helpText = fmt.Sprintf("dragging %s — release over target column to move", m.dragID)
	}
	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		Padding(0, 0, 0, 1).
		Render(helpText)

	// Error display.
	errMsg := ""
	if m.err != nil {
		errMsg = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Bold(true).
			Render(" error: " + m.err.Error())
		m.err = nil
	}

	body := statusBar + "\n" + board + "\n" + help + errMsg

	if m.overlay != nil {
		ov := m.overlay.view(m.width)
		body += "\n" + ov
	}

	v := tea.NewView(body)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// agentBadge returns a colored status indicator for a ticket's agent.
func (m *boardModel) agentBadge(ticketID string) string {
	as, ok := m.agentStatuses[ticketID]
	if !ok {
		return ""
	}

	var icon, color string
	switch as.Status {
	case agent.StatusSpawned:
		icon, color = " ◐", "#FFD700" // yellow
	case agent.StatusRunning:
		icon, color = " ●", "#00D787" // green
	case agent.StatusBlocked:
		icon, color = " ⏸", "#FF8C00" // orange
	case agent.StatusDone:
		icon, color = " ✓", "#00D787" // green
	case agent.StatusFailed:
		icon, color = " ✗", "#FF5F5F" // red
	case agent.StatusErrored:
		icon, color = " ✗", "#FF5F5F" // red
	default:
		return ""
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(icon)
}

// priorityStyle returns a lipgloss style colored by priority level.
// Unknown values fall back to the legacy gold color so free-form
// priorities still render visibly.
func priorityStyle(value string) lipgloss.Style {
	s := lipgloss.NewStyle()
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "critical", "urgent":
		return s.Foreground(lipgloss.Color("#FF5F5F")).Bold(true)
	case "high":
		return s.Foreground(lipgloss.Color("#FF8C00")).Bold(true)
	case "medium", "med":
		return s.Foreground(lipgloss.Color("#FFD700"))
	case "low":
		return s.Foreground(lipgloss.Color("#888888"))
	default:
		return s.Foreground(lipgloss.Color("#FFD700"))
	}
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
