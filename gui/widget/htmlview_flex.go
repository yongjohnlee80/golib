package widget

import (
	"strconv"
	"strings"

	xhtml "github.com/yongjohnlee80/golib/extract/html"
	"github.com/yongjohnlee80/golib/gui/flow"
	phtml "github.com/yongjohnlee80/golib/parse/html"
)

// FLEX AND GRID — a flex or grid container's children are items: each element child blockified, and
// each run of text an anonymous item. A flex container lays them in a row (or a column), wrapping
// when it may, growing and shrinking them by flex-grow and flex-shrink from their basis, then
// spreading them by justify-content and aligning them by align-items. A grid container lays them
// in order, row by row, in the columns grid-template-columns gives (lengths, fr, minmax(),
// repeat() with a count or auto-fit/auto-fill); named areas and explicit placement are not read.

// items are a flex or grid container's children as items.
func (b *builder) items(n *phtml.Node, st *computed, src [2]int) []*box {
	var out []*box
	for _, c := range n.Children {
		switch c.Kind {
		case phtml.Text:
			if strings.TrimSpace(c.Data) == "" {
				continue
			}
			holder := &phtml.Node{Kind: phtml.StartTag, Children: []*phtml.Node{c}}
			anon := &box{kind: kBlock, st: st.inherit(), src: src}
			anon.st.display = "block"
			anon.kids = b.children(holder, st, src)
			out = append(out, anon)
			continue
		case phtml.StartTag, phtml.SelfClosing:
		default:
			continue
		}
		if xhtml.Dropped(c) || b.depth >= maxBoxDepth {
			continue
		}
		b.chain = append(b.chain, c)
		cs := b.s.style(c, st, b.chain)
		switch {
		case cs.display == "none":
		case c.Name == "img":
			item := &box{kind: kBlock, st: cs, node: c, src: src}
			item.kids = []*box{{kind: kRun, st: cs, src: src, spans: []flow.Span{b.l.imageSpan(c, cs, src)}}}
			out = append(out, item)
		default:
			b.depth++
			out = append(out, b.box(c, cs, src))
			b.depth--
		}
		b.chain = b.chain[:len(b.chain)-1]
	}
	return out
}

// layFlex lays a flex container's items in width from y and answers their height.
func (l *htmlLayout) layFlex(bx *box, x, y, width float32) float32 {
	st := bx.st
	px := func(v cssLen, of float32) float32 { return v.px(st.fontSize, l.rootPx, of, l.view()) }
	gapRow, gapCol := px(st.gap[0], 0), px(st.gap[1], width)
	var items, abs []*box
	for _, k := range bx.kids {
		if k.st.position == "absolute" {
			abs = append(abs, k)
		} else {
			items = append(items, k)
		}
	}
	var h float32
	if st.flexDir == "column" {
		h = l.layFlexColumn(bx, items, x, y, width, gapRow)
	} else {
		h = l.layFlexRow(bx, items, x, y, width, gapRow, gapCol)
	}
	for _, k := range abs {
		l.layAbsolute(k, x, y, width, y)
	}
	return h
}

type flexItem struct {
	k          *box
	e          edges
	base, minW float32 // border-box widths
	w          float32 // the border-box width laid out
}

func (it flexItem) margins() float32 { return it.e.m[1] + it.e.m[3] }

func (l *htmlLayout) layFlexRow(bx *box, items []*box, x, y, width, gapRow, gapCol float32) float32 {
	st := bx.st
	fis := make([]flexItem, len(items))
	for i, k := range items {
		e := l.edgesOf(k.st, width)
		lo, hi := l.minMax(k, width)
		base := hi
		kpx := func(v cssLen) float32 { return v.px(k.st.fontSize, l.rootPx, width, l.view()) }
		switch {
		case k.st.basis.set() && !k.st.basis.auto():
			base = kpx(k.st.basis)
			if !k.st.borderBox {
				base += e.left() + e.right()
			}
		case k.st.width.set() && !k.st.width.auto():
			base = kpx(k.st.width)
			if !k.st.borderBox {
				base += e.left() + e.right()
			}
		}
		fis[i] = flexItem{k: k, e: e, base: base, minW: min(lo, base)}
	}
	// lines: one, or as many as wrapping needs
	var lines [][]flexItem
	for i := 0; i < len(fis); {
		j, used := i, float32(0)
		for j < len(fis) {
			add := fis[j].base + fis[j].margins()
			if j > i {
				add += gapCol
			}
			if st.flexWrap && j > i && used+add > width {
				break
			}
			used += add
			j++
		}
		lines = append(lines, fis[i:j])
		i = j
	}
	cy := y
	for li, line := range lines {
		if li > 0 {
			cy += gapRow
		}
		used, grow, shrink := gapCol*float32(len(line)-1), float32(0), float32(0)
		for _, it := range line {
			used += it.base + it.margins()
			grow += it.k.st.grow
			shrink += it.k.st.shrink * it.base
		}
		free := width - used
		for i := range line {
			it := &line[i]
			it.w = it.base
			switch {
			case free > 0 && grow > 0:
				it.w += free * it.k.st.grow / grow
			case free < 0 && shrink > 0:
				it.w = max(it.base+free*it.k.st.shrink*it.base/shrink, it.minW)
			}
		}
		// what is left after growing and shrinking is spread by justify-content
		left := width - gapCol*float32(len(line)-1)
		for _, it := range line {
			left -= it.w + it.margins()
		}
		start, between := justify(st.justify, left, len(line))
		ix := x + start
		var lineH float32
		for _, it := range line {
			it.k.container = st
			l.layBlock(it.k, ix, cy+it.e.m[0], it.w+it.margins())
			lineH = max(lineH, it.k.h+it.e.m[0]+it.e.m[2])
			ix += it.w + it.margins() + gapCol + between
		}
		for _, it := range line {
			l.align(it.k, it.e, cy, lineH)
		}
		cy += lineH
	}
	return cy - y
}

