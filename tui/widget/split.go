package widget

import (
	"fmt"
	"math"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
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

// Split is an interactive two-pane divider container supporting both horizontal
// (side-by-side) and vertical (stacked) arrangements.
//
// # Divider Geometry & Layout Model
//
// Split separates its two child components with a 1-cell divider line:
//
//	Horizontal Orientation:              Vertical Orientation:
//	┌─────────────┬───┬─────────────┐   ┌─────────────────────────────┐
//	│             │ │ │             │   │           Pane A            │
//	│   Pane A    │ │ │   Pane B    │   ├─────────────────────────────┤ ◄── Divider
//	│             │ │ │             │   │           Pane B            │
//	└─────────────┴───┴─────────────┘   └─────────────────────────────┘
//	                ▲
//	                └── Divider (1 cell)
//
// Available space along the main axis is (Total - 1). The division follows [Split.Ratio]
// (0 < r < 1; default 0.5), clamped by [WithMinSizes]. Integer cell division is strictly
// deterministic: the exact same ratio produces identical cell allocations on every run
// and across all operating systems.
//
// # Keyboard and Mouse Interaction
//
//  1. Keyboard Resizing: when focus is anywhere inside the split, Alt+Left/Right
//     (horizontal) or Alt+Up/Down (vertical) moves the divider by ONE CELL per
//     keystroke — [WithSplitResizeStep] changes the amount and the unit. This
//     generalizes the lazygit pane-resizing precedent across all split layouts.
//     The arrows work along the split's OWN axis only: Alt+Left on a vertical
//     split is a different gesture, not a smaller step, and consuming it would
//     swallow a binding the application may want.
//  2. Mouse Dragging: Clicking and dragging the 1-cell divider line repositions the split
//     interactively in real time.
//  3. Resized Notification: a divider that MOVES publishes [SplitResizedEvent]
//     from the commit phase, carrying the effective ratio and both pane extents
//     in cells. Motion that changes no cells publishes nothing.
//
// Both the divider and [Resizable] speak ONE action vocabulary —
// [ResizeBeginAction], [ResizeUpdateAction], [ResizeStepAction],
// [ResizeSetAction], [ResizeEndAction] and [ResizeCancelAction] — so a consumer
// binding a key to a resize action need not know which widget will answer it.
// A Split selects the divider handle for its own axis
// ([HandleVerticalDivider] for a horizontal split) and ignores the other axis.
//
// # Zooming / Maximizing Panes
//
// Split supports maximizing a single pane to occupy 100% of the container:
//   - [Split.Zoom](PaneA) or [Split.Zoom](PaneB) hides the other pane and the divider.
//   - [Split.Unzoom]() restores the dual-pane view and previous ratio.
//   - Transitions publish [SplitZoomEvent].
//
// # Focus Model
//
// Split itself is not focusable ([tui.Component] without [tui.Focusable]). Its child panes
// participate directly in normal focus traversal.
//
// # Architectural Invariants
//
//  1. Pure Deterministic Division:
//     Available space along the primary axis is strictly (Total - 1). Cell boundary
//     calculations are rounded deterministically, ensuring that resizing back and forth
//     does not experience creeping arithmetic drift.
//  2. Minimum Bounds Clamping:
//     Configured minimum pane dimensions ([WithMinSizes]) are unconditionally honored
//     during both keyboard adjustment and mouse dragging. Neither pane can be shrunk below
//     its minimum constraint unless the container's total size itself is smaller.
//  3. Event Publishing Contract:
//     Divider shifts publish [SplitResizedEvent], while zoom/unzoom actions publish
//     [SplitZoomEvent], stamped with the Split's [tui.NodeID].
//
// # Concurrency & Goroutine Ownership
//
// Split and its state methods ([Split.SetRatio], [Split.Zoom], [Split.Unzoom]) are
// loop-goroutine-owned and must be driven from the application loop goroutine.
//
// # Usage Examples
//
// 1. Standard side-by-side split with 50/50 ratio:
//
//	sidebar := widget.NewBox(treeView, widget.WithTitle("Explorer"))
//	editor := widget.NewEditor()
//	split := widget.NewSplit(
//		widget.Horizontal,
//		sidebar,
//		editor,
//		widget.WithRatio(0.3),
//		widget.WithMinSizes(15, 30),
//	)
//
// 2. Maximizing / Zooming a pane:
//
//	// Zoom editor to 100% full-screen view:
//	split.Zoom(widget.PaneB)
//
//	// Restore dual-pane view:
//	split.Unzoom()
type Split struct {
	Base
	o    Orientation
	a, b tui.Component
	// requested is the last explicit division asked for, unclamped, and
	// committed is the one the last COMMIT stored — what is actually on screen.
	// Two fields because they answer different questions: persistence stores
	// the request, and a reader asking what the split looks like wants the
	// other. Collapsing them loses the user's choice the first time a min size
	// or a narrow terminal clamps it.
	requested  float64
	committed  float64
	sized      bool
	minA, minB int

	// avail, aCells and bCells are the last COMMITTED geometry: the main-axis
	// cells available to the panes (the extent less the divider), and how the
	// commit divided them. Zero avail means there is no divider this frame —
	// either nothing has been laid out yet, or a pane is zoomed — and that is
	// the one condition the drag and the step both consult.
	avail  int
	aCells int
	bCells int
	zoomed SplitPane
	// drag is the divider gesture in progress, and dragCtx the node that took
	// the pointer for it — only the node that took a capture may release it.
	drag    *splitDrag
	dragCtx *tui.Context

	divider style.Style
	// glyph is the divider's character, and step/stepUnit the keyboard's
	// movement. Split-qualified option names, because the generic ones already
	// belong to Resizable and two options of the same name returning different
	// types is the ambiguity a consumer meets at the call site.
	glyphH, glyphV string
	step           int
	stepUnit       StepUnit
	pointerPolicy  tui.PointerPolicy
}

var _ tui.Component = (*Split)(nil)

// SplitOption customizes a Split under construction.
type SplitOption func(*Split)

// WithRatio sets the initial division (0 < r < 1; default 0.5).
func WithRatio(r float64) SplitOption {
	if r <= 0 || r >= 1 || math.IsNaN(r) {
		panic(fmt.Sprintf("widget: WithRatio: ratio %v outside (0, 1)", r))
	}
	return func(s *Split) { s.requested = r }
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

// WithSplitDividerGlyphs replaces the characters the divider is drawn with:
// first the VERTICAL line a horizontal split uses, then the horizontal line a
// vertical split uses.
//
// Both, in one option, because a Split has exactly one orientation but a
// consumer styling an application sets them together — and an option that took
// only the one for the current axis would silently do nothing on the other.
// Each must be a single grapheme of width one: the divider occupies one cell by
// construction, and a wide glyph in it renders nothing.
func WithSplitDividerGlyphs(vertical, horizontal string) SplitOption {
	return func(s *Split) {
		for _, g := range []string{vertical, horizontal} {
			n := 0
			for range grapheme.Clusters(g) {
				n++
			}
			if n != 1 || grapheme.StringWidth(g, false) != 1 {
				panic(fatalOf("widget: WithSplitDividerGlyphs",
					"each divider glyph must be one grapheme cluster of display width one",
					fmt.Sprintf("%q", g)))
			}
		}
		s.glyphV, s.glyphH = vertical, horizontal
	}
}

// WithSplitResizeStep sets how far one keyboard step moves the divider, and in
// what unit. Split-qualified because Resizable already owns the generic name.
func WithSplitResizeStep(n int, u StepUnit) SplitOption {
	return func(s *Split) {
		if n < 1 {
			panic(tuiFatal("widget: WithSplitResizeStep", "step must be at least 1", n))
		}
		if !u.Valid() {
			panic(tuiFatal("widget: WithSplitResizeStep",
				"value outside the declared StepUnit set", int(u)))
		}
		s.step, s.stepUnit = n, u
	}
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
		requested: 0.5,
		divider:   style.New().Foreground(style.TokenBorder),
		glyphV:    "│",
		glyphH:    "─",
		step:      1,
		stepUnit:  StepCells,
	}
	for _, o := range opts {
		if o != nil {
			o(s)
		}
	}
	return s
}

// Ratio returns the EFFECTIVE division — what is on screen, as stored by the
// last commit. Before the first layout it returns the request, because that is
// the only answer there is and reporting zero would be worse than reporting the
// intention.
func (s *Split) Ratio() float64 {
	if !s.sized {
		return s.requested
	}
	return s.committed
}

// RequestedRatio returns the last explicit request, unclamped.
//
// Persistence stores THIS. A split clamped to a min size on a narrow terminal
// must not persist the clamped value, or every restore on a narrower screen
// walks the divider a little further each time.
func (s *Split) RequestedRatio() float64 { return s.requested }

// Cells reports the effective main-axis cells each pane holds, and whether a
// layout has committed yet. The integer form of Ratio, for a caller that would
// otherwise re-derive it and disagree by one.
func (s *Split) Cells() (a, b int, ok bool) { return s.aCells, s.bCells, s.sized }

// SetRatio records a requested division.
//
// It CLAMPS NOTHING, requests layout, and publishes NOTHING synchronously. The
// effective ratio is whatever the next layout reaches against the min sizes and
// the cells actually available, and the commit that follows reports it — so a
// caller cannot observe a ratio the screen does not have.
func (s *Split) SetRatio(r float64) {
	if r <= 0 || r >= 1 || math.IsNaN(r) {
		panic(errs.Fatal{Op: "widget: SetRatio", Rule: fmt.Sprintf("ratio %v outside (0, 1)", r)})
	}
	if r == s.requested {
		return
	}
	s.requested = r
	s.RequestLayout()
	s.MarkDirty()
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

// WithPointerPolicy sets whether this split and its subtree accept pointer
// input, and returns the split for chaining.
//
// Remembered as well as applied, because NewSplit(...).WithPointerPolicy(...) is
// the natural way to write it and runs before there is any Context. Keyboard
// operation is unaffected: every action the divider resolves is reachable from
// the keyboard, so a pointer-disabled split stays fully usable.
func (s *Split) WithPointerPolicy(p tui.PointerPolicy) *Split {
	if !p.Valid() {
		panic(tuiFatal("widget: Split.WithPointerPolicy",
			"value outside PointerInherit, PointerEnabled, PointerDisabled", int(p)))
	}
	s.pointerPolicy = p
	if ctx := s.Context(); ctx != nil {
		ctx.SetPointerPolicy(p)
	}
	return s
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
	s.endDrag() // a divider drag cannot survive the divider vanishing
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
// pane is not a focus target, so a nested zoomed Split cannot let focusFirst
// wander into its logically hidden side.
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
	ctx.SetPointerPolicy(s.pointerPolicy)
	s.drag, s.dragCtx = nil, nil
	ctx.SetDefaultActionResolvers(tui.ActionResolverFunc(s.resolve))
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
		// Through the commit, not from here, for the same reason as the division
		// below: an assignment in Layout is a geometry-derived side effect in a
		// phase that must not have one. Zero available cells is what suppresses
		// the divider in Render and refuses the drag.
		s.ctx.AfterLayout(splitCommitKey, s.commitZoomed)
		return c.Constrain(tui.Size{W: w, H: h})
	}
	horiz := s.o == Horizontal
	main, cross := w, h
	if !horiz {
		main, cross = h, w
	}
	avail := max(main-1, 0)
	a := int(math.Floor(s.requested*float64(avail) + 0.5))
	a = min(max(a, s.minA), max(avail-s.minB, 0))
	a = max(0, min(a, avail))
	b := avail - a
	// Layout computes; the COMMIT stores and publishes. Keeping the assignment
	// here would put a geometry-derived side effect in a pure phase, which is
	// the rule the commit phase exists to make keepable rather than aspirational.
	s.ctx.AfterLayout(splitCommitKey, func() { s.commit(avail, a, b) })

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
		sur.Fill(tui.Rect{X: s.aCells, Y: 0, W: 1, H: sz.H}, s.glyphV, s.divider)
	} else {
		sur.Fill(tui.Rect{X: 0, Y: s.aCells, W: sz.W, H: 1}, s.glyphH, s.divider)
	}
}

