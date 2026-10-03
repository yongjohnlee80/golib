package widget

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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
// viewport as a PNG. The page cannot reach the network: every host resolves nowhere and every
// request goes to a proxy that is not there. A page that needs the network renders without it.
// The browser's own sandbox always stays on: where it cannot run (a system that forbids user
// namespaces), the browser refuses, the error says so, and the host shows its fallback.
func RasterizeHTML(ctx context.Context, html []byte, width, height int) ([]byte, error) {
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
		"--window-size="+strconv.Itoa(clampPixels(width))+","+strconv.Itoa(clampPixels(height)),
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
