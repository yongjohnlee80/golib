package widget

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// RASTERIZERS — SVG and HTML to PNG, for an Image, by optional tools found at runtime: no Go
// dependency, and nothing at all when a tool is missing (the host shows its fallback). Each run is
// bounded in time and in what goes in and comes out, and has no network: the HTML renderer is a
// headless browser with every request routed nowhere and a throwaway profile.

// The rasterizers' bounds.
const (
	RasterTimeout   = 15 * time.Second
	MaxRasterInput  = 8 << 20  // the SVG or HTML handed to a tool
	MaxRasterOutput = 32 << 20 // the PNG read back
	maxRasterPixels = 4096     // the widest or tallest image asked for
)

// ErrNoRasterizer is a rasterizer whose tool is not installed.
var ErrNoRasterizer = errors.New("widget: no rasterizer installed")

// The tools each rasterizer tries, first found wins; tests replace them.
var (
	svgTools  = []string{"rsvg-convert"}
	htmlTools = []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"}
	lookPath  = exec.LookPath
)

// SVGRasterizer is the tool RasterizeSVG would run, and whether one is installed.
func SVGRasterizer() (string, bool) { return find(svgTools) }

// HTMLRasterizer is the tool RasterizeHTML would run, and whether one is installed.
func HTMLRasterizer() (string, bool) { return find(htmlTools) }

func find(tools []string) (string, bool) {
	for _, t := range tools {
		if p, err := lookPath(t); err == nil {
			return p, true
		}
	}
	return "", false
}

// RasterizeSVG renders svg to a PNG width pixels wide, its height in proportion (rsvg-convert).
func RasterizeSVG(ctx context.Context, svg []byte, width int) ([]byte, error) {
	tool, ok := SVGRasterizer()
	if !ok {
		return nil, fmt.Errorf("%w: rsvg-convert", ErrNoRasterizer)
	}
	if len(svg) > MaxRasterInput {
		return nil, fmt.Errorf("widget: the SVG is over %d MiB", MaxRasterInput>>20)
	}
	width = clampPixels(width)
	ctx, cancel := context.WithTimeout(ctx, RasterTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, "--format", "png", "--width", strconv.Itoa(width), "--keep-aspect-ratio")
	cmd.Stdin = bytes.NewReader(svg)
	return runBounded(cmd)
}

// RasterizeHTML renders html in a headless browser at width × height pixels and returns the
// viewport as a PNG, once the page's scripts have settled (a diagram drawn by a script is drawn). The page cannot reach the network: every host resolves nowhere and every
// request goes to a proxy that is not there. A page that needs the network renders without it.
// The browser's own sandbox always stays on: where it cannot run (a system that forbids user
// namespaces), the browser refuses, the error says so, and the host shows its fallback.
func RasterizeHTML(ctx context.Context, html []byte, width, height int) ([]byte, error) {
	return rasterizeHTML(ctx, html, clampPixels(width), clampPixels(height), 1)
}

// MaxPageHeight is the tallest, and MaxPageWidth the widest, a page RasterizeHTMLPage renders, in
// pixels: a larger one is cut.
const (
	MaxPageHeight = 16384
	MaxPageWidth  = 4096
)

// Page is how RasterizeHTMLPage renders: Width pixels wide (with Wide, as wide as the page's
// content, at least Width, at most MaxPageWidth), as tall as the page, at least MinHeight and at
// most MaxHeight (0: MaxPageHeight), at Scale — a browser's zoom: 2 draws everything twice the
// size, the page laid out half as wide; 0 is 1. The browser's time grows with the window's area,
// so a Wide page that is never long (a diagram) does well to bound its height. Background is the
// page's own colour, "#rrggbb": what is cut below and beside the content. Without it the
// bottom-right pixel is taken for it, which is wrong for content that reaches that corner.
type Page struct {
	Width, MinHeight, MaxHeight int
	Scale                       float64
	Wide                        bool
	Background                  string
}

// RasterizeHTMLPage renders the whole of html as a PNG, for an Image that scrolls (see Page). The
// page is laid out in a window as tall as MaxPageHeight allows — as wide as MaxPageWidth allows,
// when Wide — and the rows below its content and the columns to its right, all of the colour of
// the bottom-right pixel, are cut. A page sized to the window (100vh) fills it.
func RasterizeHTMLPage(ctx context.Context, html []byte, p Page) ([]byte, error) {
	scale := p.Scale
	if scale <= 0 {
		scale = 1
	}
	width := clampPixels(p.Width)
	window := width
	if p.Wide {
		window = MaxPageWidth
	}
	height := MaxPageHeight
	if p.MaxHeight > 0 {
		height = min(p.MaxHeight, MaxPageHeight)
	}
	b, err := rasterizeHTML(ctx, html, int(float64(window)/scale), int(float64(height)/scale), scale)
	if err != nil {
		return nil, err
	}
	return trimBlank(b, width, max(1, min(p.MinHeight, height)), p.Background)
}

