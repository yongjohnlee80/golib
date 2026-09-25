package widget_test

import (
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// measurer lays its children out at an unbounded height, as a dialog card
// measures its body, and records what each asked for.
type measurer struct {
	widget.Base
	kids []tui.Component
	mu   sync.Mutex
	got  []int
}

func (m *measurer) Init(ctx *tui.Context) {
	m.Base.Init(ctx)
	for _, k := range m.kids {
		ctx.Mount(k)
	}
}

func (m *measurer) Layout(c tui.Constraints) tui.Size {
	got := make([]int, len(m.kids))
	y := 0
	for i, k := range m.kids {
		sz := m.Context().LayoutChild(k, tui.Constraints{MaxW: c.MaxW, MaxH: tui.Unbounded})
		got[i] = sz.H
		m.Context().PlaceChild(k, tui.Rect{Y: y, W: sz.W, H: sz.H})
		y += sz.H
	}
	m.mu.Lock()
	m.got = got
	m.mu.Unlock()
	return c.Constrain(tui.Size{W: c.MaxW, H: y})
}

func (*measurer) Render(tui.Surface) {}

// A view offered an unbounded height asks for its content — a List its rows,
// a Table its header and rows — as a Qt view's implicit height is; at least one.
func TestAViewsIntrinsicHeightIsItsContent(t *testing.T) {
	text := func(s string) string { return s }
	items := []string{"a", "b", "c", "d"}
	table := widget.NewTable([]widget.TableColumn[string]{{Title: "X", Cell: text}})
	table.SetItems(items)
	m := &measurer{kids: []tui.Component{
		widget.NewList(widget.WithItems(items, text)),
		table,
		widget.NewList(widget.WithItems([]string{}, text)),
	}}
	h := startApp(t, m, 20, 40)
	defer h.stop()
	h.settle()
	m.mu.Lock()
	got := append([]int(nil), m.got...)
	m.mu.Unlock()
	if len(got) != 3 || got[0] != 4 || got[1] != 5 || got[2] != 1 {
		t.Errorf("intrinsic heights %v: want list 4, table header + 4 = 5, empty list 1", got)
	}
}
