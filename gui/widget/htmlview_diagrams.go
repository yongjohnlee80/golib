package widget

import (
	"context"
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	phtml "github.com/yongjohnlee80/golib/parse/html"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// DIAGRAMS — a page's <pre class="mermaid"> and <pre class="vega-lite">, drawn by the view's
// Diagrammer (tui/widget's WithDiagrams), as the editor's Rendered view draws a mermaid or
// vega-lite fence: Ready is its picture, in the pre's place; Pending keeps the last picture, or
// the code until there is one; Declined, or no Diagrammer, is the code.
//
// # HTML Mermaid Diagram Lifecycle Architecture
//
// Below is the lifecycle and task handoff pipeline for HTML page diagrams:
//
//	  <pre class="mermaid|vega-lite"> discovered in page DOM
//	                       │
//	                       ▼
//	          htmlDiagrams.answer(dg, width)
//	                       │
//	        ┌──────────────┴──────────────┐
//	        │  Cached & !refresh?         │─── Yes ───► Return s.answerOf()
//	        └──────────────┬──────────────┘
//	                       │ No (Dirty / Stale)
//	                       ▼
//	          Cancel prior context, gen++
//	          d.Diagram(ctx, req, readyFor)
//	                       │
//	          ┌────────────┴────────────┐
//	          ▼                         ▼
//	       Ready / Declined          Pending
//	       (Store result)      (Keep prior pic)
//	                                    │
//	                               ready() fires
//	                                    ▼
//	                           tc.Go Task Result
//	                                    ▼
//	                           htmlDiagrams.done()
//	                                    ▼
//	                           s.refresh = true -> rebuild page

// htmlDiagramReady is a request's ready, brought to the loop as a task result.
type htmlDiagramReady struct {
	dg  pageDiagram
	gen uint64
}

// WithHTMLDiagrams gives an HTMLView the Diagrammer that draws its pages' diagrams (a pre of class
// mermaid or vega-lite), as tui/widget's WithDiagrams does, typed: that one holds any value, and
// one that is not a Diagrammer draws every diagram as its source.
func WithHTMLDiagrams(d Diagrammer) tuiwidget.HTMLOption { return tuiwidget.WithDiagrams(d) }

// pageDiagram is a pre drawn as a picture: its language (mermaid, vega-lite) and its source.
type pageDiagram struct{ lang, src string }

// maxHTMLDiagrams bounds the slots a view keeps: each holds a picture.
const maxHTMLDiagrams = 64

type htmlDiagrams struct {
	l     *htmlLayout
	slots map[pageDiagram]*diagramSlot
	set   uint64 // the view's DiagramsSet the slots were asked under
	gen   uint64
	tasks map[tui.TaskID]struct{} // ready waits started for this layout: their results are ours
}

// newHTMLDiagrams constructs the diagram manager for an HTML layout instance.
func newHTMLDiagrams(l *htmlLayout) *htmlDiagrams {
	return &htmlDiagrams{l: l, slots: map[pageDiagram]*diagramSlot{}, tasks: map[tui.TaskID]struct{}{}}
}

// diagramSource is a pre's diagram when it is a <pre class="mermaid"> or a <pre
// class="vega-lite">: its language, and its text as the page holds it.
func diagramSource(n *phtml.Node) (pageDiagram, bool) {
	if n.Name != "pre" {
		return pageDiagram{}, false
	}
	class, _ := n.Attr("class")
	lang := ""
	for _, c := range strings.Fields(class) {
		if l, ok := diagramLangs[c]; ok { // the pre classes drawn as pictures, as a fence's languages
			lang = l
			break
		}
	}
	if lang == "" {
		return pageDiagram{}, false
	}
	var b strings.Builder
	walkNodes(n, nil, func(c, _ *phtml.Node) {
		if c.Kind == phtml.Text {
			b.WriteString(c.Data)
		}
	})
	return pageDiagram{lang: lang, src: strings.TrimSpace(b.String())}, true
}

// theme is the diagrams' theme from the page's colours.
func (m *htmlDiagrams) theme() Theme {
	l := m.l
	return Theme{Text: l.fg, Background: l.bg, Accent: l.accent, Muted: l.muted, CodeBackground: l.surface,
		Prose: gui.Font{Size: l.textPx}, Mono: gui.Font{Size: l.textPx, Family: gui.CellFamily}}
}

// answer is dg's picture at width: asked of the view's Diagrammer on first sight, and again once
// its ready fires or its width, scale or theme changes.
func (m *htmlDiagrams) answer(dg pageDiagram, width float32) DiagramAnswer {
	d, _ := m.l.v.Diagrams().(Diagrammer)
	if d == nil || m.l.shaper == nil {
		return DiagramAnswer{State: Declined}
	}
	if set := m.l.v.DiagramsSet(); set != m.set { // WithDiagrams again: the new Diagrammer's answers
		m.reset()
		m.set = set
	}
	req := DiagramRequest{Lang: dg.lang, Src: dg.src, Width: width, Scale: m.l.shaper.Scale(), Theme: diagramFonts(m.theme(), m.l.shaper.Fonts())}
	s := m.slots[dg]
	if s.current(keyOf(req)) {
		return s.answer()
	}
	if len(m.slots) >= maxHTMLDiagrams { // a page edited for long: forget the others, kept by the chain's cache
		m.reset() // s's picture still stands in for the new request's
	}
	m.gen++
	gen := m.gen
	s = askAfter(s, d, req, gen, func(ctx context.Context) func() {
		ready, id, ok := readyTask(m.l.v.Context(), ctx, htmlDiagramReady{dg: dg, gen: gen})
		if ok {
			m.tasks[id] = struct{}{}
		}
		return ready
	})
	m.slots[dg] = s
	return s.answer()
}

// genOf is a diagram's state as a number, for its block's key: a ready that fires, or a picture
// that lands, changes it, so the block is laid out again.
func (m *htmlDiagrams) genOf(dg pageDiagram) uint64 {
	s, ok := m.slots[dg]
	if !ok {
		return 0
	}
	refresh := uint64(0)
	if s.refresh {
		refresh = 1
	}
	return s.gen<<3 | refresh<<2 | uint64(s.state)
}

// done takes a ready on the loop; it reports whether the result was a diagram's. A current
// generation marks its slot to be asked again; an older one is dropped.
func (m *htmlDiagrams) done(r tui.TaskResult) bool {
	if _, ok := m.tasks[r.ID]; !ok {
		return false
	}
	delete(m.tasks, r.ID)
	if v, ok := r.Value.(htmlDiagramReady); ok && r.Err == nil {
		if s := m.slots[v.dg]; s != nil && s.gen == v.gen {
			s.refresh = true
		}
	}
	return true
}

// reset withdraws every request: a new page.
func (m *htmlDiagrams) reset() {
	for _, s := range m.slots {
		s.cancel()
	}
	m.slots = map[pageDiagram]*diagramSlot{}
}

// diagramBox is a diagram pre's box when its picture is drawn: one run holding the picture as an
// inline box, fitted to width. False keeps the code.
func (b *builder) diagramBox(bx *box, n *phtml.Node, st *computed, src [2]int) bool {
	dg, ok := diagramSource(n)
	if !ok || dg.src == "" {
		return false
	}
	a := b.l.diagrams.answer(dg, b.l.contentWidth())
	if a.State == Declined || a.Pic == nil {
		return false
	}
	pic, size := a.Pic, a.Size
	span := flow.Span{Atom: &flow.Atom{W: size.W, H: size.H, Baseline: size.H, Paint: func(c gui.Canvas) { pic.Paint(c) }},
		Line: -1, Src: src}
	bx.kids = []*box{{kind: kRun, st: st, src: src, spans: []flow.Span{span}}}
	return true
}
