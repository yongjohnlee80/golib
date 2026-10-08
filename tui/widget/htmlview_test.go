package widget_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func mountedHTML(t *testing.T, w, h int, html string, opts ...widget.HTMLOption) (*harness, *widget.HTMLView, *shell) {
	t.Helper()
	v := widget.NewHTMLView(opts...)
	v.SetHTML([]byte(html))
	sh := newShell(v)
	hh := startApp(t, sh, w, h)
	hh.inject(tab())
	hh.barrier(sh)
	return hh, v, sh
}

func mouseAt(kind tui.MouseKind, x, y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: kind, Button: tui.MouseLeft, X: x, Y: y}
}

// TestHTMLViewDrawsTheDocument: in cells a heading is bold, a link underlined, a list's marker
// hangs, and nothing a page hides is drawn.
func TestHTMLViewDrawsTheDocument(t *testing.T) {
	h, _, _ := mountedHTML(t, 40, 10, `<h1>Title</h1><p>see <a href="b.md">the link</a></p>`+
		`<ul><li>item</li></ul><script>hidden()</script><p hidden>secret</p>`)
	h.waitFor("the document drawn", func() bool { return strings.Contains(h.grid(), "the link") })
	if a := cellAttrs(h, 0, 0); a.Mask&tui.AttrBold == 0 {
		t.Errorf("the heading is not bold: %+v", a)
	}
	if a := cellAttrs(h, 4, 2); a.Mask&tui.AttrUnderline == 0 {
		t.Errorf("the link is not underlined: %+v\n%s", a, h.grid())
	}
	h.wantContains(" • item")
	h.wantNotContains("hidden()")
	h.wantNotContains("secret")
}

// TestHTMLViewSelectsAndCopies: a drag selects across a paragraph, a list item and a table row,
// in document order whichever way it goes; Ctrl+C copies the text with a line between blocks and
// a tab between cells.
func TestHTMLViewSelectsAndCopies(t *testing.T) {
	h, v, sh := mountedHTML(t, 30, 10, `<p>alpha beta</p><ul><li>gamma</li></ul><table><tr><td>c1</td><td>c2</td></tr></table>`)
	// rows: "alpha beta" / "" / " • gamma" / "" / "c1 │ c2"
	h.waitFor("drawn", func() bool { return strings.Contains(h.grid(), "c1 │ c2") })
	h.inject(mouseAt(tui.MouseRelease, 0, 0)) // a stray release before any press does nothing
	h.inject(mouseAt(tui.MousePress, 6, 0), mouseAt(tui.MouseMotion, 7, 4), mouseAt(tui.MouseRelease, 7, 4))
	h.barrier(sh)
	var got string
	h.onLoop(func() { got = v.SelectedText() })
	if want := "beta\ngamma\nc1\tc2"; got != want {
		t.Fatalf("selected %q, want %q", got, want)
	}
	if a := cellAttrs(h, 3, 2); a.Mask&tui.AttrReverse == 0 {
		t.Errorf("the selected item is not drawn reversed: %+v", a)
	}
	h.inject(ctrl('c'))
	h.waitFor("the clipboard", func() bool { return string(h.tb.Clipboard()) == "beta\ngamma\nc1\tc2" })

	// backwards: the same text
	h.inject(mouseAt(tui.MousePress, 7, 4), mouseAt(tui.MouseMotion, 6, 0), mouseAt(tui.MouseRelease, 6, 0))
	h.barrier(sh)
	h.onLoop(func() { got = v.SelectedText() })
	if got != "beta\ngamma\nc1\tc2" {
		t.Errorf("a backward drag selected %q", got)
	}
}

// TestHTMLViewLinks: a click on a link reports its target; a drag that ends on one does not, and
// a click off it reports nothing.
func TestHTMLViewLinks(t *testing.T) {
	var links []string
	h, _, sh := mountedHTML(t, 30, 5, `<p>go <a href="next.md#s">there</a> now</p>`,
		widget.WithOnLink(func(href string) { links = append(links, href) }))
	h.waitFor("drawn", func() bool { return strings.Contains(h.grid(), "there") })
	h.inject(mouseAt(tui.MousePress, 4, 0), mouseAt(tui.MouseRelease, 4, 0))
	h.inject(mouseAt(tui.MousePress, 0, 0), mouseAt(tui.MouseMotion, 4, 0), mouseAt(tui.MouseRelease, 4, 0))
	h.inject(mouseAt(tui.MousePress, 1, 0), mouseAt(tui.MouseRelease, 1, 0))
	h.barrier(sh)
	if fmt.Sprint(links) != "[next.md#s]" {
		t.Errorf("links reported %v, want the click's alone", links)
	}
}

