package widget

import (
	"strconv"
	"strings"
	"unicode"

	xhtml "github.com/yongjohnlee80/golib/extract/html"
	phtml "github.com/yongjohnlee80/golib/parse/html"
)

// THE CELL LAYOUT'S DOCUMENT — parse/html's tree read into blocks of styled spans, for an
// HTMLView drawn in a terminal. What a page drops (scripts, styles, anything hidden) and where a
// block starts are extract/html's rules, so the view and the extractor read a page alike; unlike
// the extractor, the view keeps link targets, on the spans of their text.

// hspan is a run of text in one inline style. A deco span is drawn but never part of the
// document's text: a list marker, a table's cell separator, padding. Copying a selection across
// one gives copy instead (a tab between table cells).
type hspan struct {
	text                              string
	bold, italic, code, under, strike bool
	link                              string
	deco                              bool
	copy                              string
}

type hblockKind uint8

const (
	hPara hblockKind = iota
	hHeading
	hPre
	hRule
	hRow // a table row: its cells' spans, cellStarts marking where each cell begins
)

// hblock is one block of the document: a paragraph, a heading, preformatted text, a rule or a
// table row, with the quotes and list it sits in.
type hblock struct {
	kind       hblockKind
	level      int    // a heading's
	quote      int    // blockquote and callout depth: a bar each
	indent     int    // cells a list item's content is indented by
	marker     string // a list item's, drawn hanging before its first row
	gap        bool   // a blank row before it
	src        [2]int // the source bytes it shows (data-src="from-to"); {-1, -1} when unknown
	table      int    // a row's table, so its columns align with its siblings'
	cellStarts []int  // a row's: the span each cell starts at
	spans      []hspan
}

// inlineStyle is the inline formatting in force at a point of the walk.
type inlineStyle struct {
	bold, italic, code, under, strike bool
	link                              string
}

type hlist struct {
	ordered bool
	n       int // the number the last item had
	items   int // how many items it has had
}

type hwalker struct {
	blocks   []hblock
	open     bool // blocks[len-1] takes inline content
	st       inlineStyle
	pre      int
	quote    int
	lists    []hlist
	marker   string // the next block opened is a list item's first: it takes the marker
	firstRow bool   // the next row opened starts its table
	src      [2]int
	tables   int
	cell     int  // inside a table cell: block elements are read inline, so they never end the row
	space    bool // a collapsed space is owed: written before the next text of the open block
}

// readHTMLDoc reads src into blocks. A page parse/html refuses (over its limits) is one paragraph
// saying why.
func readHTMLDoc(src []byte) []hblock {
	doc, err := phtml.Parse(src)
	if err != nil {
		return []hblock{{kind: hPara, src: [2]int{-1, -1}, spans: []hspan{{text: "this page could not be shown: " + err.Error(), italic: true}}}}
	}
	w := &hwalker{src: [2]int{-1, -1}}
	w.walk(doc)
	w.close()
	return w.blocks
}

// dataSrc reads data-src="from-to".
func dataSrc(n *phtml.Node) ([2]int, bool) {
	v, ok := n.Attr("data-src")
	if !ok {
		return [2]int{}, false
	}
	a, b, ok := strings.Cut(v, "-")
	if !ok {
		return [2]int{}, false
	}
	from, err1 := strconv.Atoi(strings.TrimSpace(a))
	to, err2 := strconv.Atoi(strings.TrimSpace(b))
	if err1 != nil || err2 != nil || from < 0 || to < from {
		return [2]int{}, false
	}
	return [2]int{from, to}, true
}

func hasClass(n *phtml.Node, class string) bool {
	v, _ := n.Attr("class")
	for _, c := range strings.Fields(v) {
		if c == class {
			return true
		}
	}
	return false
}

func (w *hwalker) listIndent() int { return 3 * len(w.lists) }

// block opens a new block of kind, closing the open one.
func (w *hwalker) block(kind hblockKind) *hblock {
	w.close()
	b := hblock{kind: kind, quote: w.quote, indent: w.listIndent(), src: w.src, gap: true}
	switch {
	case w.marker != "":
		// a list follows the block before it after a blank row; its items, and the lists nested
		// in them, follow one another without one
		b.marker, w.marker = w.marker, ""
		b.gap = len(w.lists) == 1 && w.lists[0].items == 1
	case len(w.lists) > 0:
		b.gap = false
	}
	if len(w.blocks) == 0 {
		b.gap = false
	}
	w.blocks = append(w.blocks, b)
	w.open = kind != hRule
	return &w.blocks[len(w.blocks)-1]
}

