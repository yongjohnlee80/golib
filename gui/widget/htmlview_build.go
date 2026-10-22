package widget

import (
	"hash/fnv"
	"image/color"
	"math"
	"strings"

	xhtml "github.com/yongjohnlee80/golib/extract/html"
	phtml "github.com/yongjohnlee80/golib/parse/html"
)

// THE BUILD — a page's DOM and styles made into its top-level blocks, and the key a laid-out
// block is cached by.

// build reads the page into top-level blocks, keeping the layout of each block whose key is
// unchanged.
func (l *htmlLayout) build() {
	l.built = true
	// the laid-out blocks by key, each kept once: identical blocks share a key, and each takes a
	// layout of its own, never another's (their boxes hold their places)
	old := map[uint64][]*topBlock{}
	for _, b := range l.blocks {
		if b.laid {
			old[b.key] = append(old[b.key], b)
		}
	}
	doc, err := phtml.Parse(l.src)
	if err != nil {
		doc, _ = phtml.Parse([]byte("<p><i>this page could not be shown: " + escapeText(err.Error()) + "</i></p>"))
	}
	l.doc = doc
	l.parents = map[*phtml.Node]*phtml.Node{}
	// the window's theme colours a page with no stylesheet of its own; a page that styles itself
	// is drawn as its author wrote it (a light page in a dark window stays light, its code unchipped)
	var author strings.Builder
	authored := false
	linked := 0
	walkNodes(doc, nil, func(n, parent *phtml.Node) {
		l.parents[n] = parent
		if n.Kind != phtml.StartTag && n.Kind != phtml.SelfClosing {
			return
		}
		// a refused image is told for each page that names it: a block reused from an earlier
		// page does not look its images up again, so the page's build does
		if n.Name == "img" {
			if src, _ := n.Attr("src"); src != "" {
				if e, ok := l.images.entries[strings.TrimSpace(src)]; ok && e.refused {
					l.v.Refused(strings.TrimSpace(src))
				}
			}
		}
		if n.Name == "style" {
			for _, c := range n.Children {
				author.WriteString("\n" + c.Data)
				authored = authored || strings.TrimSpace(c.Data) != ""
			}
			return
		}
		// a linked sheet in its place in the cascade, once it has loaded
		if href, ok := sheetLink(n); ok && linked < maxSheets {
			linked++
			authored = true // a sheet still loading counts: the page styles itself
			if e := l.sheets.entry(href); e.state == imgReady {
				media, _ := n.Attr("media")
				author.WriteString("\n" + mediaWrapped(e.text, media))
			}
		}
	})
	var sheets strings.Builder
	sheets.WriteString(uaCSS)
	if !authored {
		sheets.WriteString(l.themeCSS())
	}
	sheets.WriteString(author.String())
	sheets.WriteString("\n" + l.v.Stylesheet())
	css := sheets.String()
	order := 0
	l.styler = &styler{rules: parseCSS(css, &order, mediaEnv{w: l.width, h: l.viewH, dark: isDark(l.bg)}), rootPx: l.rootPx, view: l.view()}
	l.css = hashText(css)

	// rem is the root element's size, which starts from the window's text: never the last build's,
	// so html{font-size:2rem} is twice the text however often the page is built
	l.rootPx = l.textPx
	l.styler.rootPx = l.textPx
	root := &computed{color: l.fg, fontSize: l.textPx, lineHeight: 1.2, display: displayBlock}
	htmlNode, bodyNode := findElement(doc, "html"), findElement(doc, "body")
	chain := []*phtml.Node{}
	st := root
	if htmlNode != nil {
		chain = append(chain, htmlNode)
		st = l.styler.style(htmlNode, root, chain)
		l.rootPx = st.fontSize
		l.styler.rootPx = st.fontSize
	}
	content := doc
	if bodyNode != nil {
		chain = append(chain, bodyNode)
		st = l.styler.style(bodyNode, st, chain)
		content = bodyNode
	} else if htmlNode != nil {
		content = htmlNode
	}
	l.body = st
	l.bodyE = l.edgesOf(st, l.width)

	// the top-level blocks: each block-level child of the body, and each run of inline content
	// between them
	l.blocks = l.blocks[:0]
	l.abs = l.abs[:0]
	var pending []*phtml.Node
	flushInline := func() {
		text, shows := 0, false
		for _, n := range pending {
			text += n.Span[1] - n.Span[0]
			shows = shows || n.Kind != phtml.Text || strings.TrimSpace(n.Data) != ""
		}
		if shows {
			l.blocks = append(l.blocks, &topBlock{inline: pending, src: [2]int{-1, -1}, textLen: text})
		}
		pending = nil
	}
	for _, c := range content.Children {
		if c.Kind == phtml.Text {
			pending = append(pending, c)
			continue
		}
		if c.Kind != phtml.StartTag && c.Kind != phtml.SelfClosing || xhtml.Dropped(c) {
			continue // a dropped element (a form, an iframe, an svg, aria-hidden) at the top too
		}
		cs := l.styler.style(c, st, append(chain, c))
		if cs.display == displayNone {
			continue
		}
		if cs.position == positionAbsolute {
			flushInline()
			l.abs = append(l.abs, &topBlock{node: c, src: spanOf(c), after: len(l.blocks)})
			continue
		}
		if !blockLevel(cs.display) {
			pending = append(pending, c)
			continue
		}
		flushInline()
		l.blocks = append(l.blocks, &topBlock{node: c, src: spanOf(c), textLen: c.Span[1] - c.Span[0]})
	}
	flushInline()

	contentW := l.contentWidth()
	for _, b := range l.blocks {
		b.key = l.keyOf(b, contentW)
		if olds := old[b.key]; len(olds) > 0 {
			o := olds[0]
			old[b.key] = olds[1:]
			b.box, b.h, b.mTop, b.mBot, b.laid = o.box, o.h, o.mTop, o.mBot, true
		}
	}
	l.place()
}

