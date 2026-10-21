package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE COMMAND LINE'S DROPDOWN: the candidates Tab offers, each with its arguments and help, under
// the field.

// completionList is the prompt's dropdown under the field: one candidate a row in a framed box, the
// one Tab chose highlighted, at most completionRows of them in view (the view follows the chosen
// one); or one message on its own (an error, a verb's arguments), wrapped and unframed. Empty, it
// takes no room.
type completionList struct {
	Base
	rows []string
	at   int    // the chosen row; -1 for none yet
	msg  string // shown in place of rows when set
}

// completionRows is how many candidates the dropdown shows at once.
const completionRows = 8

func (c *completionList) set(rows []string, at int) {
	c.rows, c.at, c.msg = rows, at, ""
	c.RequestLayout()
	c.MarkDirty()
}

func (c *completionList) choose(at int) {
	c.at = at
	c.MarkDirty()
}

func (c *completionList) say(msg string) {
	c.rows, c.at, c.msg = nil, -1, msg
	c.RequestLayout()
	c.MarkDirty()
}

func (c *completionList) clear() {
	if c.msg != "" || c.rows != nil {
		c.set(nil, -1)
	}
}

// Layout is the field's width, and a row for each candidate in view, or the message's lines.
func (c *completionList) Layout(cs tui.Constraints) tui.Size {
	w := cs.MaxW
	if w == tui.Unbounded {
		w = 0
		for _, r := range c.rows {
			w = max(w, c.measure(r))
		}
		w = max(w, c.measure(c.msg))
	}
	h := min(len(c.rows), completionRows)
	switch {
	case c.msg != "":
		h = len(wrapLine(c.msg, max(w, 1), c.measure))
	case h > 0:
		h += 2 // the frame
		if cs.MaxW == tui.Unbounded {
			w += 2
		}
	}
	return cs.Constrain(tui.Size{W: w, H: h})
}

// first is the first candidate in view: the chosen one is always in it.
func (c *completionList) first(h int) int {
	if c.at < h {
		return 0
	}
	return min(c.at-h+1, len(c.rows)-h)
}

// Render paints the rows on the card's colours, the chosen one as a selection.
func (c *completionList) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	card := cardText()
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", card)
	if c.msg != "" {
		for y, ln := range wrapLine(c.msg, sz.W, s.StringWidth) {
			if y >= sz.H {
				break
			}
			drawText(s, 0, y, ln, card)
		}
		return
	}
	if len(c.rows) == 0 || sz.W < 3 || sz.H < 3 {
		return
	}
	// the frame, on the card's colours with the theme's border
	drawFrame(s, tui.Rect{W: sz.W, H: sz.H}, card.Foreground(style.TokenBorder), style.BorderNormal)
	inner := s.Sub(tui.Rect{X: 1, Y: 1, W: sz.W - 2, H: sz.H - 2})
	iw, ih := sz.W-2, sz.H-2
	chosen := style.New().Background(style.TokenPrimary).Foreground(style.TokenTextOnPrimary)
	top := c.first(ih)
	for y := 0; y < ih && top+y < len(c.rows); y++ {
		i := top + y
		st := card
		if i == c.at {
			st = chosen
			inner.Fill(tui.Rect{Y: y, W: iw, H: 1}, " ", st)
		}
		drawText(inner, 0, y, truncate(c.rows[i], iw, inner.StringWidth), st)
	}
}