// longDoc is n paragraphs, each with its source bytes, ten apart.
func longDoc(n int, prefix string) string {
	var sb strings.Builder
	for i := range n {
		fmt.Fprintf(&sb, `<p data-src="%d-%d">%s%d</p>`, i*10, i*10+10, prefix, i)
	}
	return sb.String()
}

// TestHTMLViewScrollSync: the wheel and the keys scroll; each block that reaches the top reports
// its source byte; ScrollToSource brings a byte's block to the top; SetHTML keeps the block at
// the top there when blocks are added above it.
func TestHTMLViewScrollSync(t *testing.T) {
	var tops []int
	h, v, sh := mountedHTML(t, 20, 5, longDoc(30, "para "), widget.WithOnScroll(func(b int) { tops = append(tops, b) }))
	h.waitFor("drawn", func() bool { return strings.Contains(h.grid(), "para 0") })
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown})
	h.waitFor("scrolled by the wheel", func() bool { return strings.HasPrefix(h.row(0), "para 2") || strings.HasPrefix(h.row(1), "para 2") })
	h.inject(key(tui.KeyDown))
	h.barrier(sh)
	// the wheel's three rows put para 2 at the top (the gap row above it is its own); the key's
	// one more row keeps it there, which is not reported again
	if fmt.Sprint(tops) != "[20]" {
		t.Errorf("source bytes reported %v, want para 2's (20) once", tops)
	}
	h.onLoop(func() { v.ScrollToSource(155) }) // inside para 15
	h.waitFor("para 15 at the top", func() bool { return strings.HasPrefix(h.row(0), "para 15") })

	// three paragraphs added above: para 15 stays at the top, at its new place
	h.onLoop(func() {
		v.SetHTML([]byte(`<p data-src="900-910">new a</p><p data-src="910-920">new b</p><p data-src="920-930">new c</p>` + longDoc(30, "para ")))
	})
	h.waitFor("para 15 still at the top", func() bool { return strings.HasPrefix(h.row(0), "para 15") })
	h.inject(key(tui.KeyHome))
	h.waitFor("home", func() bool { return strings.HasPrefix(h.row(0), "new a") })
	h.inject(key(tui.KeyEnd))
	h.waitFor("end", func() bool { return strings.Contains(h.grid(), "para 29") })
}

// TestHTMLViewDragScrollsPastTheEdge: a drag held below the view scrolls it and extends the
// selection as it goes.
func TestHTMLViewDragScrollsPastTheEdge(t *testing.T) {
	h, v, sh := mountedHTML(t, 20, 4, longDoc(20, "line "))
	h.waitFor("drawn", func() bool { return strings.Contains(h.grid(), "line 0") })
	h.inject(mouseAt(tui.MousePress, 0, 0), mouseAt(tui.MouseMotion, 3, 6))
	h.waitFor("scrolled by the held drag", func() bool {
		var y float32
		h.onLoop(func() { y = v.ScrollY() })
		return y >= 4
	})
	h.inject(mouseAt(tui.MouseRelease, 3, 6))
	h.barrier(sh)
	var got string
	h.onLoop(func() { got = v.SelectedText() })
	if !strings.HasPrefix(got, "line 0\nline 1") || !strings.Contains(got, "line 3") {
		t.Errorf("the drag selected %q", got)
	}
}

// TestDirImagesStaysInItsRoot: a relative path under the root opens; an absolute path, a scheme,
// a climb out and a symlink out do not.
func TestDirImagesStaysInItsRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "img"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(root, "img", "a.png"), filepath.Join(outside, "x.png")} {
		if err := os.WriteFile(f, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "x.png"), filepath.Join(root, "img", "out.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("img", "a.png"), filepath.Join(root, "in.png")); err != nil { // relative, inside
		t.Fatal(err)
	}
	res := widget.DirImages(root)
	ctx := context.Background()
	for _, ok := range []string{"img/a.png", "./img/a.png", "img/../img/a.png", "img/a.png?v=2", "in.png"} {
		rc, err := res(ctx, ok)
		if err != nil {
			t.Errorf("%q: %v, want it opened", ok, err)
			continue
		}
		b, _ := io.ReadAll(rc)
		_ = rc.Close()
		if string(b) != "png" {
			t.Errorf("%q read %q", ok, b)
		}
	}
	for _, bad := range []string{"../x.png", "img/../../x.png", filepath.Join(outside, "x.png"), "/etc/passwd",
		"file:///etc/passwd", "https://example.com/a.png", "img/out.png", "img", ""} {
		if rc, err := res(ctx, bad); err == nil {
			_ = rc.Close()
			t.Errorf("%q opened, want it refused", bad)
		} else if bad != "" && !errors.Is(err, widget.ErrImageRefused) && !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%q: %v, want a refusal", bad, err)
		}
	}
}
