package widget

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// pngWidth is a PNG's width from its IHDR, or -1 for something that is not a PNG.
func pngWidth(b []byte) int {
	if len(b) < 24 || !bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")) || string(b[12:16]) != "IHDR" {
		return -1
	}
	return int(binary.BigEndian.Uint32(b[16:20]))
}

func TestAMissingRasterizerSaysSo(t *testing.T) {
	defer func(s, h []string) { svgTools, htmlTools = s, h }(svgTools, htmlTools)
	svgTools, htmlTools = []string{"golib-no-such-tool"}, []string{"golib-no-such-tool"}
	if _, ok := SVGRasterizer(); ok {
		t.Fatal("a missing tool was found")
	}
	if _, err := RasterizeSVG(context.Background(), []byte("<svg/>"), 10); !errors.Is(err, ErrNoRasterizer) {
		t.Fatalf("RasterizeSVG without a tool: %v", err)
	}
	if _, err := RasterizeHTML(context.Background(), []byte("<p>x</p>"), 10, 10); !errors.Is(err, ErrNoRasterizer) {
		t.Fatalf("RasterizeHTML without a tool: %v", err)
	}
}

func TestRasterizeSVG(t *testing.T) {
	if _, ok := SVGRasterizer(); !ok {
		t.Skip("rsvg-convert is not installed")
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="#336"/></svg>`)
	png, err := RasterizeSVG(context.Background(), svg, 200)
	if err != nil {
		t.Fatal(err)
	}
	if w := pngWidth(png); w != 200 {
		t.Fatalf("width = %d, want 200", w)
	}
	if _, err := RasterizeSVG(context.Background(), []byte("<svg"), 10); err == nil {
		t.Fatal("a broken SVG rendered")
	}
	if _, err := RasterizeSVG(context.Background(), make([]byte, MaxRasterInput+1), 10); err == nil {
		t.Fatal("an oversized SVG was handed to the tool")
	}
}

// The HTML renderer renders offline: a page asking a server for an image never reaches it.
func TestRasterizeHTMLOffline(t *testing.T) {
	if _, ok := HTMLRasterizer(); !ok {
		t.Skip("no headless chromium or chrome is installed")
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer srv.Close()
	html := fmt.Sprintf(`<!doctype html><body style="background:#fff"><h1>offline</h1><img src="%s/x.png"><script src="%s/s.js"></script></body>`, srv.URL, srv.URL)
	png, err := RasterizeHTML(context.Background(), []byte(html), 320, 200)
	if err != nil && strings.Contains(err.Error(), "No usable sandbox") {
		// the browser refuses to run without its sandbox (a CI runner whose AppArmor forbids user
		// namespaces), and RasterizeHTML never turns the sandbox off: a host falls back
		t.Skip("the installed browser has no usable sandbox here")
	}
	if err != nil {
		t.Fatal(err)
	}
	if w := pngWidth(png); w != 320 {
		t.Fatalf("width = %d, want 320", w)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the page reached the network %d times", n)
	}
}

// An Image reports its PNG with a version that moves on each change, and nothing once cleared;
// its cells are blank.
func TestImageWidgetReportsItsPNG(t *testing.T) {
	m := NewImage()
	if _, ok := m.Image(); ok || m.HasImage() {
		t.Fatal("an empty Image reported an image")
	}
	m.SetPNG([]byte("one"))
	a, ok := m.Image()
	if !ok || string(a.PNG) != "one" || a.ID == 0 {
		t.Fatalf("after SetPNG: %+v, %v", a, ok)
	}
	m.SetPNG([]byte("two"))
	b, _ := m.Image()
	if b.ID != a.ID || b.Version == a.Version {
		t.Fatalf("a new PNG keeps its id and moves its version: %+v then %+v", a, b)
	}
	if other := NewImage(); other.id == m.id {
		t.Fatal("two Images share an id")
	}
	m.Clear()
	if _, ok := m.Image(); ok {
		t.Fatal("a cleared Image reported an image")
	}
}

// fakeTool installs an executable script as the only tool a rasterizer finds.
func fakeTool(t *testing.T, tools *[]string, script string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "tool")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	old := *tools
	*tools = []string{p}
	t.Cleanup(func() { *tools = old })
}

// The rasterizers' contract against tools that behave, fail and overflow: what a tool writes is
// the PNG, a failing tool's words reach the error, and an oversized input never reaches the tool.
func TestRasterizersAgainstFakeTools(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell scripts as tools")
	}
	fakeTool(t, &svgTools, `cat >/dev/null; printf 'svg-png'`)
	if b, err := RasterizeSVG(context.Background(), []byte("<svg/>"), 10); err != nil || string(b) != "svg-png" {
		t.Fatalf("RasterizeSVG = %q, %v", b, err)
	}
	fakeTool(t, &svgTools, `echo "bad svg" >&2; exit 3`)
	if _, err := RasterizeSVG(context.Background(), []byte("<svg/>"), 10); err == nil || !strings.Contains(err.Error(), "bad svg") {
		t.Fatalf("a failing tool: %v", err)
	}

	// the browser writes its screenshot to the --screenshot= path
	fakeTool(t, &htmlTools, `for a in "$@"; do case "$a" in --screenshot=*) printf 'html-png' > "${a#--screenshot=}";; esac; done`)
	if b, err := RasterizeHTML(context.Background(), []byte("<p>x</p>"), 10, 10); err != nil || string(b) != "html-png" {
		t.Fatalf("RasterizeHTML = %q, %v", b, err)
	}
	fakeTool(t, &htmlTools, `exit 0`) // ran, wrote nothing
	if _, err := RasterizeHTML(context.Background(), []byte("<p>x</p>"), 10, 10); err == nil || !strings.Contains(err.Error(), "no screenshot") {
		t.Fatalf("no screenshot: %v", err)
	}
	fakeTool(t, &htmlTools, `echo "crashed" >&2; exit 1`)
	if _, err := RasterizeHTML(context.Background(), []byte("<p>x</p>"), 10, 10); err == nil || !strings.Contains(err.Error(), "crashed") {
		t.Fatalf("a crashing browser: %v", err)
	}
	if _, err := RasterizeHTML(context.Background(), make([]byte, MaxRasterInput+1), 10, 10); err == nil {
		t.Fatal("an oversized page was handed to the browser")
	}
	if p, ok := HTMLRasterizer(); !ok || !strings.HasSuffix(p, "tool") {
		t.Fatalf("HTMLRasterizer = %q, %v", p, ok)
	}
	if clampPixels(0) != 1 || clampPixels(1<<20) != maxRasterPixels {
		t.Fatal("clampPixels")
	}
}

// An Image fills what it is offered, takes its minimum where the offer is unbounded, and paints
// its cells blank so nothing beneath shows through.
func TestImageLayoutAndPaint(t *testing.T) {
	m := NewImage()
	if sz := m.Layout(tui.Constraints{MaxW: 30, MaxH: 8}); sz.W != 30 || sz.H != 8 {
		t.Fatalf("bounded layout = %+v", sz)
	}
	if sz := m.Layout(tui.Constraints{MinW: 4, MinH: 2, MaxW: tui.Unbounded, MaxH: tui.Unbounded}); sz.W != 4 || sz.H != 2 {
		t.Fatalf("unbounded layout = %+v", sz)
	}
	tb := tui.NewTestBackend(6, 3)
	app := tui.NewApp(m, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Run(ctx) }()
	defer func() { cancel(); <-done }()
	cells := func() (int, int) { // loop-owned: read where the paint writes it
		ch := make(chan [2]int, 1)
		app.Update(func() { c, r := m.Cells(); ch <- [2]int{c, r} })
		v := <-ch
		return v[0], v[1]
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		cols, rows := cells()
		if cols == 6 && rows == 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("cells = %dx%d, want 6x3", cols, rows)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if strings.TrimSpace(tb.String()) != "" {
		t.Fatalf("the Image painted something: %q", tb.String())
	}
}

func tallPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewGray(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A scrollable Image shows one cell to CellPixelsW × CellPixelsH of its PNG — 10×5 cells are a
// 100×100 window on a 300×1000 PNG, a row 20 pixels, a step sideways 40 — and moves it by the keys
// and the wheel, never past an edge. One that does not scroll shows the whole PNG and takes no keys.
func TestAScrollableImage(t *testing.T) {
	m := NewImage()
	m.SetScrollable(true)
	m.SetPNG(tallPNG(t, 300, 1000))
	m.cols, m.rows = 10, 5 // as its paint leaves them
	clip := func() tui.Rect { img, _ := m.Image(); return img.Clip }
	if c := clip(); c != (tui.Rect{W: 100, H: 100}) {
		t.Fatalf("the first view is %+v, want the top left 100×100", c)
	}
	if !m.AcceptsFocus() {
		t.Error("a scrollable Image takes no focus")
	}
	key := func(c rune) tui.Event { return tui.KeyEvent{Kind: tui.KeyPress, Code: c} }
	wheel := func(b tui.MouseButton) tui.Event { return tui.MouseEvent{Kind: tui.MouseWheel, Button: b} }
	for _, step := range []struct {
		ev        tui.Event
		left, top int
		name      string
	}{
		{key(tui.KeyDown), 0, 20, "↓ a row"},
		{key('j'), 0, 40, "j a row"},
		{key('k'), 0, 20, "k back a row"},
		{key('l'), 40, 20, "l four columns"},
		{key(tui.KeyRight), 80, 20, "→"},
		{wheel(tui.WheelRight), 110, 20, "the wheel sideways"},
		{key('l'), 150, 20, "l"},
		{key('l'), 190, 20, "l"},
		{key('l'), 200, 20, "never past the right edge"},
		{key('h'), 160, 20, "h"},
		{key(']'), 160, 100, "] the cells less a row"},
		{key(tui.KeyPageDown), 160, 180, "Page Down"},
		{wheel(tui.WheelDown), 160, 240, "the wheel, three rows"},
		{key(tui.KeyEnd), 160, 900, "End: the bottom"},
		{key('j'), 160, 900, "never past the bottom"},
		{key('['), 160, 820, "["},
		{wheel(tui.WheelUp), 160, 760, "the wheel up"},
		{key(tui.KeyHome), 160, 0, "Home: the top"},
		{key(tui.KeyUp), 160, 0, "never past the top"},
	} {
		if !m.HandleEvent(step.ev) {
			t.Errorf("%s: not handled", step.name)
		}
		if c := clip(); c != (tui.Rect{X: step.left, Y: step.top, W: 100, H: 100}) {
			t.Errorf("%s: view %+v, want from %d,%d", step.name, c, step.left, step.top)
		}
	}
	m.HandleEvent(key(tui.KeyEnd))
	m.SetPNG(tallPNG(t, 300, 300)) // a shorter page keeps as much of the corner as it reaches
	if c := clip(); c.Y != 200 || c.X != 160 {
		t.Errorf("a shorter PNG left the view at %+v", c)
	}

	still := NewImage()
	still.SetPNG(tallPNG(t, 100, 1000))
	still.cols, still.rows = 10, 5
	if img, _ := still.Image(); !img.Clip.Empty() || still.AcceptsFocus() || still.HandleEvent(key(tui.KeyDown)) {
		t.Errorf("an Image that does not scroll clipped %+v, or took focus or keys", img.Clip)
	}
}

func pngSize(t *testing.T, b []byte) (int, int) {
	t.Helper()
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}

// A whole page: as tall as its content and no taller, at the width asked; twice as tall at a zoom
// of 2, laid out half as wide; as wide as its content when Wide; and what a script drew after a
// delay is drawn.
func TestRasterizeHTMLPage(t *testing.T) {
	if _, ok := HTMLRasterizer(); !ok {
		t.Skip("no headless chromium or chrome is installed")
	}
	page := func(body string, p Page) (int, int, []byte) {
		t.Helper()
		html := `<!doctype html><html style="background:#fff"><body style="margin:0">` + body + `</body></html>`
		b, err := RasterizeHTMLPage(context.Background(), []byte(html), p)
		if err != nil && strings.Contains(err.Error(), "No usable sandbox") {
			t.Skip("the installed browser has no usable sandbox here")
		}
		if err != nil {
			t.Fatal(err)
		}
		w, h := pngSize(t, b)
		return w, h, b
	}
	tall := `<div style="height:1500px;background:#000"></div>`
	if w, h, _ := page(tall, Page{Width: 400, MinHeight: 300}); w != 400 || h != 1500 {
		t.Errorf("a 1500px page is %d×%d, want 400×1500", w, h)
	}
	if w, h, _ := page(`<div style="height:100px;background:#000"></div>`, Page{Width: 400, MinHeight: 300}); w != 400 || h != 300 {
		t.Errorf("a short page is %d×%d, want its minimum, 400×300", w, h)
	}
	if w, h, _ := page(tall, Page{Width: 400, MinHeight: 300, Scale: 2}); w != 400 || h != 3000 {
		t.Errorf("at a zoom of 2 the page is %d×%d, want 400×3000", w, h)
	}
	if w, _, _ := page(`<div style="width:900px;height:50px;background:#000"></div>`, Page{Width: 400, MinHeight: 100, Wide: true}); w != 900 {
		t.Errorf("a wide page is %d wide, want its content's 900", w)
	}
	// a script that draws after 300ms has drawn by the time of the screenshot
	_, h, _ := page(`<div id="d"></div><script>setTimeout(function(){document.getElementById("d").style.cssText="height:2000px;background:#000"},300)</script>`,
		Page{Width: 400, MinHeight: 100})
	if h != 2000 {
		t.Errorf("the script's drawing is %d tall, want 2000: the screenshot did not wait for it", h)
	}
}