// themeCSS gives a page with no colours of its own the window's: links in the accent, code and
// pre on the surface, quotes and rules muted.
func (l *htmlLayout) themeCSS() string {
	hex := func(c color.NRGBA) string {
		return "#" + hex2(c.R) + hex2(c.G) + hex2(c.B)
	}
	return "\na{color:" + hex(l.accent) + "}code,pre{background:" + hex(l.surface) + "}" +
		"pre{padding:.5em .75em}blockquote,.callout{border-left:3px solid " + hex(l.muted) + "}" +
		"blockquote{color:" + hex(l.muted) + "}hr{border-top:1px solid " + hex(l.muted) + "}" +
		"td,th{border:1px solid " + hex(l.muted) + "}table{border-collapse:collapse}\n"
}

// hex2 formats a byte as a two-character lowercase hexadecimal string.
func hex2(b uint8) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[b>>4], digits[b&15]})
}

// escapeText escapes HTML entities (&, <, >) in plain text.
func escapeText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// hashText computes an FNV-64a hash of string s.
func hashText(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// keyOf is a top-level block's key: its source bytes, the width, the stylesheets, the look and
// the images it holds, as they stand.
func (l *htmlLayout) keyOf(b *topBlock, width float32) uint64 {
	h := fnv.New64a()
	write := func(n *phtml.Node) {
		if n.Span[1] <= len(l.src) && n.Span[0] <= n.Span[1] {
			_, _ = h.Write(l.src[n.Span[0]:n.Span[1]])
		}
	}
	if b.node != nil {
		write(b.node)
	}
	for _, n := range b.inline {
		write(n)
	}
	var buf [8]byte
	for _, v := range []uint64{uint64(math.Float32bits(width)), l.css, l.look} {
		for i := range 8 {
			buf[i] = byte(v >> (8 * i))
		}
		_, _ = h.Write(buf[:])
	}
	nodes := b.inline
	if b.node != nil {
		nodes = []*phtml.Node{b.node}
	}
	diagram := func(n *phtml.Node) {
		if dg, ok := diagramSource(n, l); ok {
			// its answer's state, and which Diagrammer gives it: another one is asked again
			for _, g := range []uint64{l.diagrams.genOf(dg), l.v.DiagramsSet()} {
				for i := range 8 {
					buf[i] = byte(g >> (8 * i))
				}
				_, _ = h.Write(buf[:])
			}
		}
	}
	for _, n := range nodes {
		if n.Kind == phtml.StartTag && n.Name == "details" && l.detailsOpen(n) {
			_, _ = h.Write([]byte{'o'}) // the block itself is a <details>: walkNodes visits below it
		}
		diagram(n) // the block itself may be the diagram's pre
		walkNodes(n, nil, func(c, _ *phtml.Node) {
			if c.Kind == phtml.StartTag || c.Kind == phtml.SelfClosing {
				if c.Name == "img" {
					src, _ := c.Attr("src")
					_, _ = h.Write([]byte{byte(l.images.gen(src))})
				}
				diagram(c)
				if c.Name == "details" && l.detailsOpen(c) {
					_, _ = h.Write([]byte{'o'})
				}
			}
		})
	}
	return h.Sum64()
}
