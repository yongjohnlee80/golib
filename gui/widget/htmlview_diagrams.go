package widget

import (
	"context"
	"strings"
	"sync"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	phtml "github.com/yongjohnlee80/golib/parse/html"
	"github.com/yongjohnlee80/golib/tui"
)

// DIAGRAMS — a page's <pre class="mermaid">, drawn by the view's Diagrammer (tui/widget's
// WithDiagrams), as the editor's Rendered view draws a mermaid fence: Ready is its picture, in
// the pre's place; Pending keeps the last picture, or the code until there is one; Declined, or
// no Diagrammer, is the code.

// htmlDiagram is one diagram's request: what was asked, under which generation, and its last
// picture.
type htmlDiagram struct {
	key     diagramKey
	gen     uint64
	cancel  context.CancelFunc
	state   DiagramState
	refresh bool // ready was called: ask again on the next build
	pic     gui.View
	size    gui.Size
}

// htmlDiagramReady is a request's ready, brought to the loop as a task result.
type htmlDiagramReady struct {
	src string
	gen uint64
}

// maxHTMLDiagrams bounds the slots a view keeps: each holds a picture.
const maxHTMLDiagrams = 64

type htmlDiagrams struct {
	l     *htmlLayout
	slots map[string]*htmlDiagram // by the diagram's source
	gen   uint64
	tasks map[tui.TaskID]struct{} // ready waits started for this layout: their results are ours
}

func newHTMLDiagrams(l *htmlLayout) *htmlDiagrams {
	return &htmlDiagrams{l: l, slots: map[string]*htmlDiagram{}, tasks: map[tui.TaskID]struct{}{}}
}

// mermaidSource is a pre's diagram source when it is a <pre class="mermaid">: its text, as the
// page holds it.
func mermaidSource(n *phtml.Node) (string, bool) {
	if n.Name != "pre" {
		return "", false
	}
	class, _ := n.Attr("class")
	if !strings.Contains(" "+strings.Join(strings.Fields(class), " ")+" ", " mermaid ") {
		return "", false
	}
	var b strings.Builder
	walkNodes(n, nil, func(c, _ *phtml.Node) {
		if c.Kind == phtml.Text {
			b.WriteString(c.Data)
		}
	})
	return strings.TrimSpace(b.String()), true
}

// theme is the diagrams' theme from the page's colours.
func (m *htmlDiagrams) theme() Theme {
	l := m.l
	return Theme{Text: l.fg, Background: l.bg, Accent: l.accent, Muted: l.muted, CodeBackground: l.surface,
		Prose: gui.Font{Size: l.textPx}, Mono: gui.Font{Size: l.textPx, Family: gui.MonospaceFamily()}}
}

// answer is src's diagram at width: asked of the view's Diagrammer on first sight, and again once
// its ready fires or its width, scale or theme changes.
func (m *htmlDiagrams) answer(src string, width float32) DiagramAnswer {
	d, _ := m.l.v.Diagrams().(Diagrammer)
	if d == nil || m.l.shaper == nil {
		return DiagramAnswer{State: Declined}
	}
	req := DiagramRequest{Lang: "mermaid", Src: src, Width: width, Scale: m.l.shaper.Scale(), Theme: m.theme()}
	key := diagramKey{req.Lang, req.Src, req.Width, req.Scale, req.Theme}
	s := m.slots[src]
	if s != nil && s.key == key && !s.refresh {
		return s.answerOf()
	}
	var pic gui.View
	var size gui.Size
	if s != nil {
		s.cancel()
		pic, size = s.pic, s.size
	}
	if len(m.slots) >= maxHTMLDiagrams { // a page edited for long: forget the others, kept by the chain's cache
		m.reset()
	}
	m.gen++
	ctx, cancel := context.WithCancel(context.Background())
	s = &htmlDiagram{key: key, gen: m.gen, cancel: cancel, state: Pending, pic: pic, size: size}
	m.slots[src] = s
	a := d.Diagram(ctx, req, m.readyFor(ctx, src, s.gen))
	switch a.State {
	case Ready:
		s.state, s.pic, s.size = Ready, a.Pic, a.Size
		cancel()
	case Declined:
		s.state = Declined
		cancel()
	case Pending:
		if a.Pic != nil {
			s.pic, s.size = a.Pic, a.Size
		}
	}
	return s.answerOf()
}

func (s *htmlDiagram) answerOf() DiagramAnswer {
	if s.state == Declined {
		return DiagramAnswer{State: Declined}
	}
	return DiagramAnswer{State: s.state, Pic: s.pic, Size: s.size}
}

// genOf is a diagram's state as a number, for its block's key: a ready that fires, or a picture
// that lands, changes it, so the block is laid out again.
func (m *htmlDiagrams) genOf(src string) uint64 {
	s, ok := m.slots[src]
	if !ok {
		return 0
	}
	refresh := uint64(0)
	if s.refresh {
		refresh = 1
	}
	return s.gen<<3 | refresh<<2 | uint64(s.state)
}

// readyFor is a request's ready: called at most once, from any goroutine, it wakes a task on the
// view's Context that brings htmlDiagramReady to the loop. A cancelled request (a newer one, a
// reset) or an unmounted view ends the task with no result.
func (m *htmlDiagrams) readyFor(ctx context.Context, src string, gen uint64) func() {
	tc := m.l.v.Context()
	if tc == nil {
		return func() {}
	}
	fired := make(chan struct{})
	var once sync.Once
	id := tc.Go(func(task context.Context) (any, error) {
		select {
		case <-fired:
			return htmlDiagramReady{src: src, gen: gen}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-task.Done():
			return nil, task.Err()
		}
	})
	m.tasks[id] = struct{}{}
	return func() { once.Do(func() { close(fired) }) }
}

// done takes a ready on the loop; it reports whether the result was a diagram's. A current
// generation marks its slot to be asked again; an older one is dropped.
func (m *htmlDiagrams) done(r tui.TaskResult) bool {
	if _, ok := m.tasks[r.ID]; !ok {
		return false
	}
	delete(m.tasks, r.ID)
	if v, ok := r.Value.(htmlDiagramReady); ok && r.Err == nil {
		if s := m.slots[v.src]; s != nil && s.gen == v.gen {
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
	m.slots = map[string]*htmlDiagram{}
}

// diagramBox is a <pre class="mermaid">'s box when its picture is drawn: one run holding the
// picture as an inline box, fitted to width. False keeps the code.
func (b *builder) diagramBox(bx *box, n *phtml.Node, st *computed, src [2]int) bool {
	text, ok := mermaidSource(n)
	if !ok || text == "" {
		return false
	}
	a := b.l.diagrams.answer(text, b.l.contentWidth())
	if a.State == Declined || a.Pic == nil {
		return false
	}
	pic, size := a.Pic, a.Size
	span := flow.Span{Atom: &flow.Atom{W: size.W, H: size.H, Baseline: size.H, Paint: func(c gui.Canvas) { pic.Paint(c) }},
		Line: -1, Src: src}
	bx.kids = []*box{{kind: kRun, st: st, src: src, spans: []flow.Span{span}}}
	return true
}
