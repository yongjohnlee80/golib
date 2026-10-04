package widget

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
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
