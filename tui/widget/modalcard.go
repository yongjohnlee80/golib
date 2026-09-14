package widget

import (
	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// modalCard is the visible panel of a dialog: border, optional title, the
// caller's body, and a row of buttons.
//
// It is NOT focusable and never will be. Focus belongs to the buttons inside it
// or, when there are none to take it, to the Modal itself; a focusable card
// would insert a tab stop that does nothing between the dialog and its
// controls.
type modalCard struct {
	Base

	body    tui.Component
	buttons []*Button
	title   string
	st      *ModalStyle
}

func newModalCard(body tui.Component) *modalCard {
	return &modalCard{body: body}
}

// Init mounts the body and the buttons. Mounting them here rather than in Modal
// keeps the card the owner of its own children, so a later SetButtons has one
// place to reconcile.
func (c *modalCard) Init(ctx *tui.Context) {
	c.Base.Init(ctx)
	if c.body != nil {
		ctx.Mount(c.body)
	}
	for _, b := range c.buttons {
		if b != nil {
			ctx.Mount(b)
		}
	}
}

// AcceptsFocus reports that the card is not a tab stop. See the type comment.
func (c *modalCard) AcceptsFocus() bool { return false }

// setButtons swaps the button list AND reconciles the mounted children.
//
// Replacing only the slice is the obvious mistake and a loud one: the departing
// buttons stay mounted while the arriving ones never are, and the next layout
// pass panics on the first unmounted child it is asked to measure. The list and
// the tree are two representations of the same thing and have to move together.
//
// Buttons carried over from the old list are left alone rather than remounted,
// so a caller reordering or appending does not destroy and rebuild the controls
// the user is currently looking at — which would also throw away their focus.
func (c *modalCard) setButtons(b []*Button) {
	ctx := c.Context()
	if ctx == nil {
		c.buttons = append([]*Button(nil), b...)
		return // not mounted yet; Init will mount whatever is here
	}
	keep := make(map[*Button]bool, len(b))
	for _, nb := range b {
		if nb != nil {
			keep[nb] = true
		}
	}
	for _, ob := range c.buttons {
		if ob != nil && !keep[ob] {
			ctx.Unmount(ob)
		}
	}
	had := make(map[*Button]bool, len(c.buttons))
	for _, ob := range c.buttons {
		if ob != nil {
			had[ob] = true
		}
	}
	c.buttons = append([]*Button(nil), b...)
	for _, nb := range c.buttons {
		if nb != nil && !had[nb] {
			ctx.Mount(nb)
		}
	}
}

// Layout stacks the body above a right-aligned button row, inside a one-cell
// border, and sizes the card to its content.
func (c *modalCard) Layout(cs tui.Constraints) tui.Size {
	ctx := c.Context()
	if ctx == nil {
		return cs.Constrain(tui.Size{})
	}
	const border, pad = 1, 1
	frame := 2 * (border + pad)

	inner := tui.Size{W: max(cs.MaxW-frame, 0), H: max(cs.MaxH-frame, 0)}

	// Buttons first: they are the floor the body has to fit above, so measuring
	// them second would let a tall body squeeze them out of the card entirely.
	btnH, btnW := 0, 0
	sizes := make([]tui.Size, len(c.buttons))
	for i, b := range c.buttons {
		if b == nil {
			continue
		}
		sz := ctx.LayoutChild(b, tui.Loose(inner))
		sizes[i] = sz
		btnW += sz.W + 1 // one cell of breathing room between buttons
		btnH = max(btnH, sz.H)
	}
	if btnW > 0 {
		btnW-- // no trailing gap after the last button
	}

	titleH := 0
	if c.title != "" {
		titleH = 1
	}

	bodyH := 0
	bodyW := 0
	if c.body != nil {
		avail := tui.Size{W: inner.W, H: max(inner.H-btnH-titleH, 0)}
		bs := ctx.LayoutChild(c.body, tui.Loose(avail))
		bodyW, bodyH = bs.W, bs.H
	}

	contentW := max(bodyW, max(btnW, c.measure(c.title)))
	contentH := titleH + bodyH + btnH
	size := tui.Size{W: contentW + frame, H: contentH + frame}
	size = cs.Constrain(size)

	// Place children inside the frame, now that the card's own size is fixed.
	x0, y0 := border+pad, border+pad
	y := y0 + titleH
	if c.body != nil {
		ctx.PlaceChild(c.body, tui.Rect{X: x0, Y: y, W: min(bodyW, contentW), H: bodyH})
		y += bodyH
	}
	// Buttons sit at the card's bottom-right, the conventional place to look
	// for them, and are placed right-to-left so the last one hugs the edge.
	bx := x0 + contentW
	for i := len(c.buttons) - 1; i >= 0; i-- {
		b := c.buttons[i]
		if b == nil {
			continue
		}
		bx -= sizes[i].W
		ctx.PlaceChild(b, tui.Rect{X: bx, Y: y, W: sizes[i].W, H: sizes[i].H})
		bx--
	}
	return size
}

// Render paints the border, the background and the title. The body and buttons
// paint themselves.
func (c *modalCard) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	card := c.st.Card()
	s.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, " ", card)
	drawBorder(s, sz, c.st.Border())

	if c.title == "" {
		return
	}
	// The title is clipped to the space between the border corners rather than
	// allowed to overwrite them, so a long title cannot break the frame.
	x, limit := 2, sz.W-2
	for cluster := range tui.Graphemes(c.title) {
		w := s.StringWidth(cluster)
		if x+w > limit {
			break
		}
		s.SetCell(x, 1, cluster, c.st.Title())
		x += w
	}
}

// drawBorder paints a single-cell frame around the card.
func drawBorder(s tui.Surface, sz tui.Size, st style.Style) {
	if sz.W < 2 || sz.H < 2 {
		return // no room for a frame that does not overlap itself
	}
	right, bottom := sz.W-1, sz.H-1
	s.SetCell(0, 0, "┌", st)
	s.SetCell(right, 0, "┐", st)
	s.SetCell(0, bottom, "└", st)
	s.SetCell(right, bottom, "┘", st)
	for x := 1; x < right; x++ {
		s.SetCell(x, 0, "─", st)
		s.SetCell(x, bottom, "─", st)
	}
	for y := 1; y < bottom; y++ {
		s.SetCell(0, y, "│", st)
		s.SetCell(right, y, "│", st)
	}
}

// scrimLayer dims everything beneath it.
//
// It is a STACK LAYER rather than something a Modal or the host paints during
// its own Render, because a parent renders before its children: a scrim painted
// by the host would be covered by the base UI, and one painted by a Modal could
// not extend beyond that Modal's own rect. Ordering is the whole problem, and a
// layer is the only thing the stack orders.
type scrimLayer struct {
	Base
	st *ModalStyle
}

func (s *scrimLayer) Layout(cs tui.Constraints) tui.Size {
	return cs.Constrain(tui.Size{W: cs.MaxW, H: cs.MaxH})
}

// AcceptsFocus reports that the scrim is not a tab stop: it is decoration, and
// the dialog above it owns the focus.
func (s *scrimLayer) AcceptsFocus() bool { return false }

func (s *scrimLayer) Render(su tui.Surface) {
	sz := su.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	su.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, " ", s.st.Scrim())
}

// fatalOf builds the package's standard construction panic value.
func fatalOf(op, rule, detail string) error {
	return errs.Fatal{Op: op, Rule: rule, Detail: detail}
}