// splitCommitKey identifies the Split's commit record.
const splitCommitKey tui.CommitKey = "widget.split"

// commit stores the effective division and publishes the change.
//
// No event on the FIRST layout — there was no previous division to differ from,
// and a listener counting resizes should not see one for the split appearing —
// and none when the cells did not move, however far the pointer did.
func (s *Split) commit(avail, a, b int) {
	s.avail = avail
	eff := 0.0
	if avail > 0 {
		eff = float64(a) / float64(avail)
	}
	if s.sized && s.aCells == a && s.bCells == b {
		return
	}
	first := !s.sized
	s.aCells, s.bCells, s.committed, s.sized = a, b, eff, true
	if first {
		return
	}
	s.publish(SplitResizedEvent{Owner: s.NodeID(), Ratio: eff, ACells: a, BCells: b})
}

// commitZoomed records that there is no divider this frame.
//
// The DIVISION is deliberately left alone: a zoom hides the divider, it does not
// move it, and unzooming must put the panes back where the user left them. Only
// the availability changes, and that is what makes the divider inert rather than
// each of its entry points having to ask about zoom separately.
func (s *Split) commitZoomed() { s.avail = 0 }

// requestCells asks for a division expressed in main-axis cells.
//
// It converts to a ratio and goes through the same request path as SetRatio, so
// the drag, the keyboard and the programmatic setter cannot reach three
// different clamping rules.
func (s *Split) requestCells(a int) bool {
	if s.avail <= 0 {
		return false
	}
	a = max(0, min(a, s.avail))
	r := float64(a) / float64(s.avail)
	// The endpoints are not expressible as a ratio in (0,1); a divider dragged
	// to the very edge means "as far as it goes", which the min-size clamp in
	// Layout already expresses.
	r = math.Max(math.Min(r, 0.999), 0.001)
	if r == s.requested {
		return false
	}
	s.requested = r
	s.RequestLayout()
	s.MarkDirty()
	return true
}

