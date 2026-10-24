//go:build linux || darwin

package decl_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// presses counts the window-control handlers a document runs, on the loop.
type presses struct {
	mu                         sync.Mutex
	minimize, maximize, closes int
}

func (p *presses) handlers() map[string]decl.HandlerFunc {
	count := func(n *int) decl.HandlerFunc {
		return func([]qml.SpecValue) error {
			p.mu.Lock()
			defer p.mu.Unlock()
			*n++
			return nil
		}
	}
	return map[string]decl.HandlerFunc{"App.minimize": count(&p.minimize), "App.maximize": count(&p.maximize), "App.close": count(&p.closes)}
}

func (p *presses) counts() (int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.minimize, p.maximize, p.closes
}

// windowButtonsDoc is a Window whose one child, declared by body, fills its 40 columns from row 0.
func runWindowButtonsDoc(t *testing.T, body string) (*decltest.Screen, *presses) {
	t.Helper()
	p := &presses{}
	s := decltest.Run(t, 40, 6,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+body+"\n}")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(p.handlers()))
	return s, p
}

// The window controls' glyphs on a 40-column bar, side by side in its last nine cells: _ in cell 32,
// + in 35, x in 38.
const minimizeCell, maximizeCell, closeCell = 32, 35, 38

// click presses the cell x on row 0.
func click(t *testing.T, s *decltest.Screen, x int) {
	t.Helper()
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: 0},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: 0})
}

// A TerminalTabs and a TabView declared with windowButtons draw _, + and x side by side at their
// bar's right end; a press on _ emits minimizeRequested, on + maximizeRequested, on x closeRequested,
// which the document hands to its host. Without the property none is drawn.
func TestWindowButtonsOnTerminalTabsAndTabView(t *testing.T) {
	for name, body := range map[string]string{
		"TerminalTabs": " TerminalTabs { windowButtons: true; onMinimizeRequested: App.minimize(); onMaximizeRequested: App.maximize(); onCloseRequested: App.close() }",
		"TabView": " TabView { windowButtons: true; onMinimizeRequested: App.minimize(); onMaximizeRequested: App.maximize(); onCloseRequested: App.close()\n" +
			"  Tab { title: \"one\"; Text { text: \"page\" } } }",
	} {
		t.Run(name, func(t *testing.T) {
			s, p := runWindowButtonsDoc(t, body)
			s.WaitFor(t, "_, + and x side by side at the bar's end", func(sc string) bool {
				return strings.HasSuffix(strings.TrimRight(strings.Split(sc, "\n")[0], " "), "_  +  x")
			})
			click(t, s, closeCell)
			s.WaitFor(t, "x: closeRequested alone", func(string) bool { m, x, c := p.counts(); return m == 0 && x == 0 && c == 1 })
			click(t, s, maximizeCell)
			s.WaitFor(t, "+: maximizeRequested alone", func(string) bool { m, x, c := p.counts(); return m == 0 && x == 1 && c == 1 })
			click(t, s, minimizeCell)
			s.WaitFor(t, "_: minimizeRequested", func(string) bool { m, x, c := p.counts(); return m == 1 && x == 1 && c == 1 })
		})
	}
	s, _ := runWindowButtonsDoc(t, " TabView { Tab { title: \"one\"; Text { text: \"page\" } } }")
	s.WaitForText(t, "one")
	if bar := strings.TrimRight(strings.Split(s.String(), "\n")[0], " "); strings.HasSuffix(bar, "x") {
		t.Errorf("a TabView without windowButtons drew them: %q", bar)
	}
}
