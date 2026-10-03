package widget

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
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