// trimBlank cuts the rows at the bottom and the columns at the right of a PNG that are all of the
// background — background, "#rrggbb", or else the bottom-right pixel's colour — keeping at least
// minW × minH.
func trimBlank(b []byte, minW, minH int, background string) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("widget: the browser's screenshot: %w", err)
	}
	r := img.Bounds()
	br, bg, bb, ba := img.At(r.Max.X-1, r.Max.Y-1).RGBA()
	var rgb [3]uint8
	if n, _ := fmt.Sscanf(background, "#%02x%02x%02x", &rgb[0], &rgb[1], &rgb[2]); n == 3 {
		br, bg, bb, ba = color.RGBA{rgb[0], rgb[1], rgb[2], 0xff}.RGBA()
	}
	blank := func(x, y int) bool {
		cr, cg, cb, ca := img.At(x, y).RGBA()
		return cr == br && cg == bg && cb == bb && ca == ba
	}
	bottom := r.Max.Y
	for bottom > r.Min.Y+minH {
		all := true
		for x := r.Min.X; x < r.Max.X && all; x++ {
			all = blank(x, bottom-1)
		}
		if !all {
			break
		}
		bottom--
	}
	right := r.Max.X
	for right > r.Min.X+minW {
		all := true
		for y := r.Min.Y; y < bottom && all; y++ {
			all = blank(right-1, y)
		}
		if !all {
			break
		}
		right--
	}
	if bottom == r.Max.Y && right == r.Max.X {
		return b, nil
	}
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return b, nil
	}
	var out bytes.Buffer
	if err := png.Encode(&out, sub.SubImage(image.Rect(r.Min.X, r.Min.Y, right, bottom))); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// rasterizeHTML renders html in a window width × height (the browser's own pixels) at scale.
func rasterizeHTML(ctx context.Context, html []byte, width, height int, scale float64) ([]byte, error) {
	tool, ok := HTMLRasterizer()
	if !ok {
		return nil, fmt.Errorf("%w: chromium or google-chrome", ErrNoRasterizer)
	}
	if len(html) > MaxRasterInput {
		return nil, fmt.Errorf("widget: the HTML is over %d MiB", MaxRasterInput>>20)
	}
	dir, err := os.MkdirTemp("", "golib-raster-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	page, out := filepath.Join(dir, "page.html"), filepath.Join(dir, "page.png")
	if err := os.WriteFile(page, html, 0o600); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, RasterTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool,
		"--headless=new", "--disable-gpu", "--hide-scrollbars", "--mute-audio", "--no-first-run",
		"--disable-extensions", "--disable-background-networking", "--disable-sync", "--disable-default-apps",
		"--proxy-server=127.0.0.1:9", "--proxy-bypass-list=<-loopback>", "--host-resolver-rules=MAP * ~NOTFOUND",
		"--user-data-dir="+filepath.Join(dir, "profile"),
		// virtual time runs on until the page is idle (its scripts done) or the budget is spent
		"--virtual-time-budget=10000",
		"--force-device-scale-factor="+strconv.FormatFloat(scale, 'f', -1, 64),
		"--window-size="+strconv.Itoa(width)+","+strconv.Itoa(height),
		"--screenshot="+out, "file://"+page)
	if _, err := runBounded(cmd); err != nil {
		return nil, err
	}
	f, err := os.Open(out)
	if err != nil {
		return nil, fmt.Errorf("widget: the browser wrote no screenshot: %w", err)
	}
	defer f.Close()
	return readBounded(f)
}

// runBounded runs cmd, its output capped; a failure carries the start of what it said.
func runBounded(cmd *exec.Cmd) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitWriter{w: &stdout, n: MaxRasterOutput + 1}
	cmd.Stderr = &limitWriter{w: &stderr, n: 2048}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("widget: %s: %w: %s", filepath.Base(cmd.Path), err, bytes.TrimSpace(stderr.Bytes()))
	}
	if stdout.Len() > MaxRasterOutput {
		return nil, fmt.Errorf("widget: %s wrote over %d MiB", filepath.Base(cmd.Path), MaxRasterOutput>>20)
	}
	return stdout.Bytes(), nil
}

func readBounded(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, MaxRasterOutput+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxRasterOutput {
		return nil, fmt.Errorf("widget: the screenshot is over %d MiB", MaxRasterOutput>>20)
	}
	return b, nil
}

func clampPixels(n int) int { return max(1, min(n, maxRasterPixels)) }

// limitWriter keeps the first n bytes and counts the rest as written, so a tool is never blocked
// on a full pipe and an oversized output is still detected.
type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}
