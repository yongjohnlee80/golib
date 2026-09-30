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
	// permanent are the bar's child widgets, Qt's QStatusBar permanent widgets: each at its own
	// width, in order, at the bar's right end, so the segments share what is left of the row.
	permanent []tui.Component
	placed    int // the columns the permanent widgets took at the last layout, gaps included
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

// Init mounts the permanent widgets. Re-entrant across remounts.
func (s *StatusBar) Init(ctx *tui.Context) {
	s.Base.Init(ctx)
	for _, c := range s.permanent {
		ctx.Mount(c)
	}
}

// Add appends permanent widgets (Container contract): Qt's QStatusBar.addPermanentWidget. Each
// is laid out one row high at the width it asks for, at the bar's right end in the order added.
func (s *StatusBar) Add(children ...tui.Component) {
	for _, c := range children {
		if c == nil {
			panic("widget: StatusBar.Add: nil child")
		}
		s.permanent = append(s.permanent, c)
		if s.ctx != nil {
			s.ctx.Mount(c)
			s.RequestLayout()
		}
	}
}

// Remove unmounts a permanent widget and forgets it.
func (s *StatusBar) Remove(child tui.Component) {
	i := slices.Index(s.permanent, child)
	if child == nil || i < 0 {
		return
	}
	s.permanent = slices.Delete(s.permanent, i, i+1)
	if s.ctx != nil {
		s.ctx.Unmount(child)
		s.RequestLayout()
	}
}

// Move reorders a permanent widget without unmounting it (Container contract).
func (s *StatusBar) Move(child tui.Component, to int) {
	i := slices.Index(s.permanent, child)
	if child == nil || i < 0 {
		return
	}
	if to < 0 || to >= len(s.permanent) {
		panic(errs.Fatal{Op: "widget: StatusBar.Move", Rule: fmt.Sprintf("index %d outside the %d permanent widgets", to, len(s.permanent))})
	}
	s.permanent = slices.Insert(slices.Delete(s.permanent, i, i+1), to, child)
	s.RequestLayout()
}

// Children enumerates the permanent widgets.
func (s *StatusBar) Children() iter.Seq[tui.Component] { return slices.Values(s.permanent) }

// Layout is height 1, width greedy. The permanent widgets are laid out at the width each asks
// for, a column apart, from the right end; one that does not fit whole is given no columns.
func (s *StatusBar) Layout(c tui.Constraints) tui.Size {
	sz := c.Constrain(tui.Size{W: boundedMax(c.MaxW, c.MinW), H: 1})
	widths := make([]int, len(s.permanent))
	for i, child := range s.permanent {
		widths[i] = s.ctx.LayoutChild(child, tui.Constraints{MaxW: sz.W, MaxH: 1}).W
	}
	// right to left: the last added sits at the end
	x, fits := sz.W, true
	for i := len(s.permanent) - 1; i >= 0; i-- {
		w := widths[i]
		if fits && w > 0 && x-w >= 0 {
			x -= w
			s.ctx.PlaceChild(s.permanent[i], tui.Rect{X: x, W: w, H: 1})
			x-- // a column before the next
			continue
		}
		fits = fits && w == 0
		s.ctx.LayoutChild(s.permanent[i], tui.Tight(tui.Size{}))
		s.ctx.PlaceChild(s.permanent[i], tui.Rect{X: max(x, 0)})
	}
	s.placed = sz.W - max(x, 0)
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
	// the segments share the row left of the permanent widgets
	w := full - s.placed
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
