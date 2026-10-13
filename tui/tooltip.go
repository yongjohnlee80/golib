package tui

import (
	"strings"
	"time"

	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TOOLTIPS — a control's text, revealed in a card while the pointer rests on it or the keyboard
// brings focus to it: what a clipped label or a terse control means in full.
//
// A component opts in as a ToolTipper. The App shows the card, because only the App knows what
// the pointer is over and how focus arrived. It is no node: nothing hit-tests, focuses or routes
// to it, so the pointer over the card is still over the control beneath, and a dialog's own keys
// still reach the dialog. It is painted after the tree, so in a window it covers native views as
// any later-painted cell does (applyNatives).
//
//   - Hover: the nearest ToolTipper at or above the hovered node, with text, shows its card once the
//     pointer has rested on it for toolTipDelay.
//   - Keyboard: focus arriving by a key (Tab, Shift+Tab, a dialog's arrows) on a ToolTipper shows
//     its card at once. Focus a click gives shows nothing more than the hover does.
//   - It closes when the pointer leaves the control, when focus leaves a card shown for focus, on any
//     key or mouse press, when the control unmounts or is hidden, and when its text changes. A
//     pressed control shows no card again until the pointer has left it.
//
// Esc is not taken: it closes the card with whatever else it does, as any key does.

// ToolTipper is a component with text to reveal: ToolTip is read each frame, and "" is none.
type ToolTipper interface {
	Component
	ToolTip() string
}

// toolTipDelay is how long the pointer rests on a control before its card shows.
// NOTE: debounce: a pointer crossing a control on its way elsewhere must not flash its card.
const toolTipDelay = 500 * time.Millisecond

// The card's bounds: its text wraps at toolTipWidth columns (fewer on a narrow screen) and stops,
// with an ellipsis, after toolTipRows rows.
const (
	toolTipWidth = 60
	toolTipRows  = 6
)

// toolTipState is the card: the control it is for, and why.
type toolTipState struct {
	node    NodeID // the control shown for; 0: no card
	text    string // its text when shown: a change closes the card
	focus   bool   // shown for keyboard focus, not hover
	pending func() // cancels the hover timer, nil when none is armed
	pressed NodeID // the control a press closed the card of: no card again until the pointer leaves
	rect    Rect   // where the card was last painted, in screen cells
}

// toolTipTarget is the nearest ToolTipper with text at or above n, or nil.
func toolTipTarget(n *node) *node {
	for ; n != nil; n = n.parent {
		if t, ok := n.comp.(ToolTipper); ok && t.ToolTip() != "" {
			return n
		}
	}
	return nil
}

// toolTipHover follows the hovered node n (nil: none): the card of the control it left closes,
// and the control it is over arms its own.
func (a *App) toolTipHover(n *node) {
	t := toolTipTarget(n)
	if t == nil || t.id != a.tip.pressed {
		a.tip.pressed = 0
	}
	if a.tip.node != 0 && !a.tip.focus && (t == nil || t.id != a.tip.node) {
		a.hideToolTip()
	}
	a.cancelToolTipTimer()
	if t == nil || t.id == a.tip.node || t.id == a.tip.pressed {
		return
	}
	id := t.id
	a.tip.pending = a.addDeadline(toolTipDelay, func() {
		a.tip.pending = nil
		if n := a.nodes[id]; n != nil && toolTipTarget(a.nodes[a.hovered]) == n {
			a.showToolTip(n, false)
		}
	})
}

// toolTipFocus follows focus arriving at n: by a key, the card of the nearest ToolTipper at or
// above it shows at once (a ComboBox's field is inside the ComboBox); otherwise a card shown for
// the focus it left closes.
func (a *App) toolTipFocus(n *node, byKey bool) {
	if a.tip.focus && a.tip.node != 0 {
		a.hideToolTip()
	}
	if !byKey || n == nil {
		return
	}
	if t := toolTipTarget(n); t != nil {
		a.showToolTip(t, true)
	}
}

// toolTipPress is a key or mouse press: any card closes, and a pending one is dropped. A mouse
// press also holds the control under the pointer's card back until the pointer leaves it.
func (a *App) toolTipPress(mouse bool) {
	a.cancelToolTipTimer()
	if mouse {
		if t := toolTipTarget(a.nodes[a.hovered]); t != nil {
			a.tip.pressed = t.id
		}
	}
	a.hideToolTip()
}

func (a *App) showToolTip(n *node, focus bool) {
	a.tip.node, a.tip.text, a.tip.focus = n.id, n.comp.(ToolTipper).ToolTip(), focus
	a.renderDirty = true
	a.queue.wakeUp()
}

func (a *App) hideToolTip() {
	if a.tip.node == 0 {
		return
	}
	a.tip.node, a.tip.text, a.tip.focus, a.tip.rect = 0, "", false, Rect{}
	a.renderDirty = true
	a.queue.wakeUp()
}

func (a *App) cancelToolTipTimer() {
	if a.tip.pending != nil {
		a.tip.pending()
		a.tip.pending = nil
	}
}

// renderToolTip paints the card over the frame, below its control (above it when there is no room
// below), inside the screen. A control gone, hidden or with other text closes it instead.
func (a *App) renderToolTip() {
	if a.tip.node == 0 {
		return
	}
	n := a.nodes[a.tip.node]
	t, ok := ToolTipper(nil), false
	if n != nil {
		t, ok = n.comp.(ToolTipper)
	}
	if !ok || !n.mounted || !n.visible() || t.ToolTip() != a.tip.text {
		a.tip.node, a.tip.text, a.tip.focus, a.tip.rect = 0, "", false, Rect{}
		return
	}
	s := newRootSurface(a.buf, a.rctx)
	lines := wrapToolTip(a.tip.text, min(toolTipWidth, a.size.W-2), s.StringWidth)
	if len(lines) == 0 {
		return
	}
	w := 0
	for _, l := range lines {
		w = max(w, s.StringWidth(l))
	}
	r := placeToolTip(n.absRect, Size{W: w + 2, H: len(lines)}, a.size)
	a.buf.order++ // after every node: in a window, it covers the native views beneath
	st := style.New().Foreground(style.TokenForeground).Background(style.TokenPanel)
	s.Fill(r, " ", st)
	for i, l := range lines {
		x := r.X + 1
		for g := range grapheme.Clusters(l) {
			s.SetCell(x, r.Y+i, g, st)
			x += max(s.StringWidth(g), 1)
		}
	}
	a.tip.rect = r
}

// placeToolTip is where a card of size sz goes for a control at anchor, on a screen of size
// screen: under it, else over it, else at the screen's bottom; its left edge at the control's,
// moved left to stay on the screen.
func placeToolTip(anchor Rect, sz Size, screen Size) Rect {
	r := Rect{X: anchor.X, Y: anchor.Y + anchor.H, W: min(sz.W, screen.W), H: min(sz.H, screen.H)}
	if r.Y+r.H > screen.H {
		r.Y = anchor.Y - r.H
		if r.Y < 0 {
			r.Y = screen.H - r.H
		}
	}
	r.X = max(0, min(r.X, screen.W-r.W))
	return r
}

// wrapToolTip breaks text into lines of at most width columns: at spaces, and inside a word too
// long for a line; its own line breaks are kept. Past toolTipRows, the last line kept ends with an
// ellipsis.
func wrapToolTip(text string, width int, measure func(string) int) []string {
	if width < 1 {
		return nil
	}
	var lines []string
	for _, para := range strings.Split(text, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			if line != "" && measure(line)+1+measure(word) <= width {
				line += " " + word
				continue
			}
			if line != "" {
				lines = append(lines, line)
			}
			for measure(word) > width {
				head := cutToWidth(word, width, measure)
				lines, word = append(lines, head), word[len(head):]
			}
			line = word
		}
		lines = append(lines, line)
	}
	if len(lines) > toolTipRows {
		last := cutToWidth(lines[toolTipRows-1], width-1, measure)
		lines = append(lines[:toolTipRows-1], last+"…")
	}
	return lines
}

// cutToWidth is the longest prefix of s, whole clusters, at most width columns wide; at least its
// first cluster when s is not empty, so a cut always advances.
func cutToWidth(s string, width int, measure func(string) int) string {
	n, w := 0, 0
	for g := range grapheme.Clusters(s) {
		if n > 0 && w+measure(g) > width {
			break
		}
		n, w = n+len(g), w+measure(g)
	}
	return s[:n]
}