func (l *htmlLayout) layFlexColumn(bx *box, items []*box, x, y, width, gapRow float32) float32 {
	cy := y
	for i, k := range items {
		if i > 0 {
			cy += gapRow
		}
		k.container = bx.st
		e := l.edgesOf(k.st, width)
		avail := width
		al := alignOf(bx.st, k.st)
		if al != "stretch" && (!k.st.width.set() || k.st.width.auto()) {
			_, hi := l.minMax(k, width)
			avail = min(hi+e.m[1]+e.m[3], width)
		}
		l.layBlock(k, x, cy+e.m[0], avail)
		switch al {
		case "center":
			shiftBox(k, (width-avail)/2, 0)
		case "end", "flex-end":
			shiftBox(k, width-avail, 0)
		}
		cy = k.y + k.h + e.m[2]
	}
	return cy - y
}

// alignOf is an item's alignment in its container: align-self, else the container's align-items.
func alignOf(container, item *computed) string {
	if item.alignSelf != "" && item.alignSelf != "auto" {
		return item.alignSelf
	}
	if container.alignItems == "" || container.alignItems == "normal" {
		return "stretch"
	}
	return container.alignItems
}

// align places an item laid at the top of a line lineH tall: stretched to it (a box with no
// height of its own), centred, or at its end.
func (l *htmlLayout) align(k *box, e edges, top, lineH float32) {
	room := lineH - e.m[0] - e.m[2] - k.h
	switch alignOf(l.containerOf(k), k.st) {
	case "stretch":
		if !k.st.height.set() || k.st.height.auto() {
			k.h += max(room, 0)
		}
	case "center":
		shiftBox(k, 0, room/2)
	case "end", "flex-end":
		shiftBox(k, 0, room)
	}
}

// containerOf is the style an item aligns by: set by layFlex and layGrid on the items they lay.
func (l *htmlLayout) containerOf(k *box) *computed {
	if k.container != nil {
		return k.container
	}
	return &computed{}
}

// justify is where the first item starts and the extra room between items, for left room spread
// by justify-content among n items.
func justify(how string, left float32, n int) (start, between float32) {
	if left <= 0 || n == 0 {
		return 0, 0
	}
	switch how {
	case "center":
		return left / 2, 0
	case "end", "flex-end", "right":
		return left, 0
	case "space-between":
		if n == 1 {
			return 0, 0
		}
		return 0, left / float32(n-1)
	case "space-around":
		return left / float32(n) / 2, left / float32(n)
	case "space-evenly":
		return left / float32(n+1), left / float32(n+1)
	}
	return 0, 0
}

// ---- grid ----

// gridTrack is a column as grid-template-columns gives it: a length, a share of what is left (fr),
// or minmax() of the two.
type gridTrack struct {
	min    float32 // px
	fr     float32 // 0: a fixed track, its min
	fixed  bool
	repeat int // as written in repeat(): >0 a count, -1 auto-fill, -2 auto-fit; the tracks follow
}

