package cli

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/stepandel/tickets-md/internal/ticket"
)

// --- priority ---

var priorityOptions = []string{"critical", "high", "medium", "low", "none"}

func (m *boardModel) startSetPriority() {
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	items := make([]pickerItem, 0, len(priorityOptions))
	current := strings.ToLower(strings.TrimSpace(t.Priority))
	for _, p := range priorityOptions {
		marker := ""
		if p == current || (p == "none" && current == "") {
			marker = "(current)"
		}
		items = append(items, pickerItem{label: p, key: marker, value: p})
	}
	m.overlay = newPicker(fmt.Sprintf("Set priority for %s", t.ID), items)
	m.overlayKind = "priority"
}

func (m *boardModel) applyPriorityChoice(ov overlay) {
	p, ok := ov.(*pickerOverlay)
	if !ok || p.choice == nil {
		return
	}
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	choice, _ := p.choice.value.(string)
	if choice == "none" {
		t.Priority = ""
	} else {
		t.Priority = choice
	}
	if err := m.store.Save(t); err != nil {
		m.err = err
		return
	}
	m.err = m.reload()
}

// --- delete ---

func (m *boardModel) startDelete() {
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	m.overlay = newConfirm(fmt.Sprintf("Delete %s — %s?", t.ID, truncate(t.Title, 40)))
	m.overlayKind = "delete"
}

func (m *boardModel) applyDeleteConfirm() {
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	if err := m.store.Delete(t.ID); err != nil {
		m.err = err
		return
	}
	m.err = m.reload()
	m.clampRow()
}

// --- copy id ---

func (m *boardModel) copySelectedID() {
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	if err := copyToClipboard(t.ID); err != nil {
		m.overlay = newNotice("error", "copy failed: "+err.Error())
		return
	}
	m.overlay = newNotice("info", "copied "+t.ID)
}

func copyToClipboard(s string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		if _, err := exec.LookPath("wl-copy"); err == nil {
			cmd = exec.Command("wl-copy")
		} else if _, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command("xclip", "-selection", "clipboard")
		} else if _, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command("xsel", "--clipboard", "--input")
		} else {
			return fmt.Errorf("no clipboard tool (install xclip, xsel, or wl-copy)")
		}
	default:
		return fmt.Errorf("clipboard copy not supported on %s", runtime.GOOS)
	}
	cmd.Stdin = strings.NewReader(s)
	return cmd.Run()
}

// --- link / unlink ---

// linkCtx carries the kind of link the user chose through the picker
// round-trip. sourceID is the selected ticket at the time the picker
// was opened (selection could change, but we want to act on the card
// the user was on).
type linkCtx struct {
	sourceID string
	kind     string // "related" | "blocked_by"
}

type unlinkEntry struct {
	peerID string
	kind   string // "related" | "blocked_by" | "blocks"
}

func (m *boardModel) startLink(kind string) {
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	all, err := m.store.ListAll()
	if err != nil {
		m.err = err
		return
	}
	// Build exclusion set: self + already-linked on this relation.
	exclude := map[string]bool{t.ID: true}
	switch kind {
	case "related":
		for _, r := range t.Related {
			exclude[r] = true
		}
	case "blocked_by":
		for _, r := range t.BlockedBy {
			exclude[r] = true
		}
	}
	var items []pickerItem
	for _, stage := range m.stages {
		for _, cand := range all[stage] {
			if exclude[cand.ID] {
				continue
			}
			items = append(items, pickerItem{
				label: cand.ID + " — " + cand.Title,
				key:   stage,
				value: cand.ID,
			})
		}
	}
	if len(items) == 0 {
		m.overlay = newNotice("info", "no eligible tickets")
		return
	}
	title := "Link related to " + t.ID
	if kind == "blocked_by" {
		title = "Mark " + t.ID + " as blocked by…"
	}
	m.overlay = newPicker(title, items)
	m.overlayKind = "link"
	m.overlayCtx = linkCtx{sourceID: t.ID, kind: kind}
}

func (m *boardModel) applyLinkChoice(ov overlay, ctx any) {
	p, ok := ov.(*pickerOverlay)
	if !ok || p.choice == nil {
		return
	}
	lc, ok := ctx.(linkCtx)
	if !ok {
		return
	}
	targetID, _ := p.choice.value.(string)
	if err := m.store.Link(lc.sourceID, targetID, lc.kind); err != nil {
		m.err = err
		return
	}
	m.err = m.reload()
}

func (m *boardModel) startUnlink() {
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	if !t.HasLinks() {
		m.overlay = newNotice("info", t.ID+" has no links")
		return
	}
	all, err := m.store.ListAll()
	if err != nil {
		m.err = err
		return
	}
	byID := make(map[string]ticket.Ticket)
	for _, ts := range all {
		for _, x := range ts {
			byID[x.ID] = x
		}
	}
	var items []pickerItem
	add := func(peer, kind, label string) {
		title := peer
		if pt, ok := byID[peer]; ok {
			title = peer + " — " + pt.Title
		}
		items = append(items, pickerItem{
			label: title,
			key:   label,
			value: unlinkEntry{peerID: peer, kind: kind},
		})
	}
	for _, r := range t.Related {
		add(r, "related", "related")
	}
	for _, r := range t.BlockedBy {
		add(r, "blocked_by", "blocked by")
	}
	for _, r := range t.Blocks {
		add(r, "blocks", "blocks")
	}
	m.overlay = newPicker("Unlink from "+t.ID, items)
	m.overlayKind = "unlink"
}

func (m *boardModel) applyUnlinkChoice(ov overlay) {
	p, ok := ov.(*pickerOverlay)
	if !ok || p.choice == nil {
		return
	}
	entry, ok := p.choice.value.(unlinkEntry)
	if !ok {
		return
	}
	t, ok := m.selectedTicket()
	if !ok {
		return
	}
	var err error
	switch entry.kind {
	case "related":
		err = m.store.Unlink(t.ID, entry.peerID, "related")
	case "blocked_by":
		err = m.store.Unlink(t.ID, entry.peerID, "blocked_by")
	case "blocks":
		// "t blocks peer" == "peer blocked_by t" — unlink the inverse.
		err = m.store.Unlink(entry.peerID, t.ID, "blocked_by")
	}
	if err != nil {
		m.err = err
		return
	}
	m.err = m.reload()
}
