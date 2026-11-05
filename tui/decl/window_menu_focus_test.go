package decl_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// window_menu_focus_test.go: a menu row that took the keyboard keeps it. After a row runs, the
// Window gives the keyboard back to the document's `focus: true` only when it was left on the closed
// menu; a row that opened a pane and asked for its focus (the focus waiting for the pane's first
// layout) keeps the pane's, even when the document's focus hands itself on as it gains it (as an
// Editor hands its focus on into its shown document view).

// focusSpot is a focusable cell that shows its name.
type focusSpot struct{ *widget.Text }

func (*focusSpot) AcceptsFocus() bool { return true }

// redirector is a focusable that, gaining the focus itself, hands it on into its inner spot, as an
// Editor hands it on into its shown document view: a second redirector that is not the Editor.
type redirector struct {
	ctx   *tui.Context
	inner *focusSpot
}

func (r *redirector) Init(ctx *tui.Context) { r.ctx = ctx; ctx.Mount(r.inner) }
func (*redirector) AcceptsFocus() bool      { return true }
func (r *redirector) Layout(c tui.Constraints) tui.Size {
	sz := r.ctx.LayoutChild(r.inner, c)
	r.ctx.PlaceChild(r.inner, tui.Rect{W: sz.W, H: sz.H})
	return c.Constrain(tui.Size{W: sz.W, H: max(sz.H, 1)})
}
func (*redirector) Render(tui.Surface) {}
func (r *redirector) HandleEvent(ev tui.Event) bool {
	if f, ok := ev.(tui.FocusEvent); ok && f.Gained && r.ctx.Focused() {
		r.ctx.FocusInto(r.inner)
	}
	return false
}

// runMenuFocusDoc is a Window with a Go menu (Pane: opens a drawer and focuses the field in it;
// Log: runs, leaving the keyboard alone), a redirecting home with the document's focus, and the
// drawer. It returns the screen and the home.
func runMenuFocusDoc(t *testing.T) (*decltest.Screen, *redirector) {
	t.Helper()
	home := &redirector{inner: &focusSpot{widget.NewText("home")}}
	var s *decltest.Screen
	call := func(id, method string) error { return s.Program.Call(id, method) }
	s = decltest.Run(t, 60, 12,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n"+
			" MenuBar { Dock.edge: Tui.Top\n  Menu { title: \"&Go\"\n"+
			"   MenuItem { text: \"&Pane\"; onTriggered: App.openPane() }\n"+
			"   MenuItem { text: \"&Log\"; onTriggered: App.log() } } }\n"+
			" Home { id: home; focus: true }\n"+
			" Drawer { id: pane; modal: false; edge: Tui.Right; size: 30; length: 100\n  Spot { id: inPane } }\n}")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(
			tuidecl.Type{Name: "Home", Build: func(tuidecl.Build) (tui.Component, []string, error) { return home, nil, nil }},
			tuidecl.Type{Name: "Spot", Build: func(tuidecl.Build) (tui.Component, []string, error) {
				return &focusSpot{widget.NewText("in the pane")}, nil, nil
			}}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{
			"App.openPane": func([]qml.SpecValue) error {
				if err := call("pane", "open"); err != nil {
					return err
				}
				return call("inPane", "forceActiveFocus")
			},
			"App.log": func([]qml.SpecValue) error { return nil },
		}))
	s.WaitForText(t, "home")
	return s, home
}

// menuFocusOnLoop runs fn on s's loop and returns its answer.
func menuFocusOnLoop(s *decltest.Screen, fn func() bool) bool {
	ch := make(chan bool, 1)
	s.Program.Post(func() { ch <- fn() })
	return <-ch
}

// menuFocusKeyboardIn reports whether the keyboard is in the document's component id.
func menuFocusKeyboardIn(s *decltest.Screen, id string) bool {
	return menuFocusOnLoop(s, func() bool {
		c, ok := s.Program.Find(id)
		return ok && s.Program.App().FocusWithin(c)
	})
}

// A menu row that opens a pane and asks for its focus keeps it: the pane's field has the keyboard
// once the pane is laid out, though the menu bar gave the keyboard back to the document's focus
// before the row ran, and that focus handed itself on as it gained it. (Before, the Window gave the
// keyboard back again after the row, and that move overtook the pane's focus, still waiting for the
// pane's first layout.)
func TestAMenuRowThatAskedForThePanesFocusKeepsIt(t *testing.T) {
	s, _ := runMenuFocusDoc(t)
	s.Keys(t, decltest.Alt('g'))
	s.WaitForText(t, "Pane")
	s.Keys(t, decltest.Rune('p'))
	s.WaitFor(t, "the pane's field has the keyboard", func(string) bool { return menuFocusKeyboardIn(s, "inPane") })
}

// A menu row that leaves the keyboard alone gives it back to the document's focus, handed on as it
// gains it into its inner part, as before (an Editor's document view keeps the keyboard).
func TestAMenuRowThatLeftTheKeyboardGivesItBackHandedOn(t *testing.T) {
	s, home := runMenuFocusDoc(t)
	s.Keys(t, decltest.Alt('g'))
	s.WaitForText(t, "Log")
	s.Keys(t, decltest.Rune('l'))
	s.WaitFor(t, "the keyboard back in the home's inner part", func(string) bool {
		return menuFocusOnLoop(s, func() bool {
			return s.Program.App().FocusWithin(home) && !home.ctx.Focused() && home.inner.Context().Focused()
		})
	})
}