// THE DIVIDER RUNS ON CAPTURE AND ACTIONS.
//
// It used to read raw mouse events and track a dragging flag, which meant the
// drag ended wherever the pointer happened to still be over the Split — leave
// its rect and the motion went elsewhere, so the divider stopped following and
// the release was never seen. A capture fixes that, and the named actions mean
// the keyboard and the pointer reach one implementation rather than two.
//
// A divider REDISTRIBUTES ONE SHARED EXTENT between two panes; Resizable SIZES
// ONE BOX. They are different operations, which is why Split keeps its own
// divider rather than being expressed as a wrapper.

// splitDrag is the state one divider gesture needs.
type splitDrag struct {
	// beginRequested is restored on cancel: the REQUEST, not the effective
	// ratio, because cancelling must put back what the user had asked for
	// rather than what a clamp had made of it.
	beginRequested float64
}

// dividerHandle is the handle this split's divider IS: a horizontal split puts
// its panes side by side, so the line between them runs down the screen.
func (s *Split) dividerHandle() Handle {
	if s.o == Horizontal {
		return HandleVerticalDivider
	}
	return HandleHorizontalDivider
}

// resolve maps the divider's input onto the SHARED resize vocabulary.
//
// One vocabulary for both resizing widgets: a consumer binding a key to
// ResizeStepAction gets a divider step here and a box step on a Resizable,
// without having to know which widget will answer. The parallel split.* family
// this replaced meant the same intent had two names and two implementations.
func (s *Split) resolve(ev tui.Event) (tui.Action, bool) {
	if s.zoomed != PaneNone {
		return nil, false // no divider: resize keys and drag are inert
	}
	switch e := ev.(type) {
	case tui.KeyEvent:
		return s.resolveKey(e)
	case tui.MouseEvent:
		return s.resolveMouse(e)
	}
	return nil, false
}

