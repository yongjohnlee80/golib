package widget

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
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
// pixels: a larger one is cut. firstPageHeight is the window it tries first.
const (
	MaxPageHeight = 16384
	MaxPageWidth  = 4096

	firstPageHeight = 4096
)

// Page is how RasterizeHTMLPage renders: Width pixels wide (with Wide, as wide as the page's
// content, at least Width, at most MaxPageWidth), as tall as the page, at least MinHeight and at
// most MaxHeight (0: MaxPageHeight), at Scale — a browser's zoom: 2 draws everything twice the
// size, the page laid out half as wide; 0 is 1. The browser's time grows with the window's area,
// so a Wide page that is never long (a diagram) does well to bound its height. Background is the
// page's own colour, "#rrggbb": what is cut beside the content and fills a short page. Without it
// the bottom-right pixel is taken for it, which is wrong for content that reaches that corner.
type Page struct {
	Width, MinHeight, MaxHeight int
	Scale                       float64
	Wide                        bool
	Background                  string
}

// RasterizeHTMLPage renders the whole of html as a PNG, for an Image that scrolls (see Page).
//
// Where the page ends is marked, not guessed: an end marker — a bar of a reserved colour, after
// everything in the page's flow — is added before </body>, and the page is cut just above it. A
// blank stretch of the page is no evidence of its end (content may follow it). The page is laid
// out first in a window firstPageHeight rows tall, and again in the whole height only when the
// marker is not in that first window. A page whose policy keeps the marker unstyled, so that it
// never shows, is cut where the whole-height window shows nothing more: its rows of the background
// at the bottom. To the right, a Wide page is cut where the window shows nothing more. A page
// sized to the window (100vh) fills it. One shorter than MinHeight is filled to it with its
// background.
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
	html = withEndMarker(html)
	minH := max(1, min(p.MinHeight, height))
	render := func(h int) (image.Image, error) {
		b, err := rasterizeHTML(ctx, html, int(float64(window)/scale), int(float64(h)/scale), scale)
		if err != nil {
			return nil, err
		}
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			return nil, fmt.Errorf("widget: the browser's screenshot: %w", err)
		}
		return img, nil
	}
	// most pages are short, and the browser's time grows with the window
	first := min(firstPageHeight, height)
	img, err := render(first)
	if err != nil {
		return nil, err
	}
	end, found := endMarkerRow(img)
	if !found && first < height {
		if img, err = render(height); err != nil {
			return nil, err
		}
		end, found = endMarkerRow(img)
	}
	return cutPage(img, width, minH, end, found, p.Background)
}

// endMarker is the bar withEndMarker adds: its colour is reserved, and it is tall enough to show
// at the smallest zoom.
const endMarker = `<div style="display:block;clear:both;height:6px;margin:0;padding:0;border:0;background:#fe01fd"></div>`

// withEndMarker adds the end marker before the page's last </body>, or at its end.
func withEndMarker(html []byte) []byte {
	i := bytes.LastIndex(bytes.ToLower(html), []byte("</body>"))
	if i < 0 {
		return append(append([]byte(nil), html...), endMarker...)
	}
	out := make([]byte, 0, len(html)+len(endMarker))
	out = append(out, html[:i]...)
	out = append(out, endMarker...)
	return append(out, html[i:]...)
}

// endMarkerRow is the first row of img holding a run of the end marker's colour, and whether any
// does.
func endMarkerRow(img image.Image) (int, bool) {
	r := img.Bounds()
	marker := func(x, y int) bool {
		cr, cg, cb, _ := img.At(x, y).RGBA()
		near := func(v uint32, want uint32) bool { return v>>8+3 >= want && v>>8 <= want+3 }
		return near(cr, 0xfe) && near(cg, 0x01) && near(cb, 0xfd)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		run := 0
		for x := r.Min.X; x < r.Max.X; x++ {
			if !marker(x, y) {
				run = 0
				continue
			}
			if run++; run >= 6 {
				return y, true
			}
		}
	}
	return 0, false
}

// cutPage cuts img at the page's end — just above the end marker's row when found, else where its
// rows of the background at the bottom begin — and to the right where its columns of the
// background begin (never narrower than minW), and fills it to minH with the background.
// background is "#rrggbb", or else the bottom-right pixel's colour.
func cutPage(img image.Image, minW, minH, end int, found bool, background string) ([]byte, error) {
	r := img.Bounds()
	bg := color.RGBAModel.Convert(img.At(r.Max.X-1, r.Max.Y-1)).(color.RGBA)
	var rgb [3]uint8
	if n, _ := fmt.Sscanf(background, "#%02x%02x%02x", &rgb[0], &rgb[1], &rgb[2]); n == 3 {
		bg = color.RGBA{rgb[0], rgb[1], rgb[2], 0xff}
	}
	blank := func(x, y int) bool { return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA) == bg }
	bottom := r.Max.Y
	if found {
		bottom = end
	} else {
		for bottom > r.Min.Y {
			all := true
			for x := r.Min.X; x < r.Max.X && all; x++ {
				all = blank(x, bottom-1)
			}
			if !all {
				break
			}
			bottom--
		}
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
	out := image.NewRGBA(image.Rect(0, 0, right-r.Min.X, max(bottom-r.Min.Y, minH)))
	draw.Draw(out, out.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	draw.Draw(out, image.Rect(0, 0, right-r.Min.X, bottom-r.Min.Y), img, r.Min, draw.Src)
	var b bytes.Buffer
	if err := png.Encode(&b, out); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
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
