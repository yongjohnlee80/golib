package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
)

// RESIZE IS A WRAPPER, NOT A PER-WIDGET CAPABILITY.
//
// Resizable is a single-child container that owns a size and hands it down as
// constraints. That is not a new mechanism — it is what the layout model already
// is, constraints down and sizes up — and it buys four things bolting setters
// and grips onto every container does not:
//
//  1. The child needs ZERO knowledge and zero code. An Editor, a Modal, a
//     floating TextArea become resizable by being WRAPPED. They are not
//     modified, implement no interface, and never learn that resizing exists.
//  2. Making a new widget resizable requires no edit here. The extension is
//     composition.
//  3. The handle is a real component, so it gets hit-testing, capture, styling
//     and theming for free — no synthetic hit regions, no coordinate arithmetic
//     in a parent.
//  4. One implementation to get right, rather than one per container.
//
// DO NOT WRAP A SPLIT PANE. A Split sizes its children from its own ratio, so a
// Resizable inside a pane would fight its parent for authority over the same
// extent. The wrapper reports its clamped effective size honestly rather than
// pretending — a child of a tight parent simply cannot grow.

// SizeMode says where a Resizable's size comes from.
type SizeMode uint8

const (
	// SizeAuto tracks the child's intrinsic size, re-measured EVERY pass, so
	// later growth or shrinkage tracks. The default: a zero tui.Size run through
	// the sizing algorithm collapses the child to the parent minimum, often
	// 0x0, so "no configuration" has to mean intrinsic rather than zero.
	SizeAuto SizeMode = iota
	// SizeExplicit holds a caller-supplied size, from WithInitialSize, SetSize
	// or a drag.
	SizeExplicit
)

// String names the mode for traces and test failures.
func (m SizeMode) String() string {
	switch m {
	case SizeAuto:
		return "auto"
	case SizeExplicit:
		return "explicit"
	}
	return "unknown"
}

// Handle names one of the geometry operations a rectangle has: its four edges,
// its four corners, and the two dividers a two-pane split can carry.
//
// A deliberately CLOSED set. Extensibility comes from composing a widget with
// [Resizable], not from teaching Resizable arbitrary consumer-defined geometry —
// a new handle would need a new inset rule, a new grip rect and a new drag
// direction, none of which a consumer can supply without also owning layout.
//
// ONE vocabulary for both widgets. [Split] uses the divider appropriate to its
// axis and ignores the other, so a consumer binding a key to a resize action
// does not have to know which of the two widgets will answer it.
type Handle uint8

const (
	// HandleLeft drags the left edge: moving leftwards grows the box.
	HandleLeft Handle = iota
	HandleRight
	HandleTop
	HandleBottom
	HandleTopLeft
	HandleTopRight
	HandleBottomLeft
	HandleBottomRight
	// HandleVerticalDivider is the divider of a HORIZONTAL split — the panes
	// sit side by side, so the line between them runs down the screen.
	HandleVerticalDivider
	// HandleHorizontalDivider is the divider of a VERTICAL split.
	HandleHorizontalDivider
)

// String names the handle for traces and test failures.
func (h Handle) String() string {
	switch h {
	case HandleLeft:
		return "left"
	case HandleRight:
		return "right"
	case HandleTop:
		return "top"
	case HandleBottom:
		return "bottom"
	case HandleTopLeft:
		return "top-left"
	case HandleTopRight:
		return "top-right"
	case HandleBottomLeft:
		return "bottom-left"
	case HandleBottomRight:
		return "bottom-right"
	case HandleVerticalDivider:
		return "vertical-divider"
	case HandleHorizontalDivider:
		return "horizontal-divider"
	}
	return "unknown"
}

// Valid reports whether h is one of the declared handles.
func (h Handle) Valid() bool { return h <= HandleHorizontalDivider }

// resizes reports whether this handle sizes a BOX, which is what a [Resizable]
// grip does. The two dividers redistribute a shared extent instead, and belong
// to [Split]; a Resizable configured with one has been given a handle it cannot
// draw or drag.
func (h Handle) resizes() bool { return h.Valid() && h < HandleVerticalDivider }