// resolveKey binds Alt-arrows along the split's own axis, and Escape to cancel.
//
// Along the AXIS only: Alt-Left on a vertical split is not a smaller step, it is
// a different gesture entirely, and consuming it would swallow a binding the
// application may want.
func (s *Split) resolveKey(e tui.KeyEvent) (tui.Action, bool) {
	if e.Kind == tui.KeyRelease {
		return nil, false
	}
	if e.Code == tui.KeyEscape && e.Mods == 0 {
		return ResizeCancelAction{}, true
	}
	if e.Mods&tui.ModAlt == 0 || e.Mods&^tui.ModAlt != 0 {
		return nil, false
	}
	horiz := s.o == Horizontal
	step := func(d int) (tui.Action, bool) {
		if horiz {
			return ResizeStepAction{DX: d, Unit: s.stepUnit}, true
		}
		return ResizeStepAction{DY: d, Unit: s.stepUnit}, true
	}
	switch e.Code {
	case tui.KeyLeft:
		if horiz {
			return step(-1)
		}
	case tui.KeyRight:
		if horiz {
			return step(1)
		}
	case tui.KeyUp:
		if !horiz {
			return step(-1)
		}
	case tui.KeyDown:
		if !horiz {
			return step(1)
		}
	}
	return nil, false
}

// resolveMouse maps a press on the divider, and everything after it, onto the
// gesture vocabulary.
func (s *Split) resolveMouse(e tui.MouseEvent) (tui.Action, bool) {
	if e.Button != tui.MouseLeft && e.Kind != tui.MouseMotion {
		return nil, false // a non-primary release does not end a primary drag
	}
	at := tui.Point{X: e.X, Y: e.Y}
	pos := e.X
	if s.o == Vertical {
		pos = e.Y
	}
	switch e.Kind {
	case tui.MousePress:
		if pos != s.aCells {
			return nil, false // not on the divider
		}
		return ResizeBeginAction{Handle: s.dividerHandle(), At: at}, true
	case tui.MouseMotion:
		if s.drag == nil {
			return nil, false
		}
		return ResizeUpdateAction{At: at}, true
	case tui.MouseRelease:
		if s.drag == nil {
			return nil, false
		}
		return ResizeEndAction{}, true
	}
	return nil, false
}

