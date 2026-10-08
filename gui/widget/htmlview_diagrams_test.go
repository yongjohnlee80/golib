package widget

import (
	"context"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// htmlFakeDiagrams answers every request with state, and a picture that counts its paints.
type htmlFakeDiagrams struct {
	state  DiagramState
	asked  []DiagramRequest
	paints int
}

func (f *htmlFakeDiagrams) Diagram(_ context.Context, req DiagramRequest, _ func()) DiagramAnswer {
	f.asked = append(f.asked, req)
	if f.state != Ready {
		return DiagramAnswer{State: f.state}
	}
	return DiagramAnswer{State: Ready, Size: gui.Size{W: 120, H: 60},
		Pic: gui.ViewFunc(func(gui.Canvas) { f.paints++ })}
}

// diagramView is a pixel HTMLView of html with d as its Diagrammer, painted once.
func diagramView(t *testing.T, html string, d Diagrammer) (*htmlLayout, func()) {
	t.Helper()
	var opts []tuiwidget.HTMLOption
	if d != nil {
		opts = append(opts, tuiwidget.WithDiagrams(d))
	}
	v := tuiwidget.NewHTMLView(opts...)
	v.SetHTML([]byte(html))
	BindHTML(v)
	l := v.BoundLayout().(*htmlLayout)
	paint := func() {
		c := gui.NewRecordingCanvas(gui.Size{W: 600, H: 400}, gui.Size{W: 8, H: 19.2})
		c.TextPx = 16
		c.Colors = func(int, int) (color.NRGBA, color.NRGBA) {
			return color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}, color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff}
		}
		l.Paint(c)
	}
	paint()
	return l, paint
}

// preDrawn reports whether the page's pre holds the diagram's picture rather than its code.
func preDrawn(l *htmlLayout) bool {
	pre := l.boxOf("pre")
	return pre != nil && len(pre.kids) == 1 && len(pre.kids[0].spans) == 1 && pre.kids[0].spans[0].Atom != nil
}

const mermaidPage = "<p>before</p><pre class=\"mermaid\">flowchart LR\n  A --&gt; B</pre><p>after</p>"

// A <pre class="mermaid"> asks the view's Diagrammer with its text, at the page's width; a Ready
// picture takes the pre's place and is painted.
func TestAMermaidPreDrawsItsDiagram(t *testing.T) {
	f := &htmlFakeDiagrams{state: Ready}
	l, _ := diagramView(t, mermaidPage, f)
	if len(f.asked) == 0 || f.asked[0].Src != "flowchart LR\n  A --> B" || f.asked[0].Lang != "mermaid" || f.asked[0].Width <= 0 {
		t.Fatalf("asked %+v, want the pre's text, decoded, at the page's width", f.asked)
	}
	if !preDrawn(l) || f.paints == 0 {
		t.Errorf("the picture is not in the pre's place (drawn %v, painted %d times)", preDrawn(l), f.paints)
	}
}

// Declined, or no Diagrammer, keeps the code; a pre that is not a diagram is never asked.
func TestAMermaidPreKeepsItsCodeWhenNotDrawn(t *testing.T) {
	declined := &htmlFakeDiagrams{state: Declined}
	if l, _ := diagramView(t, mermaidPage, declined); preDrawn(l) {
		t.Error("a declined diagram's pre lost its code")
	}
	if l, _ := diagramView(t, mermaidPage, nil); preDrawn(l) {
		t.Error("a view with no Diagrammer drew a picture")
	}
	f := &htmlFakeDiagrams{state: Ready}
	if l, _ := diagramView(t, "<pre class=\"go\">x := 1</pre><pre>plain</pre>", f); len(f.asked) != 0 || preDrawn(l) {
		t.Errorf("a pre that is not mermaid was asked %d times", len(f.asked))
	}
}

// Pending keeps the code; its ready, on the loop, asks again and lays the block out anew, now
// with the picture. A ready of an older request is swallowed.
func TestAMermaidPreDrawsOnceReady(t *testing.T) {
	f := &htmlFakeDiagrams{state: Pending}
	l, paint := diagramView(t, mermaidPage, f)
	if preDrawn(l) {
		t.Fatal("a pending diagram drew a picture")
	}
	s := l.diagrams.slots["flowchart LR\n  A --> B"]
	if s == nil {
		t.Fatal("no request for the diagram")
	}
	l.diagrams.tasks[41] = struct{}{}
	if !l.HandleTask(tui.TaskResult{ID: 41, Value: htmlDiagramReady{src: "flowchart LR\n  A --> B", gen: s.gen - 1}}) || s.refresh {
		t.Error("an older request's ready was not swallowed")
	}
	f.state = Ready
	l.diagrams.tasks[42] = struct{}{}
	if !l.HandleTask(tui.TaskResult{ID: 42, Value: htmlDiagramReady{src: "flowchart LR\n  A --> B", gen: s.gen}}) {
		t.Fatal("the ready was not the layout's")
	}
	paint()
	if !preDrawn(l) || f.paints == 0 {
		t.Errorf("after its ready the pre is drawn %v, painted %d times", preDrawn(l), f.paints)
	}
}
