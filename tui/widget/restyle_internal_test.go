package widget

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// restyle_internal_test.go holds the runtime setters to their rule: a restyle
// REPLACES the construction looks, derived as the constructor derives them, and
// the zero value is a widget built with no style option — nothing of an earlier
// look survives. Compared against a freshly built widget, not against constants
// restated here, so a changed default cannot make the test agree with itself.

var red = style.New().Background(style.ANSI(1)).Foreground(style.ANSI(7))

func TestTextRestylesAndReverts(t *testing.T) {
	x := NewText("hi")
	if x.WithStyle(red).st != red {
		t.Fatal("WithStyle did not replace the style")
	}
	if x.WithStyle(style.Style{}).st != NewText("hi").st {
		t.Fatal("the zero style did not restore the default")
	}
}

func TestBoxRestylesAndRevertsKeepingItsBorder(t *testing.T) {
	child := NewText("x")
	b := NewBox(child, WithBorder(style.BorderRounded), WithStyle(red.Padding(1, 2)))
	b.WithStyle(red)
	want := NewBox(child, WithBorder(style.BorderRounded), WithStyle(red))
	if b.base != want.base {
		t.Fatal("WithStyle did not derive the base as the constructor does")
	}
	b.WithStyle(style.Style{}).WithFocusedStyle(style.New().BorderForeground(style.ANSI(1)))
	b.WithFocusedStyle(style.Style{})
	plain := NewBox(child, WithBorder(style.BorderRounded))
	if b.base != plain.base || b.focusedSt != plain.focusedSt {
		t.Fatal("the zero styles did not restore the defaults — the border kept?")
	}
}

func TestEditorAndInputRestyleAndRevert(t *testing.T) {
	e := NewEditor(WithEditorStyles(TextInputStyles{Text: red}))
	e.WithStyles(TextInputStyles{Selection: red})
	if e.styles != NewEditor(WithEditorStyles(TextInputStyles{Selection: red})).styles {
		t.Fatal("the editor's earlier Text look survived a restyle")
	}
	if e.WithStyles(TextInputStyles{}).styles != NewEditor().styles {
		t.Fatal("the editor's zero styles did not restore the default")
	}
	in := NewTextInput(WithTextInputStyles(TextInputStyles{Text: red}))
	if in.WithStyles(TextInputStyles{}).styles != NewTextInput().styles {
		t.Fatal("the input's zero styles did not restore the default")
	}
}

func TestBarAndDividerRestyleAndRevert(t *testing.T) {
	s := NewStatusBar()
	if s.WithBarStyle(red).bar != red || s.WithBarStyle(style.Style{}).bar != NewStatusBar().bar {
		t.Fatal("status bar restyle")
	}
	sp := NewSplit(Horizontal, NewText("a"), NewText("b"))
	if sp.WithDividerStyle(red).divider != red ||
		sp.WithDividerStyle(style.Style{}).divider != NewSplit(Horizontal, NewText("a"), NewText("b")).divider {
		t.Fatal("split divider restyle")
	}
}

func TestMenuBarRestylesItsMenu(t *testing.T) {
	m := NewMenu(nil)
	bar := NewMenuBar(m)
	st := NewMenuStyle(red, red)
	if bar.WithStyle(st); m.style != st {
		t.Fatal("the menu did not take the bar's style")
	}
	if bar.WithStyle(nil); m.style != nil {
		t.Fatal("nil did not restore the menu's default")
	}
}

// TestFileViewsRestyleEveryPart: a file view is a list, a preview, frames and
// a gap, each styled at construction. A restyle must reach all of them, and
// the zero value must leave the view as one built unstyled.
func TestFileViewsRestyleEveryPart(t *testing.T) {
	src := FileSource{FS: fstest.MapFS{"a.txt": {Data: []byte("x")}}, Root: "mem://"}
	st := FilePaneStyles{Surface: red, Cursor: red.Bold(true), CursorBlurred: red.Italic(true),
		Border: red, FocusedBorder: red.Underline(true), Gap: red}
	type parts struct {
		list, preview, pane, gap any
	}
	open := func(v *FileOpenView) parts {
		return parts{v.list.list.styles, v.preview.view.styles,
			[2]style.Style{v.list.box.base, v.list.box.focusedSt}, v.root.(*Split).divider}
	}
	styled := NewFileOpenView(WithFileViewSource(src), WithFileViewStyles(st))
	v := NewFileOpenView(WithFileViewSource(src))
	if v.WithStyles(st); open(v) != open(styled) {
		t.Fatalf("restyled Open view differs from one built styled:\n%+v\n%+v", open(v), open(styled))
	}
	if v.WithStyles(FilePaneStyles{}); open(v) != open(NewFileOpenView(WithFileViewSource(src))) {
		t.Fatal("the zero styles did not restore an unstyled Open view")
	}

	save := func(v *FileSaveView) [3]any {
		return [3]any{v.name.styles, [2]style.Style{v.namePane.base, v.namePane.focusedSt}, open(v.listing)}
	}
	sv := NewFileSaveView(WithFileViewSource(src), WithFileViewPreview(true))
	if sv.WithStyles(st); save(sv) != save(NewFileSaveView(WithFileViewSource(src), WithFileViewPreview(true), WithFileViewStyles(st))) {
		t.Fatal("restyled Save view differs from one built styled")
	}
	if sv.WithStyles(FilePaneStyles{}); save(sv) != save(NewFileSaveView(WithFileViewSource(src), WithFileViewPreview(true))) {
		t.Fatal("the zero styles did not restore an unstyled Save view")
	}
}

// TestARestyleRepaints: a restyle of a mounted widget reaches the screen.
func TestARestyleRepaints(t *testing.T) {
	tb := tui.NewTestBackend(10, 1)
	x := NewText("hi")
	app := tui.NewApp(x, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	defer func() { cancel(); <-done }()
	bg := func() tui.CellColor { return tb.Snapshot()[0][0].Attrs.BG }
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%s never happened", what)
			}
		}
	}
	waitFor("the first frame", func() bool { return tb.Snapshot()[0][0].Content == "h" })
	before := bg()
	app.Update(func() { x.WithStyle(red) })
	waitFor("the restyle", func() bool { return bg() != before })
	app.Update(func() { x.WithStyle(style.Style{}) })
	waitFor("the revert", func() bool { return bg() == before })
}
