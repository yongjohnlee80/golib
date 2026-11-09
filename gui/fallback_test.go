package gui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// fbView is a GUI widget built on Base: Render draws its fallback.
type fbView struct {
	Base
	ctx *tui.Context
}

func (v *fbView) Init(ctx *tui.Context)           { v.ctx = ctx }
func (*fbView) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH}) }
func (v *fbView) Render(s tui.Surface)            { v.RenderFallback(v.ctx, s) }
func (*fbView) HandleEvent(tui.Event) bool        { return false }
func (*fbView) Paint(Canvas)                      {}
func newFBView() *fbView                          { v := &fbView{}; v.Bind(v); return v }

// mockView is one with a mock: it fills its cells with M.
type mockView struct{ fbView }

func (*mockView) RenderTUI(s tui.Surface) {
	sz := s.Size()
	for y := range sz.H {
		for x := range sz.W {
			s.SetCell(x, y, "M", style.Style{})
		}
	}
}
func newMockView() *mockView { v := &mockView{}; v.Bind(v); return v }

// screen runs root on a w x h TestBackend with opts and returns the screen once it shows want
// (or what it last showed).
func screen(t *testing.T, root tui.Component, w, h int, caps tui.Capabilities, want func(string) bool, opts ...tui.AppOption) string {
	t.Helper()
	be := tui.NewTestBackend(w, h, tui.WithTestCapabilities(caps))
	app := tui.NewApp(root, append([]tui.AppOption{tui.WithBackend(be)}, opts...)...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.Run(ctx)
	var got string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if got = be.String(); be.Flushes() > 0 && want(got) {
			return got
		}
	}
	return got
}

// On a terminal, a widget without a mock shows the notice, centred, in the App's language.
func TestAGUIWidgetWithoutAMockShowsTheNoticeOnATerminal(t *testing.T) {
	const en = "Not available in the terminal"
	got := screen(t, newFBView(), 40, 5, tui.Capabilities{}, func(s string) bool { return strings.Contains(s, en) })
	rows := strings.Split(got, "\n")
	if len(rows) != 5 || rows[2] != strings.Repeat(" ", 5)+en+strings.Repeat(" ", 6) {
		t.Fatalf("the screen is\n%s\nwant the notice centred on row 2", got)
	}
	ko := "터미널에서는 표시할 수 없습니다"
	got = screen(t, newFBView(), 40, 5, tui.Capabilities{}, func(s string) bool { return strings.Contains(s, ko) }, tui.WithLanguage("ko_KR"))
	if !strings.Contains(got, ko) {
		t.Fatalf("in Korean the screen is\n%s\nwant %q", got, ko)
	}
	// Narrower than the notice: it is cut to the width, never wrapped past it.
	got = screen(t, newFBView(), 10, 1, tui.Capabilities{}, func(s string) bool { return strings.HasPrefix(s, "Not") })
	if got != "Not availa" {
		t.Fatalf("10 columns show %q, want the notice cut to %q", got, "Not availa")
	}
}

// On a terminal a widget with a mock draws its mock; SetNotice puts the notice in its place.
func TestAGUIWidgetWithAMockDrawsItOnATerminal(t *testing.T) {
	got := screen(t, newMockView(), 6, 2, tui.Capabilities{}, func(s string) bool { return s == "MMMMMM\nMMMMMM" })
	if got != "MMMMMM\nMMMMMM" {
		t.Fatalf("the mock drew\n%s", got)
	}
	v := newMockView()
	v.SetNotice(true)
	got = screen(t, v, 40, 1, tui.Capabilities{}, func(s string) bool { return strings.Contains(s, "Not available") })
	if strings.Contains(got, "M") || !strings.Contains(got, "Not available") {
		t.Fatalf("with SetNotice the screen is %q; want the notice, not the mock", got)
	}
}

// Where the backend paints native views, the fallback draws nothing; NativeView is the bound view.
func TestTheFallbackDrawsNothingWhereNativeViewsArePainted(t *testing.T) {
	got := screen(t, newMockView(), 6, 2, tui.Capabilities{NativeViews: true}, func(s string) bool { return true })
	if strings.TrimSpace(strings.ReplaceAll(got, "\n", "")) != "" {
		t.Fatalf("with native views the fallback drew\n%s", got)
	}
	var b Base
	if _, ok := b.NativeView(); ok {
		t.Fatal("an unbound Base has a native view")
	}
	v := newFBView()
	if nv, ok := v.NativeView(); !ok || nv != View(v) {
		t.Fatalf("NativeView = %v, %v; want the bound view", nv, ok)
	}
}