// dx and dy are the sign a drag on this handle applies to each axis: dragging
// the right edge rightwards grows the box, dragging the left edge rightwards
// shrinks it, and an edge contributes nothing on the axis it does not touch.
//
// Written as an exhaustive switch rather than a default: a handle added without
// a direction would otherwise silently inherit one and drag the wrong way,
// which is the kind of defect that looks like a sign error for weeks.
func (h Handle) dx() int {
	switch h {
	case HandleLeft, HandleTopLeft, HandleBottomLeft:
		return -1
	case HandleRight, HandleTopRight, HandleBottomRight:
		return 1
	}
	return 0
}

func (h Handle) dy() int {
	switch h {
	case HandleTop, HandleTopLeft, HandleTopRight:
		return -1
	case HandleBottom, HandleBottomLeft, HandleBottomRight:
		return 1
	}
	return 0
}

// PlacementMode says whether the handle costs the child any cells.
type PlacementMode uint8

const (
	// PlacementOverlay draws the grip over the child's corner cell, changing no
	// geometry. The default: a resize affordance should not resize the thing it
	// is attached to merely by existing.
	PlacementOverlay PlacementMode = iota
	// PlacementReserve shrinks the child by the handle's cells, so nothing is
	// occluded.
	PlacementReserve
)

// String names the placement mode.
func (p PlacementMode) String() string {
	switch p {
	case PlacementOverlay:
		return "overlay"
	case PlacementReserve:
		return "reserve"
	}
	return "unknown"
}

// Valid reports whether p is one of the declared modes.
func (p PlacementMode) Valid() bool { return p <= PlacementReserve }

// StepUnit is what a keyboard resize step counts.
type StepUnit uint8

const (
	// StepCells moves a fixed number of cells.
	StepCells StepUnit = iota
	// StepPercent moves a percentage of the current size.
	StepPercent
)

// String names the unit.
func (s StepUnit) String() string {
	switch s {
	case StepCells:
		return "cells"
	case StepPercent:
		return "percent"
	}
	return "unknown"
}

// Valid reports whether s is one of the declared units.
func (s StepUnit) Valid() bool { return s <= StepPercent }

// ResizedEvent reports a committed size change.
//
// Owner is the WRAPPER's node, never a handle's: a wrapper with several grips
// still has one size, and reporting which grip moved it would make every
// listener normalise that away.
type ResizedEvent struct {
	Owner tui.NodeID
	Size  tui.Size
}

// Resizable is a single-child container whose size the user can drag.
type Resizable struct {
	Base

	child tui.Component

	mode      SizeMode
	requested tui.Size
	// committed is the effective size the last COMMIT stored. Layout computes
	// it and stores nothing; that is the whole point of the commit phase.
	committed tui.Size
	sized     bool

	min, max tui.Size
	handles  []Handle
	glyph    string
	// glyphCells is the DISPLAY WIDTH of glyph, measured once at construction.
	// Carried rather than recomputed because every grip rect and every reserve
	// inset depends on it, and a width computed in two places is a width that
	// can disagree with itself.
	glyphCells int
	placement  PlacementMode
	step       int
	stepUnit   StepUnit
	st         *ResizableStyle

	grips []*resizeHandle
	// drag is the pointer gesture in progress, nil when there is none, and
	// gripCtx is the handle that took the capture for it — only the node whose
	// handler took a capture may release it.
	drag          *resizeDrag
	gripCtx       *tui.Context
	pointerPolicy tui.PointerPolicy
}

// ResizableOption configures a Resizable at construction.
type ResizableOption func(*Resizable)

// NewResizable wraps child in a resizable box.
func NewResizable(child tui.Component, opts ...ResizableOption) *Resizable {
	if child == nil {
		panic(fatalOf("widget: NewResizable", "nil child",
			"a wrapper with nothing to size has no behaviour to offer"))
	}
	r := &Resizable{
		child:         child,
		mode:          SizeAuto,
		max:           tui.Size{W: tui.Unbounded, H: tui.Unbounded},
		handles:       []Handle{HandleBottomRight},
		glyph:         "◢",
		glyphCells:    1,
		step:          1,
		stepUnit:      StepCells,
		pointerPolicy: tui.PointerInherit,
	}
	for _, o := range opts {
		if o != nil {
			o(r)
		}
	}
	// Misconfiguration fails loud, at construction, where the values are written
	// in source. A min above a max has no sane interpretation — clamping one to
	// the other would silently pick which of the author's two statements to
	// ignore.
	if r.min.W > r.max.W || r.min.H > r.max.H {
		panic(tuiFatal("widget: NewResizable",
			"minimum size exceeds maximum on an axis", r.min.W))
	}
	for _, h := range r.handles {
		if !h.Valid() {
			panic(tuiFatal("widget: WithHandles",
				"value outside the declared Handle set", int(h)))
		}
	}
	return r
}

