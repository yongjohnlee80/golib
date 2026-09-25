package widget

import (
	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// modalCard is the visible panel of a dialog: border, optional title, the
// caller's body, an optional rule, a row of buttons, and an optional footer.
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
	// rule draws a line between the body and the buttons; footer is a help line
	// beneath the buttons.
	rule   bool
	footer string
	// width is the card's own width, frame included; 0 sizes it to its
	// content.
	width int
	// ruleY and footerY are the rows the rule and the help line were laid
	// out on, for Render; -1 for none.
	ruleY, footerY int
}

func newModalCard(body tui.Component) *modalCard {
	return &modalCard{body: body, ruleY: -1, footerY: -1}
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

// NOT FOCUSABLE BY DESIGN — no tui.Focusable: the card is not a tab stop. See
// the type comment.

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
	frame := 2 * (border + pad) // across: the frame and its padding, both sides

	inner := tui.Size{W: max(cs.MaxW-frame, 0), H: max(cs.MaxH-2*border, 0)}
	if c.width > 0 {
		// A set width is the room the body is offered — a field fills it —
		// and never more than the host has. It is a width, not a clip: the
		// help line and the title still fit, as they do on a card sized to
		// its content.
		want := max(c.width-frame, c.measure(c.footer), c.measure(c.title))
		inner.W = min(want, inner.W)
	}

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

	// THE ROWS DOWN THE CARD. Padding inside the frame, top and bottom.
	//
	// A BLANK LINE BETWEEN THE MESSAGE AND WHAT FOLLOWS IT — the controls, or,
	// with none, the help line — when there are both. Without it the buttons
	// sit directly under the last line of prose and read as part of it, which
	// is how a confirmation ends up looking like a sentence with two words
	// highlighted.
	//
	// A RULE takes the blank line's place and a row either side of it: the
	// line is the separation, and prose touching it would read as underlined.
	//
	// The footer sits one blank row under the buttons, so it reads as the
	// footer of the whole card rather than a caption on the button row.
	padTop, padBottom := pad, pad
	blankAbove, ruleRow, blankBelow := 0, 0, 0
	if c.body != nil && (btnH > 0 || c.footer != "") {
		blankAbove = 1
		if c.rule {
			ruleRow, blankBelow = 1, 1
		}
	}
	footBlank, footLine, footW := 0, 0, 0
	if c.footer != "" {
		footLine, footW = 1, c.measure(c.footer)
		if btnH > 0 {
			footBlank = 1
		}
	}
	decoration := func() int { return padTop + padBottom + blankAbove + ruleRow + blankBelow + footBlank }

	// THE QUESTION AND ITS ANSWERS BEFORE ANYTHING ELSE. A card taller than
	// the host gives up rows before any of the message: its blank rows — the
	// padding, then the rule's margins, then the footer's — then the rule line,
	// then the help line. The message is what the dialog is for and the
	// buttons are how it is answered; a squeezed card that kept its decoration
	// and lost the question asked nothing.
	//
	// What the body NEEDS is its INTRINSIC height: what it answers when offered
	// an unbounded height, which the layout contract defines as its preferred
	// content size and never the offer itself (tui.Unbounded). A message of six
	// lines needs six; a view that fills whatever it is given — a list, an
	// editor — answers its own minimum, so a roomy dialog around one keeps its
	// rule and help line. Nothing is inferred from how a body reacts to a
	// constraint: a long message clipped by one looks exactly like a filler.
	bodyNeed := 0
	if c.body != nil {
		bodyNeed = ctx.LayoutChild(c.body, tui.Constraints{MaxW: inner.W, MaxH: tui.Unbounded}).H
	}
	for _, row := range []*int{&padBottom, &padTop, &blankBelow, &blankAbove, &footBlank, &ruleRow, &footLine} {
		if bodyNeed+btnH+footLine+decoration() <= inner.H {
			break
		}
		*row = 0
	}

	bodyH := 0
	bodyW := 0
	if c.body != nil {
		avail := tui.Size{W: inner.W, H: max(inner.H-btnH-footLine-decoration(), 0)}
		bs := ctx.LayoutChild(c.body, tui.Loose(avail))
		bodyW, bodyH = bs.W, bs.H
	}

	// The help line's width counts only while it is shown, and the content is
	// never wider than the room inside the frame: the buttons are centred
	// within it, and a width wider than the card would place them outside it —
	// a long help line or title that the card clips anyway.
	if footLine == 0 {
		footW = 0
	}
	contentW := min(max(bodyW, btnW, footW, c.measure(c.title)), inner.W)
	if c.width > 0 {
		contentW = inner.W
	}
	contentH := bodyH + blankAbove + ruleRow + blankBelow + btnH + footBlank + footLine
	size := tui.Size{W: contentW + frame, H: contentH + 2*border + padTop + padBottom}
	size = cs.Constrain(size)

	// Place children inside the frame, now that the card's own size is fixed.
	x0, y := border+pad, border+padTop
	c.ruleY, c.footerY = -1, -1
	if c.body != nil {
		ctx.PlaceChild(c.body, tui.Rect{X: x0, Y: y, W: min(bodyW, contentW), H: bodyH})
		if ruleRow == 1 {
			c.ruleY = y + bodyH + blankAbove
		}
		y += bodyH + blankAbove + ruleRow + blankBelow
	}
	if footLine == 1 {
		c.footerY = size.H - 1 - border - padBottom
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
	c.renderRule(s, sz)
	c.renderFooter(s, sz)

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

// renderRule draws the rule across the card, joined to the frame with tees.
func (c *modalCard) renderRule(s tui.Surface, sz tui.Size) {
	if c.ruleY <= 0 || c.ruleY >= sz.H-1 || sz.W < 2 {
		return
	}
	st := c.st.Rule()
	s.SetCell(0, c.ruleY, "├", st)
	for x := 1; x < sz.W-1; x++ {
		s.SetCell(x, c.ruleY, "─", st)
	}
	s.SetCell(sz.W-1, c.ruleY, "┤", st)
}

// renderFooter draws the help line on the row Layout gave it — the last row
// inside the frame, above the bottom padding while the card has room for it —
// clipped to the space inside the padding.
func (c *modalCard) renderFooter(s tui.Surface, sz tui.Size) {
	const inset = 2 // border and padding, across
	y := c.footerY  // the row Layout gave it
	if c.footer == "" || y <= 0 || y >= sz.H-1 {
		return
	}
	x, limit := inset, sz.W-inset
	st := c.st.Footer()
	for cluster := range tui.Graphemes(c.footer) {
		w := s.StringWidth(cluster)
		if x+w > limit {
			break
		}
		s.SetCell(x, y, cluster, st)
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

// NOT FOCUSABLE BY DESIGN — no tui.Focusable: the scrim is decoration, and the
// dialog above it owns the focus.

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
