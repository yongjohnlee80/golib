package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// StatusBar is a height-1, dock-bottom chrome line with left/center/right
// segments (ADR-0007 §2.5). Segments truncate center-first, then left, then
// right — the rightmost content (usually keybinding hints) survives longest,
// the lazygit convention. Not focusable; consumes nothing.
type StatusBar struct {
	Base
	bar                 style.Style
	left, center, right segment
}

var _ tui.Component = (*StatusBar)(nil)

type segment struct {
	text string
	st   style.Style
	set  bool
}

// StatusBarOption customizes a StatusBar under construction.
type StatusBarOption func(*StatusBar)

// WithBarStyle replaces the bar's base style (default background
// style.TokenPanel).
func WithBarStyle(st style.Style) StatusBarOption {
	return func(s *StatusBar) { s.bar = st }
}

// NewStatusBar builds an empty status bar.
func NewStatusBar(opts ...StatusBarOption) *StatusBar {
	s := &StatusBar{bar: style.New().Background(style.TokenPanel).Foreground(style.TokenForeground)}
	for _, o := range opts {
		if o != nil {
			o(s)
		}
	}
	return s
}

func setSegment(seg *segment, text string, st []style.Style) {
	if len(st) > 1 {
		panic(fmt.Sprintf("widget: StatusBar segment: %d styles (want at most 1)", len(st)))
	}
	seg.text = text
	seg.set = len(st) == 1
	if seg.set {
		seg.st = st[0]
	}
}

// SetLeft replaces the left segment (optional per-segment style).
func (s *StatusBar) SetLeft(text string, st ...style.Style) {
	setSegment(&s.left, text, st)
	s.MarkDirty()
}

// SetCenter replaces the center segment.
func (s *StatusBar) SetCenter(text string, st ...style.Style) {
	setSegment(&s.center, text, st)
	s.MarkDirty()
}

// SetRight replaces the right segment.
func (s *StatusBar) SetRight(text string, st ...style.Style) {
	setSegment(&s.right, text, st)
	s.MarkDirty()
}

// Layout is height 1, width greedy.
func (s *StatusBar) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: boundedMax(c.MaxW, c.MinW), H: 1})
}

// segStyle merges a segment's style over the bar style.
func (s *StatusBar) segStyle(seg segment) style.Style {
	if !seg.set {
		return s.bar
	}
	return seg.st.Inherit(s.bar)
}

// Render paints the bar background and the three segments with the
// truncation priority right > left > center.
func (s *StatusBar) Render(sur tui.Surface) {
	w := sur.Size().W
	if w <= 0 {
		return
	}
	sur.Fill(tui.Rect{X: 0, Y: 0, W: w, H: 1}, " ", s.bar)

	// Right first: it survives longest.
	rt := truncate(s.right.text, w, sur.StringWidth)
	rw := sur.StringWidth(rt)
	if rt != "" {
		drawText(sur, w-rw, 0, rt, s.segStyle(s.right))
	}
	// Left in what remains (one-cell gap before right).
	lAvail := w - rw
	if rw > 0 {
		lAvail--
	}
	lt := truncate(s.left.text, max(lAvail, 0), sur.StringWidth)
	lw := sur.StringWidth(lt)
	if lt != "" {
		drawText(sur, 0, 0, lt, s.segStyle(s.left))
	}
	// Center in the gap between left and right, truncated first.
	lo := lw
	if lw > 0 {
		lo++
	}
	hi := w - rw
	if rw > 0 {
		hi--
	}
	cAvail := hi - lo
	if cAvail <= 0 || s.center.text == "" {
		return
	}
	ct := truncate(s.center.text, cAvail, sur.StringWidth)
	cw := sur.StringWidth(ct)
	cx := lo + (cAvail-cw)/2
	// Center within the whole bar when it fits there without overlap.
	if ideal := (w - cw) / 2; ideal >= lo && ideal+cw <= hi {
		cx = ideal
	}
	drawText(sur, cx, 0, ct, s.segStyle(s.center))
}
