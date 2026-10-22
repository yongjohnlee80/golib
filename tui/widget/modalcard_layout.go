package widget

import "github.com/yongjohnlee80/golib/tui"

// THE MODAL CARD'S LAYOUT: its buttons measured and wrapped into rows, the rows down the card and
// how a card too tall for the host gives them up, and where body, rule, help line and buttons go.

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
	title, footer := c.title.inLanguage(&c.Base), c.footer.inLanguage(&c.Base)
	const border, pad = 1, 1
	frame := 2 * (border + pad) // across: the frame and its padding, both sides

	inner := tui.Size{W: max(cs.MaxW-frame, 0), H: max(cs.MaxH-2*border, 0)}
	if c.width > 0 {
		// A set width is the room the body is offered — a field fills it —
		// and never more than the host has. It is a width, not a clip: the
		// help line and the title still fit, as they do on a card sized to
		// its content.
		want := max(c.width-frame, c.measure(footer), c.measure(title))
		inner.W = min(want, inner.W)
	}

	// Buttons first: they are the floor the body has to fit above, so measuring
	// them second would let a tall body squeeze them out of the card entirely.
	sizes, buttonRows, btnW, btnH := c.layoutButtons(ctx, inner)

	rows := c.cardRowsFor(pad, btnH, footer)
	footW := 0
	if footer != "" {
		footW = c.measure(footer)
	}

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
	rows.squeeze(bodyNeed, btnH, inner.H)

	bodyH := 0
	bodyW := 0
	if c.body != nil {
		avail := tui.Size{W: inner.W, H: max(inner.H-btnH-rows.footLine-rows.decoration(), 0)}
		bs := ctx.LayoutChild(c.body, tui.Loose(avail))
		bodyW, bodyH = bs.W, bs.H
	}

	// The help line's width counts only while it is shown, and the content is
	// never wider than the room inside the frame: the buttons are centred
	// within it, and a width wider than the card would place them outside it —
	// a long help line or title that the card clips anyway.
	if rows.footLine == 0 {
		footW = 0
	}
	contentW := min(max(bodyW, btnW, footW, c.measure(title)), inner.W)
	if c.width > 0 {
		contentW = inner.W
	}
	contentH := bodyH + rows.blankAbove + rows.ruleRow + rows.blankBelow + btnH + rows.footBlank + rows.footLine
	size := tui.Size{W: contentW + frame, H: contentH + 2*border + rows.padTop + rows.padBottom}
	size = cs.Constrain(size)

	// Place children inside the frame, now that the card's own size is fixed.
	x0, y := border+pad, border+rows.padTop
	ruleY, footerY := -1, -1
	if c.body != nil {
		ctx.PlaceChild(c.body, tui.Rect{X: x0, Y: y, W: min(bodyW, contentW), H: bodyH})
		if rows.ruleRow == 1 {
			ruleY = y + bodyH + rows.blankAbove
		}
		y += bodyH + rows.blankAbove + rows.ruleRow + rows.blankBelow
	}
	if rows.footLine == 1 {
		footerY = size.H - 1 - border - rows.padBottom
	}
	c.placeButtons(ctx, buttonRows, sizes, x0, y, contentW)
	// Render draws the rule and the help line at the rows committed after layout, so a measure
	// moves nothing on screen.
	ctx.AfterLayout(modalCardCommitKey, func() { c.ruleY, c.footerY = ruleY, footerY })
	return size
}

// cardButtonRow is one row of the card's buttons: the buttons first..end, their width with the
// gaps, and the tallest's height.
type cardButtonRow struct{ first, end, width, height int }

// layoutButtons lays each button out loose within inner, and wraps them into rows: they share a
// row while they fit and wrap only when the next would cross the inner edge, so every action
// stays reachable when the dialog is width-capped or the terminal is narrow. It returns the
// buttons' sizes, the rows, and the widest row and the rows' total height.
func (c *modalCard) layoutButtons(ctx *tui.Context, inner tui.Size) (sizes []tui.Size, rows []cardButtonRow, w, h int) {
	sizes = make([]tui.Size, len(c.buttons))
	for i, b := range c.buttons {
		if b == nil {
			continue
		}
		sizes[i] = ctx.LayoutChild(b, tui.Loose(inner))
	}
	row := cardButtonRow{first: 0}
	for i, b := range c.buttons {
		if b == nil {
			continue
		}
		sz := sizes[i]
		gap := 0
		if row.end > row.first {
			gap = 1
		}
		if gap > 0 && row.width+gap+sz.W > inner.W {
			rows = append(rows, row)
			row = cardButtonRow{first: i}
			gap = 0
		}
		row.width += gap + sz.W
		row.height = max(row.height, sz.H)
		row.end = i + 1
	}
	if row.end > row.first {
		rows = append(rows, row)
	}
	for _, r := range rows {
		w = max(w, r.width)
		h += r.height
	}
	return sizes, rows, w, h
}

// cardRows are the card's rows down it other than the body's and the buttons': the padding, the
// blank lines and rule around the message, and the footer's blank line and line.
type cardRows struct {
	padTop, padBottom               int
	blankAbove, ruleRow, blankBelow int
	footBlank, footLine             int
}

// cardRowsFor are the rows a card shows before any squeeze.
//
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
func (c *modalCard) cardRowsFor(pad, btnH int, footer string) cardRows {
	r := cardRows{padTop: pad, padBottom: pad}
	if c.body != nil && (btnH > 0 || footer != "") {
		r.blankAbove = 1
		if c.rule {
			r.ruleRow, r.blankBelow = 1, 1
		}
	}
	if footer != "" {
		r.footLine = 1
		if btnH > 0 {
			r.footBlank = 1
		}
	}
	return r
}

// decoration is the rows that are neither body, buttons nor the footer's line.
func (r *cardRows) decoration() int {
	return r.padTop + r.padBottom + r.blankAbove + r.ruleRow + r.blankBelow + r.footBlank
}

// squeeze gives up rows, in order, until the body's need, the buttons and what is left fit innerH:
// the padding, then the rule's margins, then the footer's blank line, then the rule line, then the
// help line.
func (r *cardRows) squeeze(bodyNeed, btnH, innerH int) {
	for _, row := range []*int{&r.padBottom, &r.padTop, &r.blankBelow, &r.blankAbove, &r.footBlank, &r.ruleRow, &r.footLine} {
		if bodyNeed+btnH+r.footLine+r.decoration() <= innerH {
			break
		}
		*row = 0
	}
}

// placeButtons places the button rows from y down, each aligned on its own within contentW, so a
// short final row is still centred (or right-aligned) rather than stranded at the left edge.
func (c *modalCard) placeButtons(ctx *tui.Context, rows []cardButtonRow, sizes []tui.Size, x0, y, contentW int) {
	for _, r := range rows {
		bx := x0
		switch c.align {
		case ButtonsRight:
			bx = x0 + max(contentW-r.width, 0)
		case ButtonsCenter:
			bx = x0 + max(contentW-r.width, 0)/2
		}
		first := true
		for i := r.first; i < r.end; i++ {
			if c.buttons[i] == nil {
				continue
			}
			if !first {
				bx++
			}
			ctx.PlaceChild(c.buttons[i], tui.Rect{X: bx, Y: y, W: sizes[i].W, H: sizes[i].H})
			bx += sizes[i].W
			first = false
		}
		y += r.height
	}
}

// modalCardCommitKey is the card's rule and footer rows commit (tui.Context.AfterLayout).
const modalCardCommitKey tui.CommitKey = "widget.modalcard"
