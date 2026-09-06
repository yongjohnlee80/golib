package widget

import (
	"fmt"
	"math"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Orientation selects a Split's main axis.
type Orientation uint8

const (
	// Horizontal places the panes side by side (vertical divider).
	Horizontal Orientation = iota
	// Vertical stacks the panes (horizontal divider).
	Vertical
)

// Split is the two-pane interactive splitter: a one-cell
// divider whose position follows a ratio, adjustable by Alt+arrows when
// either pane holds focus (the lazygit resize precedent, generalized) and
// by mouse drag on the divider. Min sizes clamp; the integer division is
// deterministic (same ratio, same cells — every run, every platform).
//
// Split is not itself focusable; its panes are ordinary members of the
// focus chain.
type Split struct {
	Base
	o          Orientation
	a, b       tui.Component
	ratio      float64
	minA, minB int

	avail    int // last main-axis cells available to panes (minus divider)
	aCells   int // last main-axis cells given to pane a
	dragging bool
	zoomed   SplitPane

	divider style.Style
}

var _ tui.Component = (*Split)(nil)

// SplitOption customizes a Split under construction.
type SplitOption func(*Split)

// WithRatio sets the initial division (0 < r < 1; default 0.5).
func WithRatio(r float64) SplitOption {
	if r <= 0 || r >= 1 || math.IsNaN(r) {
		panic(fmt.Sprintf("widget: WithRatio: ratio %v outside (0, 1)", r))
	}
	return func(s *Split) { s.ratio = r }
}

// WithMinSizes clamps each pane's main-axis extent during resize.
func WithMinSizes(a, b int) SplitOption {
	if a < 0 || b < 0 {
		panic("widget: WithMinSizes: negative minimum")
	}
	return func(s *Split) { s.minA, s.minB = a, b }
}

// WithDividerStyle replaces the divider line style (default
// style.TokenBorder foreground).
func WithDividerStyle(st style.Style) SplitOption {
	return func(s *Split) { s.divider = st }
}

// NewSplit builds a splitter around two panes.
func NewSplit(o Orientation, a, b tui.Component, opts ...SplitOption) *Split {
	if o > Vertical {
		panic("widget: NewSplit: invalid orientation")
	}
	if a == nil || b == nil {
		panic("widget: NewSplit: nil pane")
	}
	s := &Split{
		o: o, a: a, b: b,
		ratio:   0.5,
		divider: style.New().Foreground(style.TokenBorder),
	}
	for _, o := range opts {
		if o != nil {
			o(s)
		}
	}
	return s
}

// Ratio returns the current division.
func (s *Split) Ratio() float64 { return s.ratio }

// SetRatio moves the divider programmatically (0 < r < 1; clamped by the
// min sizes at layout). The exported sibling of the drag/Alt-arrow path.
func (s *Split) SetRatio(r float64) {
	if r <= 0 || r >= 1 || math.IsNaN(r) {
		panic(errs.Fatal{Op: "widget: SetRatio", Rule: fmt.Sprintf("ratio %v outside (0, 1)", r)})
	}
	if r == s.ratio {
		return
	}
	s.ratio = r
	s.RequestLayout()
	s.MarkDirty()
	s.publish(SplitResizedEvent{Owner: s.NodeID(), Ratio: s.ratio})
}

// SplitPane identifies a Split pane for Zoom.
type SplitPane uint8

const (
	// PaneNone restores the two-pane layout.
	PaneNone SplitPane = iota
	// PaneA zooms the first pane.
	PaneA
	// PaneB zooms the second pane.
	PaneB
)

// SplitZoomEvent is published on every Zoom transition.
type SplitZoomEvent struct {
	Owner tui.NodeID
	Pane  SplitPane
}

// Zoomed reports the current zoom state.
func (s *Split) Zoomed() SplitPane { return s.zoomed }

// Zoom gives one pane the full rect: the other pane is not
// laid out, rendered, hit-tested, or focusable, and the divider disappears
// (divider interaction is inert while zoomed and any drag is cancelled).
// If focus currently lives inside the pane being hidden, it transfers to
// the first focusable in the retained pane (the walk honors nested zoom);
// when the retained pane has no focusable, focus is deliberately LEFT IN
// PLACE — the hidden pane is on screen until the zoom's layout renders,
// and the post-layout repair then re-homes trap-aware against fresh
// visibility. PaneNone restores the prior ratio without moving focus.
func (s *Split) Zoom(p SplitPane) {
	if p > PaneB {
		panic(errs.Fatal{Op: "widget: Zoom", Rule: "invalid pane"})
	}
	if s.zoomed == p {
		return
	}
	s.zoomed = p
	s.dragging = false // a divider drag cannot survive the divider vanishing
	if ctx := s.Context(); ctx != nil && p != PaneNone {
		hidden, kept := s.b, s.a
		if p == PaneB {
			hidden, kept = s.a, s.b
		}
		if ctx.FocusWithin(hidden) {
			// listChildren honors nested zoom, so this walk cannot land in
			// a logically hidden pane. When the retained pane has no
			// focusable, focus is deliberately LEFT IN PLACE: the pane is
			// still on screen until the zoom's layout runs, and that layout
			// pass re-homes trap-aware via repairInvisibleFocus against
			// FRESH visibility — repairing here against the stale ring
			// could land focus straight back on the hidden pane.
			_ = focusFirst(kept)
		}
	}
	s.RequestLayout()
	s.MarkDirty()
	s.publish(SplitZoomEvent{Owner: s.NodeID(), Pane: p})
}

// listChildren feeds the package's focus walk, honoring zoom: a hidden
// pane is not a focus target (MF7 — a nested zoomed Split must not let
// focusFirst wander into its logically hidden side).
func (s *Split) listChildren() []tui.Component {
	switch s.zoomed {
	case PaneA:
		return []tui.Component{s.a}
	case PaneB:
		return []tui.Component{s.b}
	}
	return []tui.Component{s.a, s.b}
}

// Init mounts both panes. Re-entrant across remounts.
func (s *Split) Init(ctx *tui.Context) {
	s.Base.Init(ctx)
	s.dragging = false
	ctx.Mount(s.a)
	ctx.Mount(s.b)
}

// Layout divides the main axis: pane a gets round(ratio·avail) cells
// (deterministic half-up rounding — the two-child largest-remainder split),
// clamped by the min sizes; the divider takes one cell; pane b the rest.
func (s *Split) Layout(c tui.Constraints) tui.Size {
	w := boundedMax(c.MaxW, c.MinW)
	h := boundedMax(c.MaxH, c.MinH)
	if s.zoomed != PaneNone {
		full := s.a
		if s.zoomed == PaneB {
			full = s.b
		}
		s.ctx.LayoutChild(full, tui.Tight(tui.Size{W: w, H: h}))
		s.ctx.PlaceChild(full, tui.Rect{X: 0, Y: 0, W: w, H: h})
		s.avail = 0 // suppresses the divider (Render) and resize (setCells)
		return c.Constrain(tui.Size{W: w, H: h})
	}
	horiz := s.o == Horizontal
	main, cross := w, h
	if !horiz {
		main, cross = h, w
	}
	avail := max(main-1, 0)
	a := int(math.Floor(s.ratio*float64(avail) + 0.5))
	a = min(max(a, s.minA), max(avail-s.minB, 0))
	a = max(0, min(a, avail))
	b := avail - a
	s.avail, s.aCells = avail, a

	if horiz {
		s.ctx.LayoutChild(s.a, tui.Tight(tui.Size{W: a, H: cross}))
		s.ctx.PlaceChild(s.a, tui.Rect{X: 0, Y: 0, W: a, H: cross})
		s.ctx.LayoutChild(s.b, tui.Tight(tui.Size{W: b, H: cross}))
		s.ctx.PlaceChild(s.b, tui.Rect{X: a + 1, Y: 0, W: b, H: cross})
	} else {
		s.ctx.LayoutChild(s.a, tui.Tight(tui.Size{W: cross, H: a}))
		s.ctx.PlaceChild(s.a, tui.Rect{X: 0, Y: 0, W: cross, H: a})
		s.ctx.LayoutChild(s.b, tui.Tight(tui.Size{W: cross, H: b}))
		s.ctx.PlaceChild(s.b, tui.Rect{X: 0, Y: a + 1, W: cross, H: b})
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

// Render paints the divider line.
func (s *Split) Render(sur tui.Surface) {
	sz := sur.Size()
	if s.avail <= 0 {
		return
	}
	if s.o == Horizontal {
		sur.Fill(tui.Rect{X: s.aCells, Y: 0, W: 1, H: sz.H}, "│", s.divider)
	} else {
		sur.Fill(tui.Rect{X: 0, Y: s.aCells, W: sz.W, H: 1}, "─", s.divider)
	}
}

// setCells moves the divider to give pane a cells on the main axis,
// re-deriving the ratio and emitting SplitResizedEvent.
func (s *Split) setCells(a int) {
	if s.avail <= 0 {
		return
	}
	a = min(max(a, s.minA), max(s.avail-s.minB, 0))
	a = max(0, min(a, s.avail))
	if a == s.aCells {
		return
	}
	s.ratio = float64(a) / float64(s.avail)
	s.RequestLayout()
	s.MarkDirty()
	s.publish(SplitResizedEvent{Owner: s.NodeID(), Ratio: s.ratio})
}

// HandleEvent implements keyboard resize (Alt+arrows bubbling up from a
// focused pane) and divider drag (SGR mouse).
func (s *Split) HandleEvent(ev tui.Event) bool {
	if s.zoomed != PaneNone {
		return false // no divider: resize keys and drag are inert (S4)
	}
	switch e := ev.(type) {
	case tui.KeyEvent:
		if e.Kind == tui.KeyRelease || e.Mods&tui.ModAlt == 0 || e.Mods&^(tui.ModAlt) != 0 {
			return false
		}
		horiz := s.o == Horizontal
		switch e.Code {
		case tui.KeyLeft:
			if horiz {
				s.setCells(s.aCells - 1)
				return true
			}
		case tui.KeyRight:
			if horiz {
				s.setCells(s.aCells + 1)
				return true
			}
		case tui.KeyUp:
			if !horiz {
				s.setCells(s.aCells - 1)
				return true
			}
		case tui.KeyDown:
			if !horiz {
				s.setCells(s.aCells + 1)
				return true
			}
		}
		return false

	case tui.MouseEvent:
		pos := e.X
		if s.o == Vertical {
			pos = e.Y
		}
		switch {
		case e.Kind == tui.MousePress && e.Button == tui.MouseLeft && pos == s.aCells:
			s.dragging = true
			return true
		case e.Kind == tui.MouseMotion && s.dragging:
			s.setCells(pos)
			return true
		case e.Kind == tui.MouseRelease && s.dragging:
			s.dragging = false
			s.setCells(pos)
			return true
		}
		return false
	}
	return false
}