// WithInitialSize sets the starting size and implies SizeExplicit.
//
// A non-positive dimension is a construction panic: "resizable, zero cells wide"
// is not a state a caller can have meant, and adopting it would render nothing
// while looking configured.
func WithInitialSize(s tui.Size) ResizableOption {
	return func(r *Resizable) {
		if s.W <= 0 || s.H <= 0 {
			panic(tuiFatal("widget: WithInitialSize",
				"a non-positive dimension; use SizeAuto for an intrinsic size", s.W))
		}
		r.requested = s
		r.mode = SizeExplicit
	}
}

// WithMinSize sets the smallest size a drag or SetSize may reach.
func WithMinSize(s tui.Size) ResizableOption {
	return func(r *Resizable) { r.min = s }
}

// WithMaxSize sets the largest. Unbounded on an axis is the default and is
// legal for a maximum only.
func WithMaxSize(s tui.Size) ResizableOption {
	return func(r *Resizable) { r.max = s }
}

// WithHandles chooses the grips. Empty is INTENTIONAL and supported: a
// keyboard- and programmatic-only resizable has no visible affordance and still
// resizes through its actions.
//
// A DIVIDER is refused. The two divider handles redistribute a shared extent
// between two panes, which is [Split]'s operation, not a box's — a Resizable
// given one could neither draw it nor decide what dragging it should mean, so
// the mistake is caught where it is written rather than becoming a grip that
// silently does nothing.
func WithHandles(h ...Handle) ResizableOption {
	return func(r *Resizable) {
		for _, one := range h {
			if !one.resizes() {
				panic(fatalOf("widget: WithHandles",
					"a Resizable takes edge and corner handles; a divider belongs to Split",
					fmt.Sprintf("%v", one)))
			}
		}
		r.handles = append([]Handle(nil), h...)
	}
}

// WithHandleGlyph sets the grip's character.
//
// EXACTLY ONE grapheme cluster, of display width one or two. Rejected at
// construction, loudly, because the alternative is an affordance that is
// mounted, hit-testable and INVISIBLE: a two-column glyph placed in a one-column
// rect renders nothing at all, and the user sees a box they cannot resize with
// no indication why. An empty glyph is the same failure spelled differently.
//
// Width two is allowed rather than banned because plenty of natural resize
// glyphs are wide; what is not allowed is a width the geometry does not know
// about, so the measured width travels with the glyph into every rect and inset.
func WithHandleGlyph(g string) ResizableOption {
	return func(r *Resizable) {
		n := 0
		for range grapheme.Clusters(g) {
			n++
		}
		if n != 1 {
			panic(fatalOf("widget: WithHandleGlyph",
				"the grip glyph must be exactly one grapheme cluster",
				fmt.Sprintf("%q is %d clusters", g, n)))
		}
		w := grapheme.StringWidth(g, false)
		if w < 1 || w > 2 {
			panic(fatalOf("widget: WithHandleGlyph",
				"the grip glyph must have a display width of one or two cells",
				fmt.Sprintf("%q measures %d", g, w)))
		}
		r.glyph, r.glyphCells = g, w
	}
}

// WithHandlePlacement chooses whether the grip overlays the child or reserves
// cells from it.
func WithHandlePlacement(m PlacementMode) ResizableOption {
	return func(r *Resizable) {
		if !m.Valid() {
			panic(tuiFatal("widget: WithHandlePlacement",
				"value outside the declared PlacementMode set", int(m)))
		}
		r.placement = m
	}
}

// WithResizeStep sets how far one keyboard step moves an edge.
func WithResizeStep(n int, u StepUnit) ResizableOption {
	return func(r *Resizable) {
		if n <= 0 {
			panic(tuiFatal("widget: WithResizeStep", "a non-positive step", n))
		}
		if !u.Valid() {
			panic(tuiFatal("widget: WithResizeStep",
				"value outside the declared StepUnit set", int(u)))
		}
		r.step, r.stepUnit = n, u
	}
}

// WithResizableStyle associates a style at construction.
func WithResizableStyle(s *ResizableStyle) ResizableOption {
	return func(r *Resizable) { r.st = s }
}

