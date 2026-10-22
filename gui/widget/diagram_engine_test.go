package widget

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// laidFake is a laid picture of a fixed size.
type laidFake struct {
	gui.ViewFunc
	size gui.Size
}

func (p laidFake) Size() gui.Size { return p.size }

var (
	errNotDrawn = errors.New("not drawn here")
	errBroken   = errors.New("broken")
)

// engineFake lays out its languages as a fixed picture, or fails with err; it declines
// errNotDrawn.
type engineFake struct {
	langs []string
	err   error
	lays  atomic.Int32
}

func (e *engineFake) Langs() []string { return e.langs }

func (e *engineFake) Lay(context.Context, DiagramRequest, gui.Measurer) (LaidPicture, error) {
	e.lays.Add(1)
	if e.err != nil {
		return nil, e.err
	}
	return laidFake{ViewFunc: func(gui.Canvas) {}, size: gui.Size{W: 50, H: 20}}, nil
}

func (e *engineFake) Declines(err error) bool { return errors.Is(err, errNotDrawn) }

// settled asks d for req until it is no longer Pending, and returns that answer.
func settled(t *testing.T, d Diagrammer, req DiagramRequest) DiagramAnswer {
	t.Helper()
	ready := make(chan struct{}, 1)
	ans := d.Diagram(context.Background(), req, func() { ready <- struct{}{} })
	if ans.State == Pending {
		<-ready
		ans = d.Diagram(context.Background(), req, func() {})
	}
	return ans
}

// A host's engine lays out its language, in each of its spellings, through Diagrams' cache: once.
func TestAHostEngineDrawsItsLanguage(t *testing.T) {
	e := &engineFake{langs: []string{"graphviz", "dot"}}
	d := Diagrams(nil, e)
	for _, lang := range []string{"graphviz", "dot"} {
		ans := settled(t, d, DiagramRequest{Lang: lang, Src: "a -> b", Width: 300, Scale: 1})
		if ans.State != Ready || ans.Size != (gui.Size{W: 50, H: 20}) {
			t.Errorf("%s: %+v, want Ready with the engine's picture", lang, ans)
		}
	}
	settled(t, d, DiagramRequest{Lang: "graphviz", Src: "a -> b", Width: 300, Scale: 1})
	if n := e.lays.Load(); n != 2 { // one per spelling: the language is in the key; asked again, cached
		t.Errorf("laid %d times, want 2", n)
	}
}

// What the engine declines goes to the fallback; another error draws the source as code.
func TestAHostEnginesDeclinesReachTheFallback(t *testing.T) {
	fb := &fakeFallback{}
	req := DiagramRequest{Lang: "graphviz", Src: "a -> b", Width: 300, Scale: 1}
	if ans := settled(t, Diagrams(fb, &engineFake{langs: []string{"graphviz"}, err: errNotDrawn}), req); ans.State != Pending || fb.asked.Load() != 1 {
		t.Errorf("declined: %v, fallback asked %d times; want the fallback's answer", ans.State, fb.asked.Load())
	}
	if ans := settled(t, Diagrams(fb, &engineFake{langs: []string{"graphviz"}, err: errBroken}), req); ans.State != Declined || fb.asked.Load() != 1 {
		t.Errorf("failed: %v, fallback asked %d times; want Declined, the fallback not asked", ans.State, fb.asked.Load())
	}
}

// fallbackNaming is a fallback that names one language, and echoes any other name with false.
type fallbackNaming struct{ fakeFallback }

func (*fallbackNaming) DiagramLanguage(name string) (string, bool) { return name, name == "plantuml" }

// Diagrams names golib's languages and its engines', an engine taking a spelling from golib's, then
// its fallback's. A nil engine, typed or not, is none.
func TestDiagramsNamesItsLanguages(t *testing.T) {
	d := Diagrams(&fallbackNaming{}, &engineFake{langs: []string{"vegalite"}}, nil, (*engineFake)(nil)).(DiagramLanguages)
	for _, tc := range []struct {
		name, want string
		ok         bool
	}{
		{"mermaid", "mermaid", true},
		{"vega-lite", "vega-lite", true},
		{"vegalite", "vegalite", true}, // the host's engine's now, under its own name
		{"plantuml", "plantuml", true}, // the fallback's
		{"graphviz", "", false},        // no language: no name, whatever the fallback says
	} {
		if got, ok := d.DiagramLanguage(tc.name); got != tc.want || ok != tc.ok {
			t.Errorf("DiagramLanguage(%q) = (%q, %v), want (%q, %v)", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	if got, ok := Diagrams(nil).(DiagramLanguages).DiagramLanguage("vegalite"); got != "vega-lite" || !ok {
		t.Errorf("golib's vegalite = (%q, %v), want (\"vega-lite\", true)", got, ok)
	}
}

// namingDiagrams is a Diagrammer that names graphviz as a language it draws.
type namingDiagrams struct{ htmlFakeDiagrams }

func (*namingDiagrams) DiagramLanguage(name string) (string, bool) {
	return "graphviz", name == "graphviz"
}

// A page's pre of a language the view's Diagrammer names is a diagram; one that names none draws
// golib's languages only.
func TestAPreOfANamedLanguageIsADiagram(t *testing.T) {
	const page = "<pre class=\"graphviz\">a -&gt; b</pre>"
	named := &namingDiagrams{htmlFakeDiagrams{state: Ready}}
	l, _ := diagramView(t, page, named)
	if len(named.asked) == 0 || named.asked[0].Lang != "graphviz" || !preDrawn(l) {
		t.Errorf("asked %+v, drawn %v; want the graphviz pre asked for and drawn", named.asked, preDrawn(l))
	}
	plain := &htmlFakeDiagrams{state: Ready}
	if l, _ := diagramView(t, page, plain); len(plain.asked) != 0 || preDrawn(l) {
		t.Errorf("a Diagrammer naming no languages was asked %+v for a graphviz pre", plain.asked)
	}
}
