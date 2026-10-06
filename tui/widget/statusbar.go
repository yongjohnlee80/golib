package widget

import (
	"fmt"
	"iter"
	"slices"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Status Bar & Triple-Segment Dock Chrome Architecture
//
// StatusBar provides a fixed single-row dock footer that renders information across
// three independent, prioritized segments: Left, Center, and Right.
//
// # Subsystem Role & Responsibilities
//
//  1. Triple-Segment Distribution:
//     - Left Segment: Typically displays active mode, view title, or working branch.
//     - Center Segment: Displays ephemeral status messages, notifications, or progress summaries.
//     - Right Segment: Displays persistent help hints, keyboard shortcuts, or system metrics.
//  2. Graceful Truncation Priority:
//     When screen width contracts, segments truncate in strict prioritized order:
//     Center truncates first, followed by Left, while Right survives longest.
//     This ensures critical shortcuts and quit hints remain visible even on narrow terminals.
//  3. Non-Focusable Chrome:
//     StatusBar is pure presentation chrome. It does not accept focus or consume events.
//
// # Layout & Segment Placement Model
//
//	┌─────────────────────────────────────────────────────────────┐
//	│ [StatusBar] Single-Row Dock Footer (Height = 1)             │
//	│                                                             │
//	│  LEFT                  CENTER (Truncates First)       RIGHT │
//	│  [ NORMAL ]            [ Synced 12 items ]      [ ? Help ]  │
//	└─────────────────────────────────────────────────────────────┘
//
// # Architectural Invariants
//
//  1. Single-Line Height Invariant:
//     Layout always returns `Size{W: c.MaxW, H: 1}` (clamped to available constraints).
//  2. Right-Preservation Priority:
//     Rightward content is never truncated if sufficient width exists to display it.
//
// # Concurrency & Goroutine Ownership
//
// StatusBar is loop-goroutine-owned. Calling [StatusBar.SetLeft], [StatusBar.SetCenter],
// or [StatusBar.SetRight] must occur on the main application loop goroutine.
//
// # Usage Examples
//
// 1. Setting up an application status footer:
//
//	bar := widget.NewStatusBar()
//	bar.SetLeft("main*")
//	bar.SetCenter("Ready")
//	bar.SetRight("q: Quit | ?: Help", style.New().Bold(true))
type StatusBar struct {
	Base
	bar                 style.Style
	left, center, right segment
	// items are the bar's child widgets, in order: Qt's QStatusBar permanent widgets
	// (addPermanentWidget), each at its own width at the bar's right end, and its normal widgets
	// (addWidget), at its left end. The segments share what is left of the row between them.
	items []barItem
	// the columns the widgets took at the last layout, gaps included: the normal ones' at the
	// left, the permanent ones' at the right
	leading, placed int
}

// barItem is one of a StatusBar's widgets, and which end it sits at.
type barItem struct {
	c      tui.Component
	normal bool // Qt's addWidget: at the left end; else addPermanentWidget, at the right
}

var _ tui.Component = (*StatusBar)(nil)

type segment struct {
	text string
	// msg is the catalog message the segment shows, resolved each layout; zero for plain text.
	msg tui.Message
	st  style.Style
	set bool
}

// StatusBarOption customizes a StatusBar under construction.
type StatusBarOption func(*StatusBar)

// WithBarStyle replaces the bar's base style (default background
// style.TokenPanel).
func WithBarStyle(st style.Style) StatusBarOption {
	return func(s *StatusBar) { s.bar = st }
}

// defaultBarStyle is a StatusBar's look before any option.
func defaultBarStyle() style.Style {
	return style.New().Background(style.TokenPanel).Foreground(style.TokenForeground)
}

// NewStatusBar builds an empty status bar.
func NewStatusBar(opts ...StatusBarOption) *StatusBar {
	s := &StatusBar{bar: defaultBarStyle()}
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
	seg.text, seg.msg = text, tui.Message{}
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

// SetLeftMessage shows a catalog message in the left segment, in the App's language from the
// next layout (optional per-segment style). [StatusBar.SetLeft] ends it.
func (s *StatusBar) SetLeftMessage(m tui.Message, st ...style.Style) { s.setMessage(&s.left, m, st) }

// SetCenterMessage shows a catalog message in the center segment, as
// [StatusBar.SetLeftMessage] does.
func (s *StatusBar) SetCenterMessage(m tui.Message, st ...style.Style) {
	s.setMessage(&s.center, m, st)
}

// SetRightMessage shows a catalog message in the right segment, as
// [StatusBar.SetLeftMessage] does.
func (s *StatusBar) SetRightMessage(m tui.Message, st ...style.Style) { s.setMessage(&s.right, m, st) }

func (s *StatusBar) setMessage(seg *segment, m tui.Message, st []style.Style) {
	setSegment(seg, s.translate(m), st)
	seg.msg = m
	s.RequestLayout()
}

// Init mounts the widgets. Re-entrant across remounts.
func (s *StatusBar) Init(ctx *tui.Context) {
	s.Base.Init(ctx)
	for _, it := range s.items {
		ctx.Mount(it.c)
	}
}

// Add appends permanent widgets (Container contract): Qt's QStatusBar.addPermanentWidget. Each
// is laid out one row high at the width it asks for, at the bar's right end in the order added.
func (s *StatusBar) Add(children ...tui.Component) { s.add(false, children) }

// AddWidget appends normal widgets: Qt's QStatusBar.addWidget. Each is laid out one row high at
// the width it asks for, at the bar's left end in the order added, before the left segment.
func (s *StatusBar) AddWidget(children ...tui.Component) { s.add(true, children) }

func (s *StatusBar) add(normal bool, children []tui.Component) {
	for _, c := range children {
		if c == nil {
			panic("widget: StatusBar.Add: nil child")
		}
		s.items = append(s.items, barItem{c: c, normal: normal})
		if s.ctx != nil {
			s.ctx.Mount(c)
			s.RequestLayout()
		}
	}
}

func (s *StatusBar) indexOf(child tui.Component) int {
	return slices.IndexFunc(s.items, func(it barItem) bool { return it.c == child })
}

// Remove unmounts a widget and forgets it.
func (s *StatusBar) Remove(child tui.Component) {
	i := s.indexOf(child)
	if child == nil || i < 0 {
		return
	}
	s.items = slices.Delete(s.items, i, i+1)
	if s.ctx != nil {
		s.ctx.Unmount(child)
		s.RequestLayout()
	}
}

// Move reorders a widget among all of the bar's, without unmounting it (Container contract); it
// keeps its end.
func (s *StatusBar) Move(child tui.Component, to int) {
	i := s.indexOf(child)
	if child == nil || i < 0 {
		return
	}
	if to < 0 || to >= len(s.items) {
		panic(errs.Fatal{Op: "widget: StatusBar.Move", Rule: fmt.Sprintf("index %d outside the %d widgets", to, len(s.items))})
	}
	it := s.items[i]
	s.items = slices.Insert(slices.Delete(s.items, i, i+1), to, it)
	s.RequestLayout()
}

// Children enumerates the widgets, normal and permanent, in the order added.
func (s *StatusBar) Children() iter.Seq[tui.Component] {
	return func(yield func(tui.Component) bool) {
		for _, it := range s.items {
			if !yield(it.c) {
				return
			}
		}
	}
}

// Layout is height 1, width greedy. The permanent widgets are laid out from the right end, a
// column apart, each offered the columns still free and taking the width it asks for within them
// (a Text truncates); one offered none is given none. The normal widgets are laid out the same
// way from the left end, in what the permanent ones left.
func (s *StatusBar) Layout(c tui.Constraints) tui.Size {
	for _, seg := range []*segment{&s.left, &s.center, &s.right} {
		if seg.msg != (tui.Message{}) {
			seg.text = s.translate(seg.msg)
		}
	}
	sz := c.Constrain(tui.Size{W: boundedMax(c.MaxW, c.MinW), H: 1})
	// right to left: the last added sits at the end
	x := sz.W
	for i := len(s.items) - 1; i >= 0; i-- {
		if s.items[i].normal {
			continue
		}
		child := s.items[i].c
		var w int
		if x > 0 {
			w = s.ctx.LayoutChild(child, tui.Constraints{MaxW: x, MaxH: 1}).W
		}
		if w > 0 && x-w >= 0 {
			x -= w
			s.ctx.PlaceChild(child, tui.Rect{X: x, W: w, H: 1})
			x-- // a column before the next
			continue
		}
		s.ctx.LayoutChild(child, tui.Tight(tui.Size{}))
		s.ctx.PlaceChild(child, tui.Rect{X: max(x, 0)})
	}
	s.placed = sz.W - max(x, 0)
	// left to right, in what is left: the first added sits at the start
	lx, end := 0, max(x, 0)
	for _, it := range s.items {
		if !it.normal {
			continue
		}
		var w int
		if free := end - lx; free > 0 {
			w = s.ctx.LayoutChild(it.c, tui.Constraints{MaxW: free, MaxH: 1}).W
		}
		if w > 0 && lx+w <= end {
			s.ctx.PlaceChild(it.c, tui.Rect{X: lx, W: w, H: 1})
			lx += w + 1 // a column after it
			continue
		}
		s.ctx.LayoutChild(it.c, tui.Tight(tui.Size{}))
		s.ctx.PlaceChild(it.c, tui.Rect{X: min(lx, end)})
	}
	s.leading = min(lx, end)
	return sz
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
	full := sur.Size().W
	if full <= 0 {
		return
	}
	sur.Fill(tui.Rect{X: 0, Y: 0, W: full, H: 1}, " ", s.bar)
	// the segments share the row between the normal widgets and the permanent ones
	sur = sur.Sub(tui.Rect{X: s.leading, W: max(full-s.placed-s.leading, 0), H: 1})
	w := sur.Size().W
	if w <= 0 {
		return
	}

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