// mainAxis projects a point onto the axis the divider moves along.
func (s *Split) mainAxis(p tui.Point) int {
	if s.o == Vertical {
		return p.Y
	}
	return p.X
}

// HandleAction interprets the shared resize vocabulary for the divider.
//
// Every case validates before it mutates, for the same reason the wrapper's
// does: an action is public input, and a refused one must leave no gesture
// state behind for a later Update to act on.
func (s *Split) HandleAction(inv tui.ActionInvocation) bool {
	switch a := inv.Action.(type) {
	case ResizeBeginAction:
		return s.beginDrag(a.Handle)
	case ResizeUpdateAction:
		if s.drag == nil {
			return false
		}
		s.requestCells(s.mainAxis(a.At))
		return true
	case ResizeStepAction:
		return s.stepBy(a)
	case ResizeSetAction:
		return s.setFromAction(a.Size)
	case ResizeEndAction:
		if s.drag == nil {
			return false
		}
		s.endDrag()
		return true
	case ResizeCancelAction:
		return s.cancelDrag()
	}
	return false
}

// stepBy nudges the divider along its own axis.
//
// The off-axis component is IGNORED rather than refused: one vocabulary serves
// both widgets, so a consumer's "grow by one" binding carries both axes and a
// split simply has nothing to do with the one it does not have.
func (s *Split) stepBy(a ResizeStepAction) bool {
	if !a.Unit.Valid() || s.zoomed != PaneNone || s.avail <= 0 {
		return false
	}
	d := a.DX
	if s.o == Vertical {
		d = a.DY
	}
	if d == 0 {
		return false
	}
	return s.requestCells(s.aCells + d*s.stepCells(a.Unit))
}

// stepCells is how many cells one step moves, in the unit asked for.
func (s *Split) stepCells(unit StepUnit) int {
	if unit == StepPercent {
		// At least one cell, for the same reason the wrapper rounds up: a
		// percentage of a narrow split rounds to zero, and a keypress that
		// provably cannot move anything is worse than a slow one.
		return max(s.avail*s.step/100, 1)
	}
	return s.step
}

// setFromAction places the divider at an absolute main-axis size, routed
// through the same request path as everything else.
func (s *Split) setFromAction(sz tui.Size) bool {
	if s.zoomed != PaneNone || s.avail <= 0 {
		return false
	}
	n := sz.W
	if s.o == Vertical {
		n = sz.H
	}
	return s.requestCells(n)
}

// beginDrag records the request to restore on cancel and takes the pointer, so
// motion and the release keep arriving even once the pointer has left the
// Split's own rect.
func (s *Split) beginDrag(h Handle) bool {
	// The handle must be THIS split's divider. A box handle, the other axis's
	// divider, or a value outside the declared set is refused with no drag
	// stored — so the Update and End that follow are inert too, rather than
	// moving a divider the gesture never legitimately grabbed.
	if h != s.dividerHandle() {
		return false
	}
	if s.zoomed != PaneNone || s.avail <= 0 {
		return false
	}
	s.drag = &splitDrag{beginRequested: s.requested}
	if ctx := s.Context(); ctx != nil {
		ctx.CapturePointer()
		s.dragCtx = ctx
	}
	return true
}

// endDrag finishes at the current division and releases the pointer.
func (s *Split) endDrag() bool {
	if s.drag == nil {
		return false
	}
	s.drag = nil
	if s.dragCtx != nil {
		s.dragCtx.ReleasePointer()
		s.dragCtx = nil
	}
	return true
}

// cancelDrag restores the division asked for when the drag began.
func (s *Split) cancelDrag() bool {
	d := s.drag
	if d == nil {
		return false
	}
	s.endDrag()
	s.requested = d.beginRequested
	s.RequestLayout()
	s.MarkDirty()
	return true
}

// HandleEvent cleans up when the runtime revokes the capture. The division
// reached so far STANDS: a revoked capture is not a cancellation, and silently
// reverting the user's drag would be a change they never made.
func (s *Split) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.PointerCaptureLostEvent); ok {
		s.drag, s.dragCtx = nil, nil
	}
	return false
}
