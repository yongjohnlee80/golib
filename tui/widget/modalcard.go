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
	align   ButtonAlign
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

// setButtons swaps the button list AND reconciles the mounted children, as one
// tree mutation.
//
// Replacing only the slice is the obvious mistake and a loud one: the departing
// buttons stay mounted while the arriving ones never are, and the next layout
// pass panics on the first unmounted child it is asked to measure. The list and
// the tree are two representations of the same thing and have to move together.
//
// THE WHOLE RECONCILE IS ONE BATCH. Each Unmount repairs focus on its own, so an
// unbatched three-out-two-in replacement repairs five times against lists that
// were never a state anybody asked for, and leaves focus wherever the last
// intermediate step happened to put it. Inside a batch there is exactly one
// repair, at the end, against the final list.
//
// RETAINED BUTTONS ARE MOVED, NEVER REMOUNTED. A caller reordering the controls
// wants them reordered, not destroyed and rebuilt: a remount would give each a
// new NodeID, cancel its lifetime context, run its unmount hooks and throw away
// its focus, for a change the user asked to be cosmetic. Move preserves all of
// that and still updates document order — which IS tab order, so a visual
// reorder that skipped the Move would leave Tab visiting the old sequence.
//
// The caller has already validated the list; this method assumes it and only
// reconciles.
func (c *modalCard) setButtons(b []*Button) {
	ctx := c.Context()
	if ctx == nil {
		c.buttons = append([]*Button(nil), b...)
		return // not mounted yet; Init will mount whatever is here
	}
	next := append([]*Button(nil), b...)
	old := c.buttons // snapshot: c.buttons is replaced before the tree catches up

	keep := make(map[*Button]bool, len(next))
	for _, nb := range next {
		keep[nb] = true
	}
	had := make(map[*Button]bool, len(old))
	for _, ob := range old {
		if ob != nil {
			had[ob] = true
		}
	}

	ctx.BatchTreeMutation(func() {
		// The logical list is installed FIRST, so that anything reached during
		// the mutation — a focus repair, an InitialFocus nomination, a layout
		// request — sees the list the caller asked for rather than a mixture.
		c.buttons = next
		for _, ob := range old {
			if ob != nil && !keep[ob] {
				ctx.Unmount(ob)
			}
		}
		for _, nb := range next {
			if !had[nb] {
				ctx.Mount(nb)
			}
		}
		// Order every button, arrivals included, in one pass. Mounting appends,
		// so an arrival is already last and a Move to its real index is what
		// puts it where the caller asked; Move is a no-op when the child is
		// already there, so this costs nothing for an unchanged list.
		off := c.childOffset()
		for i, nb := range next {
			ctx.Move(nb, i+off)
		}
	})
}

// childOffset is the number of the card's children that precede its buttons.
// The body, when there is one, is mounted first.
func (c *modalCard) childOffset() int {
	if c.body != nil {
		return 1
	}
	return 0
}

// Layout stacks the body above a button row, inside a one-cell border, and
// sizes the card to its content.
//
// THE TITLE COSTS NO CONTENT ROW. It is painted into the top border, the way a
// framed panel is titled everywhere else, so the body starts on the first line
// inside the frame instead of one below a banner. It still sets a floor on the
// card's width, since a title wider than the body must not be clipped.
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

	// A BLANK LINE BETWEEN THE MESSAGE AND THE CONTROLS, when there are both.
	// Without it the buttons sit directly under the last line of prose and read
	// as part of it, which is how a confirmation ends up looking like a
	// sentence with two words highlighted.
	gap := 0
	if c.body != nil && btnH > 0 {
		gap = 1
	}

	bodyH := 0
	bodyW := 0
	if c.body != nil {
		avail := tui.Size{W: inner.W, H: max(inner.H-btnH-gap, 0)}
		bs := ctx.LayoutChild(c.body, tui.Loose(avail))
		bodyW, bodyH = bs.W, bs.H
	}

	contentW := max(bodyW, max(btnW, c.measure(c.title)))
	contentH := bodyH + gap + btnH
	size := tui.Size{W: contentW + frame, H: contentH + frame}
	size = cs.Constrain(size)

	// Place children inside the frame, now that the card's own size is fixed.
	x0, y0 := border+pad, border+pad
	y := y0
	if c.body != nil {
		ctx.PlaceChild(c.body, tui.Rect{X: x0, Y: y, W: min(bodyW, contentW), H: bodyH})
		y += bodyH + gap
	}
	// Where the row of buttons sits within the content width. Centred by
	// default: a dialog is read down its middle, and a pair of controls hugging
	// one edge of a card wider than they are looks detached from the question.
	bx := x0
	switch c.align {
	case ButtonsRight:
		bx = x0 + max(contentW-btnW, 0)
	case ButtonsCenter:
		bx = x0 + max(contentW-btnW, 0)/2
	}
	for i, b := range c.buttons {
		if b == nil {
			continue
		}
		ctx.PlaceChild(b, tui.Rect{X: bx, Y: y, W: sizes[i].W, H: sizes[i].H})
		bx += sizes[i].W + 1
	}
	return size
}

// Render paints the card, its frame, and the title INTO the top border.
//
// The body and the buttons are children and paint themselves.
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
	// ON THE BORDER LINE, with a space either side so the rule does not touch
	// the text. Clipped to the space between the corners rather than allowed to
	// overwrite them, so a long title cannot break the frame.
	// The last WRITABLE cell is sz.W-2: the frame owns column sz.W-1. An
	// exclusive limit of sz.W-2 is one short and clips the title's trailing
	// space against the corner, which reads as the text running into the frame.
	limit := sz.W - 1
	x := 1
	if x < limit {
		s.SetCell(x, 0, " ", c.st.Border())
		x++
	}
	for cluster := range tui.Graphemes(c.title) {
		w := s.StringWidth(cluster)
		if x+w > limit {
			break
		}
		s.SetCell(x, 0, cluster, c.st.Title())
		x += w
	}
	if x < limit {
		s.SetCell(x, 0, " ", c.st.Border())
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