// close ends the open block: a trailing collapsed space goes, and a block left empty is dropped
// (an empty paragraph shows nothing), unless it is a list item's, whose marker still shows.
func (w *hwalker) close() {
	if !w.open {
		return
	}
	w.open = false
	b := &w.blocks[len(w.blocks)-1]
	if w.pre == 0 && len(b.spans) > 0 {
		last := &b.spans[len(b.spans)-1]
		if !last.deco {
			last.text = strings.TrimRight(last.text, " ")
			if last.text == "" {
				b.spans = b.spans[:len(b.spans)-1]
			}
		}
	}
	if len(b.spans) == 0 && b.marker == "" && b.kind != hRow {
		w.blocks = w.blocks[:len(w.blocks)-1]
	}
}

// inline gives the open block, opening a paragraph for text outside any.
func (w *hwalker) inline() *hblock {
	if !w.open {
		kind := hPara
		if w.pre > 0 {
			kind = hPre
		}
		w.block(kind)
	}
	return &w.blocks[len(w.blocks)-1]
}

// owed settles the collapsed space owed before the next text, when the open block already has
// text: it ends that text when that is plain, else it starts the next one, so a link, a code
// span or a struck word never ends in an underlined or shaded space. It answers what the next
// text starts with. At a block's start the space shows nothing.
func (w *hwalker) owed() string {
	owe := w.space && w.open && len(w.blocks[len(w.blocks)-1].spans) > 0
	w.space = false
	if !owe {
		return ""
	}
	b := &w.blocks[len(w.blocks)-1]
	if last := &b.spans[len(b.spans)-1]; !last.deco && last.link == "" && !last.code && !last.under && !last.strike {
		last.text += " "
		return ""
	}
	return " "
}

// text adds character data in the inline style in force. Outside pre, each run of white space
// collapses to one space, written only before more text of the same block, as a browser does.
func (w *hwalker) text(s string) {
	if w.pre > 0 {
		w.add(hspan{text: s})
		return
	}
	var sb strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			w.space = true
			continue
		}
		if w.space {
			if sb.Len() > 0 {
				sb.WriteByte(' ')
				w.space = false
			} else {
				sb.WriteString(w.owed())
			}
		}
		sb.WriteRune(r)
	}
	if sb.Len() > 0 {
		w.add(hspan{text: sb.String()})
	}
}

// add appends sp in the inline style in force, merging with the span before when they match.
func (w *hwalker) add(sp hspan) {
	b := w.inline()
	if !sp.deco {
		st := w.st
		sp.bold, sp.italic, sp.code, sp.under, sp.strike, sp.link = st.bold, st.italic, st.code, st.under, st.strike, st.link
	}
	if n := len(b.spans); n > 0 && !sp.deco {
		last := &b.spans[n-1]
		if !last.deco && last.bold == sp.bold && last.italic == sp.italic && last.code == sp.code &&
			last.under == sp.under && last.strike == sp.strike && last.link == sp.link {
			last.text += sp.text
			return
		}
	}
	b.spans = append(b.spans, sp)
}

var bullets = []string{"•", "◦", "▪"}

// inlineInCell are the elements a table cell reads as themselves; anything else in a cell is
// read as inline content.
var inlineInCell = map[string]bool{
	"a": true, "b": true, "strong": true, "i": true, "em": true, "cite": true, "dfn": true, "var": true,
	"code": true, "kbd": true, "samp": true, "tt": true, "u": true, "ins": true, "s": true, "del": true,
	"strike": true, "br": true, "img": true, "span": true, "small": true, "sub": true, "sup": true,
	"mark": true, "abbr": true, "time": true, "q": true, "label": true,
}

func (w *hwalker) walk(n *phtml.Node) {
	for _, c := range n.Children {
		w.node(c)
	}
}