// WithStyle associates a style at runtime and returns the wrapper for chaining.
// nil reverts to the default look.
func (r *Resizable) WithStyle(s *ResizableStyle) *Resizable {
	r.st = s
	r.MarkDirty()
	return r
}

// WithPointerPolicy sets whether this wrapper and its subtree accept pointer
// input, and returns it for chaining.
//
// It disables only the POINTER path: every resize action resolves on the
// wrapper itself, so a pointer-disabled Resizable is still fully resizable from
// the keyboard.
func (r *Resizable) WithPointerPolicy(p tui.PointerPolicy) *Resizable {
	if !p.Valid() {
		panic(tuiFatal("widget: Resizable.WithPointerPolicy",
			"value outside PointerInherit, PointerEnabled, PointerDisabled", int(p)))
	}
	r.pointerPolicy = p
	if ctx := r.Context(); ctx != nil {
		ctx.SetPointerPolicy(p)
	}
	return r
}

// SizeMode reports where the current size comes from.
func (r *Resizable) SizeMode() SizeMode { return r.mode }

// Size is the EFFECTIVE size — what is on screen, as stored by the last commit.
// Before the first layout it is the zero Size, because nothing has been
// committed and reporting a request as an effective size would be a lie.
func (r *Resizable) Size() tui.Size { return r.committed }

// RequestedSize is the last explicit request, unclamped, and whether there is
// one at all.
//
// ok is false in SizeAuto, because auto has no requested value: an earlier
// design returned a bare Size documented as "last request / auto", which had
// nothing to return before the first layout and no way to say "there is no
// request". Persistence stores this; an auto wrapper's geometry is never
// mistaken for a user's choice.
func (r *Resizable) RequestedSize() (tui.Size, bool) {
	if r.mode == SizeAuto {
		return tui.Size{}, false
	}
	return r.requested, true
}

// SetSize records an explicit request and switches to SizeExplicit.
//
// It CLAMPS TO THE CONFIGURED MINIMUM rather than panicking, unlike
// WithInitialSize: a runtime setter receives values computed from restored
// state and user input, where out-of-range is an ordinary outcome, while a
// construction option receives what an author wrote.
//
// It stores the request and requests layout; it publishes nothing. The
// effective size is whatever the next layout reaches, and the commit that
// follows reports it.
func (r *Resizable) SetSize(s tui.Size) {
	if s.W < r.min.W {
		s.W = r.min.W
	}
	if s.H < r.min.H {
		s.H = r.min.H
	}
	if s.W < 0 {
		s.W = 0
	}
	if s.H < 0 {
		s.H = 0
	}
	r.requested, r.mode = s, SizeExplicit
	r.RequestLayout()
}

// SetAuto returns to tracking the child's intrinsic size.
func (r *Resizable) SetAuto() {
	if r.mode == SizeAuto {
		return
	}
	r.mode = SizeAuto
	r.RequestLayout()
}

// Init mounts the child and one component per configured handle.
func (r *Resizable) Init(ctx *tui.Context) {
	r.Base.Init(ctx)
	ctx.SetPointerPolicy(r.pointerPolicy)
	ctx.SetDefaultActionResolvers(tui.ActionResolverFunc(r.resizeKeys))
	ctx.Mount(r.child)
	r.grips = r.grips[:0]
	for _, h := range r.handles {
		g := &resizeHandle{owner: r, handle: h}
		r.grips = append(r.grips, g)
		ctx.Mount(g)
	}
}

// THE WRAPPER IS NOT A TAB STOP EITHER, and that is the whole of row 15d:
// wrapping arbitrary content must change Tab order in no way at all.
//
// The resize actions are still reachable from the keyboard, because resolvers
// run at every node on the bubble path: an unhandled Shift-arrow from a focused
// DESCENDANT reaches this node's resolver on the way up.
//
// TWO COMPOSITIONS DO NOT GET THAT, and both are ordinary rather than exotic:
// content with no focusable descendant at all, and content that CONSUMES the
// keys — an [Editor] or a [TextArea] takes Shift-arrows for selection, so
// nothing bubbles. Those consumers drive resizing through the grips, through
// [Context.DoAction] with a resize action, or from a focus owner of their own.
// That cost is paid by the few compositions that need it, rather than by every
// Tab press in every application that wraps anything.
