package widget

import (
	"github.com/yongjohnlee80/golib/tui"
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

// Handle names a corner or edge grip.
//
// The set is closed and geometric: these are the positions a rectangle has.
type Handle uint8

const (
	// HandleBottomRight is the default and the conventional resize corner.
	HandleBottomRight Handle = iota
	HandleBottomLeft
	HandleTopRight
	HandleTopLeft
	HandleRight
	HandleBottom
)

// String names the handle for traces and test failures.
func (h Handle) String() string {
	switch h {
	case HandleBottomRight:
		return "bottom-right"
	case HandleBottomLeft:
		return "bottom-left"
	case HandleTopRight:
		return "top-right"
	case HandleTopLeft:
		return "top-left"
	case HandleRight:
		return "right"
	case HandleBottom:
		return "bottom"
	}
	return "unknown"
}

// Valid reports whether h is one of the declared handles.
func (h Handle) Valid() bool { return h <= HandleBottom }

// dx and dy are the sign a drag on this handle applies to each axis: dragging
// the right edge rightwards grows, dragging the left edge rightwards shrinks.
func (h Handle) dx() int {
	switch h {
	case HandleBottomLeft, HandleTopLeft:
		return -1
	case HandleBottom:
		return 0
	}
	return 1
}

func (h Handle) dy() int {
	switch h {
	case HandleTopRight, HandleTopLeft:
		return -1
	case HandleRight:
		return 0
	}
	return 1
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

	min, max  tui.Size
	handles   []Handle
	glyph     string
	placement PlacementMode
	step      int
	stepUnit  StepUnit
	st        *ResizableStyle

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
func WithHandles(h ...Handle) ResizableOption {
	return func(r *Resizable) { r.handles = append([]Handle(nil), h...) }
}

// WithHandleGlyph sets the grip's character.
func WithHandleGlyph(g string) ResizableOption {
	return func(r *Resizable) { r.glyph = g }
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
	ctx.SetDefaultActionResolvers(tui.ActionResolverFunc(resizeKeys))
	ctx.Mount(r.child)
	r.grips = r.grips[:0]
	for _, h := range r.handles {
		g := &resizeHandle{owner: r, handle: h}
		r.grips = append(r.grips, g)
		ctx.Mount(g)
	}
}

// AcceptsFocus reports that the WRAPPER is a tab stop, so the resize actions
// are reachable from the keyboard.
//
// The handles are deliberately not focusable: adding nodes to the tree must not
// pollute Tab order, and a grip is a pointer affordance rather than a traversal
// stop.
func (r *Resizable) AcceptsFocus() bool { return true }

// handleCells is the space a grip occupies on each axis, which is zero unless
// the placement reserves it.
func (r *Resizable) handleCells() (w, h int) {
	if r.placement != PlacementReserve || len(r.handles) == 0 {
		return 0, 0
	}
	for _, g := range r.handles {
		if g.dx() != 0 {
			w = 1
		}
		if g.dy() != 0 {
			h = 1
		}
	}
	return w, h
}
