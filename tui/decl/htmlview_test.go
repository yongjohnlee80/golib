package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

const htmlViewDoc = `import tui 1.0
import demo 1.0
Window {
 HTMLView {
  id: page
  html: "<h1>Hello</h1><p data-src=\"0-9\">see <a href=\"x.md\">the note</a></p><p data-src=\"10-19\">two</p><p data-src=\"20-29\">three</p><p data-src=\"30-39\">four</p>"
  onLinkActivated: App.log(href)
  onScrolledTo: App.log(sourceByte)
 }
}`

// TestAnHTMLViewFromTheDocument: QML's HTMLView lays its html out in cells on a terminal, emits
// linkActivated with a clicked link's target and scrolledTo with the source byte at the top, and
// scrollToSource scrolls; the host reaches the widget by id.
func TestAnHTMLViewFromTheDocument(t *testing.T) {
	rec := &recorder{}
	s := decltest.Run(t, 30, 3,
		tuidecl.LayoutSource("main.qml", []byte(htmlViewDoc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "Hello")
	s.WaitForText(t, "see the note")
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 5, Y: 2},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 5, Y: 2})
	s.WaitFor(t, "the link emitted", func(string) bool { return logged(rec) == "x.md" })
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("page", "scrollToSource", 20); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "scrolled to three", func(sc string) bool { return strings.HasPrefix(strings.Split(sc, "\n")[0], "three") })
	s.WaitFor(t, "scrolledTo emitted", func(string) bool { return strings.HasSuffix(logged(rec), ",20") })
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("page", "scrollToSource"); err == nil || !strings.Contains(err.Error(), "one argument") {
			t.Errorf("scrollToSource with no byte: %v, want its arity refused", err)
		}
		if err := s.Program.Call("page", "scrollToSource", "three"); err == nil {
			t.Error("scrollToSource took a string for its byte")
		}
	})
	onScreenLoop(t, s, func() {
		if v, ok := tuidecl.FindAs[*widget.HTMLView](s.Program, "page"); !ok || !strings.Contains(string(v.Source()), "Hello") {
			t.Error("the host does not reach the HTMLView by id")
		}
	})
}
