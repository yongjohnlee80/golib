package widget

import (
	"fmt"

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