// gridColumns is the columns' widths in width for n items, from a grid-template-columns value.
func (l *htmlLayout) gridColumns(st *computed, width, gap float32, n int) []float32 {
	spec := strings.TrimSpace(st.gridCols)
	if spec == "" || spec == "none" {
		return []float32{width}
	}
	px := func(s string) (float32, bool) {
		v, ok := parseLen(s)
		if !ok || v.auto() {
			return 0, false
		}
		return v.px(st.fontSize, l.rootPx, width, l.view()), true
	}
	var one func(s string) (gridTrack, bool)
	one = func(s string) (gridTrack, bool) {
		s = strings.TrimSpace(s)
		switch {
		case strings.HasSuffix(s, "fr"):
			f, err := strconv.ParseFloat(strings.TrimSuffix(s, "fr"), 32)
			return gridTrack{fr: float32(f)}, err == nil && f > 0
		case s == "auto":
			return gridTrack{fr: 1}, true
		case strings.HasPrefix(s, "minmax(") && strings.HasSuffix(s, ")"):
			parts := splitTop(s[len("minmax("):len(s)-1], ',')
			if len(parts) != 2 {
				return gridTrack{}, false
			}
			lo, _ := px(strings.TrimSpace(parts[0]))
			hi, ok := one(parts[1])
			if !ok {
				return gridTrack{}, false
			}
			if hi.fr > 0 {
				return gridTrack{min: lo, fr: hi.fr}, true
			}
			return gridTrack{min: max(lo, hi.min), fixed: true}, true
		}
		v, ok := px(s)
		return gridTrack{min: v, fixed: true}, ok
	}
	var tracks []gridTrack
	for _, tok := range splitTop(spec, ' ') {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		if strings.HasPrefix(tok, "repeat(") && strings.HasSuffix(tok, ")") {
			parts := splitTop(tok[len("repeat("):len(tok)-1], ',')
			if len(parts) != 2 {
				return []float32{width}
			}
			var inner []gridTrack
			for _, t := range splitTop(strings.TrimSpace(parts[1]), ' ') {
				if strings.TrimSpace(t) == "" {
					continue
				}
				tr, ok := one(t)
				if !ok {
					return []float32{width}
				}
				inner = append(inner, tr)
			}
			if len(inner) == 0 {
				return []float32{width}
			}
			count := 0
			switch how := strings.TrimSpace(parts[0]); how {
			case "auto-fit", "auto-fill":
				per := float32(0)
				for _, tr := range inner {
					per += max(tr.min, 1)
				}
				per += gap * float32(len(inner))
				count = max(int((width+gap)/per), 1)
				if how == "auto-fit" {
					count = min(count, max((n+len(inner)-1)/len(inner), 1)) // empty tracks fold away
				}
			default:
				c, err := strconv.Atoi(how)
				if err != nil || c < 1 {
					return []float32{width}
				}
				count = min(c, 1000)
			}
			for range count {
				tracks = append(tracks, inner...)
			}
			continue
		}
		tr, ok := one(tok)
		if !ok {
			return []float32{width}
		}
		tracks = append(tracks, tr)
	}
	if len(tracks) == 0 {
		return []float32{width}
	}
	// fixed tracks first; what is left is shared by fr. A track whose share is under its min is
	// frozen at the min and the rest shared again among the others, as CSS's fr algorithm does.
	out := make([]float32, len(tracks))
	left := width - gap*float32(len(tracks)-1)
	frozen := make([]bool, len(tracks))
	for i, tr := range tracks {
		if tr.fixed {
			out[i], frozen[i] = tr.min, true
			left -= tr.min
		}
	}
	for range len(tracks) {
		frs := float32(0)
		for i, tr := range tracks {
			if !frozen[i] {
				frs += tr.fr
			}
		}
		if frs == 0 {
			break
		}
		changed := false
		for i, tr := range tracks {
			if !frozen[i] && max(left, 0)*tr.fr/frs < tr.min {
				out[i], frozen[i] = tr.min, true
				left -= tr.min
				changed = true
			}
		}
		if changed {
			continue
		}
		for i, tr := range tracks {
			if !frozen[i] {
				out[i] = max(left, 0) * tr.fr / frs
			}
		}
		break
	}
	return out
}

// layGrid lays a grid container's items in its columns, in order, row by row, and answers their
// height.
func (l *htmlLayout) layGrid(bx *box, x, y, width float32) float32 {
	st := bx.st
	px := func(v cssLen, of float32) float32 { return v.px(st.fontSize, l.rootPx, of, l.view()) }
	gapRow, gapCol := px(st.gap[0], 0), px(st.gap[1], width)
	var items, abs []*box
	for _, k := range bx.kids {
		if k.st.position == "absolute" {
			abs = append(abs, k)
		} else {
			items = append(items, k)
		}
	}
	cols := l.gridColumns(st, width, gapCol, len(items))
	cy := y
	for r := 0; r*len(cols) < len(items); r++ {
		if r > 0 {
			cy += gapRow
		}
		row := items[r*len(cols) : min((r+1)*len(cols), len(items))]
		cx := x
		var rowH float32
		edgesOf := make([]edges, len(row))
		for i, k := range row {
			e := l.edgesOf(k.st, cols[i])
			edgesOf[i] = e
			k.container = st
			l.layBlock(k, cx, cy+e.m[0], cols[i])
			rowH = max(rowH, k.h+e.m[0]+e.m[2])
			cx += cols[i] + gapCol
		}
		for i, k := range row {
			l.align(k, edgesOf[i], cy, rowH)
		}
		cy += rowH
	}
	for _, k := range abs {
		l.layAbsolute(k, x, y, width, y)
	}
	return cy - y
}