func (w *hwalker) node(n *phtml.Node) {
	switch n.Kind {
	case phtml.Text:
		w.text(n.Data)
		return
	case phtml.StartTag, phtml.SelfClosing:
	default:
		return // comments, doctypes
	}
	if xhtml.Dropped(n) {
		return
	}
	if r, ok := dataSrc(n); ok {
		saved := w.src
		w.src = r
		defer func() { w.src = saved }()
	}
	st := w.st
	defer func() { w.st = st }()
	if w.cell > 0 && !inlineInCell[n.Name] {
		// a block inside a table cell is read inline, a space before it, so the row holds
		w.space = true
		w.walk(n)
		w.space = true
		return
	}
	switch n.Name {
	case "br":
		if w.pre > 0 || w.open {
			w.add(hspan{text: "\n"})
			w.space = false
		}
	case "img":
		alt, _ := n.Attr("alt")
		if alt = strings.TrimSpace(alt); alt == "" {
			alt = "image"
		}
		w.st.italic = true
		w.add(hspan{text: w.owed() + "[" + alt + "]"})
	case "a":
		if href, ok := n.Attr("href"); ok {
			w.st.link = href
		}
		w.walk(n)
	case "b", "strong":
		w.st.bold = true
		w.walk(n)
	case "i", "em", "cite", "dfn", "var":
		w.st.italic = true
		w.walk(n)
	case "code", "kbd", "samp", "tt":
		w.st.code = true
		w.walk(n)
	case "u", "ins":
		w.st.under = true
		w.walk(n)
	case "s", "del", "strike":
		w.st.strike = true
		w.walk(n)
	case "h1", "h2", "h3", "h4", "h5", "h6":
		b := w.block(hHeading)
		b.level = int(n.Name[1] - '0')
		w.walk(n)
		w.close()
	case "pre":
		w.block(hPre)
		w.pre++
		w.walk(n)
		w.pre--
		if b := &w.blocks[len(w.blocks)-1]; w.open && len(b.spans) > 0 {
			// the newline before </pre> ends the last line, it is not an empty one
			last := &b.spans[len(b.spans)-1]
			last.text = strings.TrimSuffix(last.text, "\n")
		}
		w.close()
	case "blockquote":
		w.close()
		w.quote++
		w.walk(n)
		w.close()
		w.quote--
	case "div":
		switch {
		case hasClass(n, "callout"):
			w.close()
			w.quote++
			w.walk(n)
			w.close()
			w.quote--
		case hasClass(n, "callout-title"):
			w.block(hPara)
			w.st.bold = true
			w.walk(n)
			w.close()
		default:
			w.close()
			w.walk(n)
			w.close()
		}
	case "ul", "ol", "menu":
		w.close()
		l := hlist{ordered: n.Name == "ol"}
		if v, ok := n.Attr("start"); ok && l.ordered {
			if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				l.n = s - 1
			}
		}
		w.lists = append(w.lists, l)
		w.walk(n)
		w.close()
		w.lists = w.lists[:len(w.lists)-1]
		w.marker = ""
	case "li":
		w.close()
		if len(w.lists) == 0 {
			w.lists = append(w.lists, hlist{})
			defer func() { w.lists = w.lists[:len(w.lists)-1] }()
		}
		l := &w.lists[len(w.lists)-1]
		l.n++
		l.items++
		if l.ordered {
			w.marker = strconv.Itoa(l.n) + "."
		} else {
			w.marker = bullets[(len(w.lists)-1)%len(bullets)]
		}
		w.walk(n)
		if w.marker != "" {
			// an item with no text still shows its marker
			w.block(hPara)
		}
		w.close()
	case "hr":
		w.block(hRule)
	case "table":
		w.close()
		w.tables++
		w.firstRow = true
		w.walk(n)
		w.close()
	case "tr":
		b := w.block(hRow)
		b.table = w.tables
		b.gap = w.firstRow
		w.firstRow = false
		w.walk(n)
		w.close()
	case "td", "th":
		if !w.open || w.blocks[len(w.blocks)-1].kind != hRow {
			w.walk(n) // a cell outside a row is read as inline content
			return
		}
		b := &w.blocks[len(w.blocks)-1]
		if len(b.cellStarts) > 0 {
			b.spans = append(b.spans, hspan{text: " │ ", deco: true, copy: "\t"})
		}
		b.cellStarts = append(b.cellStarts, len(b.spans))
		w.space = false
		if n.Name == "th" {
			w.st.bold = true
		}
		w.cell++
		w.walk(n)
		w.cell--
		if n := len(b.spans); n > 0 && !b.spans[n-1].deco {
			b.spans[n-1].text = strings.TrimRight(b.spans[n-1].text, " ")
		}
	default:
		if xhtml.Block(n.Name) {
			w.close()
			w.walk(n)
			w.close()
			return
		}
		w.walk(n)
	}
}
