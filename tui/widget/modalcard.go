package widget

import (
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
	title   uiText
	st      *ModalStyle
	align   ButtonAlign
	// rule draws a line between the body and the buttons; footer is a help line
	// beneath the buttons.
	rule   bool
	footer uiText
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

	title := c.title.inLanguage(&c.Base)
	if title == "" {
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
	for cluster := range tui.Graphemes(title) {
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
	footer := c.footer.inLanguage(&c.Base)
	if footer == "" || y <= 0 || y >= sz.H-1 {
		return
	}
	x, limit := inset, sz.W-inset
	st := c.st.Footer()
	for cluster := range tui.Graphemes(footer) {
		w := s.StringWidth(cluster)
		if x+w > limit {
			break
		}
		s.SetCell(x, y, cluster, st)
		x += w
	}
}

// drawBorder paints a single-cell frame around sz, in the border st carries (BorderNormal when
// none).
func drawBorder(s tui.Surface, sz tui.Size, st style.Style) {
	drawFrame(s, tui.Rect{W: sz.W, H: sz.H}, st, style.BorderNormal)
}

// Scrim dims everything beneath a dialog. The overlay host makes and owns it; it is exported
// so a native style can draw it (a real dim over what is beneath, which cells cannot show).
//
// It is a STACK LAYER rather than something a Modal or the host paints during
// its own Render, because a parent renders before its children: a scrim painted
// by the host would be covered by the base UI, and one painted by a Modal could
// not extend beyond that Modal's own rect. Ordering is the whole problem, and a
// layer is the only thing the stack orders.
type Scrim struct {
	Base
	st *ModalStyle
}

func (s *Scrim) Layout(cs tui.Constraints) tui.Size {
	return cs.Constrain(tui.Size{W: cs.MaxW, H: cs.MaxH})
}

// NOT FOCUSABLE BY DESIGN — no tui.Focusable: the scrim is decoration, and the
// dialog above it owns the focus.

func (s *Scrim) Render(su tui.Surface) { paintScrim(su, s.Context(), " ", s.st.Scrim()) }

// paintScrim dims su's whole area with glyph in st: the one backdrop painter, a Modal's Scrim's and
// a dimmed Float's. A native view drawing ctx's node dims what is beneath itself, and cells over it
// would hide it, so then nothing is painted.
func paintScrim(su tui.Surface, ctx *tui.Context, glyph string, st style.Style) {
	sz := su.Size()
	if sz.W <= 0 || sz.H <= 0 || ctx != nil && ctx.DrawnNatively() {
		return
	}
	su.Fill(tui.Rect{W: sz.W, H: sz.H}, glyph, st)
}
