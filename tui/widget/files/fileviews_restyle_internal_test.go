package files

import (
	"reflect"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// TestFileViewsRestyleEveryPart: a file view is a list, a preview, frames and a gap, each styled at
// construction. A restyle must reach all of them, and the zero value must leave the view as one
// built unstyled: drawn, each looks cell for cell as the view built that way.
func TestFileViewsRestyleEveryPart(t *testing.T) {
	red := style.New().Background(style.ANSI(1)).Foreground(style.ANSI(7))
	src := FileSource{FS: fstest.MapFS{"a.txt": {Data: []byte("x")}}, Root: "mem://"}
	st := FilePaneStyles{Surface: red, Cursor: red.Bold(true), CursorBlurred: red.Italic(true),
		Border: red, FocusedBorder: red.Underline(true), Gap: red}
	drawn := func(name string, root tui.Component) [][]tui.Cell {
		t.Helper()
		h := widgettest.StartApp(t, root, 60, 12)
		defer h.Stop()
		// settled: the listing is in, and the screen is the same two polls running (a preview
		// reads its file off the loop, so the first frame with a.txt may come before it)
		var prev [][]tui.Cell
		if !widgettest.Wait(h, func() bool {
			cur := h.TB.Snapshot()
			done := strings.Contains(h.TB.String(), "a.txt") && reflect.DeepEqual(prev, cur)
			prev = cur
			return done
		}) {
			t.Fatalf("%s: the view never settled with a.txt listed:\n%s", name, h.TB.String())
		}
		return prev
	}
	same := func(name string, got, want tui.Component) {
		t.Helper()
		if g, w := drawn(name, got), drawn(name+" (built so)", want); !reflect.DeepEqual(g, w) {
			t.Errorf("%s draws other cells than a view built that way", name)
		}
	}

	restyled := NewFileOpenView(WithFileViewSource(src))
	restyled.WithStyles(st)
	same("an Open view restyled", restyled, NewFileOpenView(WithFileViewSource(src), WithFileViewStyles(st)))
	reverted := NewFileOpenView(WithFileViewSource(src), WithFileViewStyles(st))
	reverted.WithStyles(FilePaneStyles{})
	same("an Open view given the zero styles", reverted, NewFileOpenView(WithFileViewSource(src)))

	saved := NewFileSaveView(WithFileViewSource(src), WithFileViewPreview(true))
	saved.WithStyles(st)
	same("a Save view restyled", saved, NewFileSaveView(WithFileViewSource(src), WithFileViewPreview(true), WithFileViewStyles(st)))
	savedBack := NewFileSaveView(WithFileViewSource(src), WithFileViewPreview(true), WithFileViewStyles(st))
	savedBack.WithStyles(FilePaneStyles{})
	same("a Save view given the zero styles", savedBack, NewFileSaveView(WithFileViewSource(src), WithFileViewPreview(true)))
}
