// Command scene3d renders a textured 3D scene on its own GPU device and shows it in a pixel pane
// beside a cell sidebar, with the frame rate in the pane's top-right corner.
//
//	go run ./examples/scene3d                  # Vulkan (Linux) or Metal (macOS) when zero-copy, else the CPU
//	go run ./examples/scene3d -producer=cpu    # the CPU producer, copied into the window
//
// It is golib/gui v0.2.0's pieces together: a pixel root of panes (WithPanes), a Stream of
// frames from another device (FrameCaps, Push, Receipt), a CanvasStream for the overlay, and the
// zero-copy renderer on Linux (WithZeroCopyFrames). Space pauses; q quits.
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yongjohnlee80/gio/op/extimage"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/style"

	"github.com/yongjohnlee80/golib/gui"
)

func main() {
	want := flag.String("producer", "auto", "the scene's producer: auto, cpu or "+gpuProducerName)
	flag.Parse()
	gui.Main(func() error { return run(*want) })
}

func run(want string) error {
	ctx, quit := context.WithCancel(context.Background())
	defer quit()
	st := &stats{}
	vp := &viewport{}
	side := &sidebar{stats: st, quit: quit}
	panes := gui.NewPanes(gui.Row(
		gui.Sized(gui.Fixed(260), gui.Cells(side)),
		gui.Sized(gui.Flex(1), gui.Pixels(vp)),
	))
	b := gui.NewBackend(gui.WithTitle("scene3d"), gui.WithSize(1280, 760), gui.WithPanes(panes), gui.WithZeroCopyFrames())
	vp.frames, vp.fps = b.NewStream(), b.NewCanvasStream()
	app := tui.NewApp(panes, tui.WithBackend(b))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := loop(ctx, b, vp, st, want); err != nil {
			st.setErr(err)
		}
	}()
	err := app.Run(ctx)
	quit()
	vp.frames.Close()
	vp.fps.Close()
	wg.Wait()
	return err
}

// loop renders the scene at the display's pace: it renders a frame, pushes it, and renders the
// next when the window has drawn it (or superseded it). Its producer follows the window's caps.
func loop(ctx context.Context, b *gui.Backend, vp *viewport, st *stats, want string) error {
	caps, err := waitCaps(ctx, b)
	if err != nil {
		return err
	}
	prod, err := choose(want, caps)
	if err != nil {
		return err
	}
	defer func() { prod.close() }()
	st.setProducer(prod, caps)
	start := time.Now()
	var paused time.Duration // time spent paused, taken out of the scene's clock
	var pausedAt time.Time
	window := time.Now()
	drawn := 0
	for ctx.Err() == nil {
		size := vp.pixels()
		if size.X <= 0 || size.Y <= 0 || st.paused.Load() {
			if st.paused.Load() && pausedAt.IsZero() {
				pausedAt = time.Now()
			}
			select { // the pane has no size yet, or the scene is paused: look again soon
			case <-ctx.Done():
				return nil
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}
		if !pausedAt.IsZero() {
			paused += time.Since(pausedAt)
			pausedAt = time.Time{}
		}
		t0 := time.Now()
		f, err := prod.render(time.Since(start).Seconds()-paused.Seconds(), size)
		if err != nil {
			return err
		}
		st.renderTime.Store(int64(time.Since(t0)))
		r := vp.frames.Push(f)
		select {
		case <-r.Done():
		case <-ctx.Done():
			return nil
		}
		switch r.Outcome() {
		case gui.Drawn:
			drawn++
		case gui.Unsupported: // the window's device changed: choose again
			prod.close()
			if caps, err = waitCaps(ctx, b); err != nil {
				return err
			}
			if prod, err = choose(want, caps); err != nil {
				return err
			}
			st.setProducer(prod, caps)
		case gui.Closed:
			return nil
		}
		if el := time.Since(window); el >= 500*time.Millisecond {
			fps := float64(drawn) / el.Seconds()
			st.fps.Store(math.Float64bits(fps))
			st.size.Store(&size)
			vp.pushFPS(fps)
			window, drawn = time.Now(), 0
		}
	}
	return nil
}

// waitCaps is the window's caps, once it has a GPU.
func waitCaps(ctx context.Context, b *gui.Backend) (caps extimage.Caps, err error) {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if c, ok := b.FrameCaps(); ok {
			return c, nil
		}
		select {
		case <-ctx.Done():
			return caps, ctx.Err()
		case <-tick.C:
		}
	}
}

// stats is what the sidebar shows; the loop writes it.
type stats struct {
	mu         sync.Mutex
	producer   string
	path       string
	driver     string
	err        error
	fps        atomic.Uint64 // float64 bits
	renderTime atomic.Int64  // time.Duration of the last render
	size       atomic.Pointer[image.Point]
	paused     atomic.Bool
}

func (s *stats) setProducer(p producer, caps extimage.Caps) {
	s.mu.Lock()
	s.producer, s.path, s.driver = p.name(), p.path(), caps.Driver
	s.mu.Unlock()
}

func (s *stats) setErr(err error) {
	s.mu.Lock()
	s.err = err
	s.mu.Unlock()
}

// viewport is the pixel pane: the scene's frames, and the frame rate over its top-right corner.
type viewport struct {
	frames *gui.Stream
	fps    *gui.CanvasStream
	size   atomic.Pointer[image.Point] // the pane in device pixels, from its last paint
}

func (v *viewport) Init(*tui.Context)                 {}
func (v *viewport) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (v *viewport) Render(tui.Surface)                {}
func (v *viewport) HandleEvent(tui.Event) bool        { return false }

// NativeView is the pane's View: it records the two stream slots, and its size for the loop.
func (v *viewport) NativeView() (any, bool) {
	return gui.ViewFunc(func(c gui.Canvas) {
		sz, scale := c.Size(), c.Scale()
		px := image.Pt(int(math.Round(float64(sz.W*scale))), int(math.Round(float64(sz.H*scale))))
		v.size.Store(&px)
		gui.DrawStream(c, v.frames, gui.Rect{W: sz.W, H: sz.H})
		gui.DrawCanvasStream(c, v.fps, gui.Rect{X: sz.W - fpsW - 12, Y: 12, W: fpsW, H: fpsH})
	}), true
}

func (v *viewport) pixels() image.Point {
	if p := v.size.Load(); p != nil {
		return *p
	}
	return image.Point{}
}

const fpsW, fpsH = 112, 30 // the overlay, in dp

// pushFPS draws the overlay with the rate.
func (v *viewport) pushFPS(fps float64) {
	label := fmt.Sprintf("%.0f fps", fps)
	v.fps.Push(gui.Size{W: fpsW, H: fpsH}, func(c gui.Canvas) {
		c.FillRRect(gui.Rect{W: fpsW, H: fpsH}, 6, gui.Brush{Color: color.NRGBA{A: 0xb0}})
		t := c.Text().Layout(label, gui.Font{Family: gui.CellFamily, Size: 15, Bold: true}, fpsW)
		c.DrawText(t, gui.Pt(fpsW-10-t.Width, (fpsH-t.Height)/2), gui.Brush{Color: color.NRGBA{R: 0x9e, G: 0xe0, B: 0x7a, A: 0xff}})
	})
}

// sidebar is the cell pane: what draws the scene, how, and how fast; Space pauses, q quits.
type sidebar struct {
	ctx   *tui.Context
	stats *stats
	quit  func()
}

func (s *sidebar) Init(ctx *tui.Context) {
	s.ctx = ctx
	ctx.Every(500 * time.Millisecond)
	ctx.RequestFocus() // the keys are the sidebar's: the viewport takes none
}

func (s *sidebar) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
}

func (s *sidebar) AcceptsFocus() bool { return true }

func (s *sidebar) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.TickEvent:
		s.ctx.MarkDirty()
		return true
	case tui.KeyEvent:
		if e.Kind != tui.KeyPress {
			return false
		}
		switch e.Code {
		case ' ':
			s.stats.paused.Store(!s.stats.paused.Load())
			s.ctx.MarkDirty()
			return true
		case 'q', key.Escape:
			s.quit()
			return true
		}
	}
	return false
}

func (s *sidebar) Render(sf tui.Surface) {
	st := s.stats
	st.mu.Lock()
	prod, path, driver, err := st.producer, st.path, st.driver, st.err
	st.mu.Unlock()
	title := style.New().Foreground(style.TokenPrimary).Bold(true)
	label := style.New().Foreground(style.TokenTextMuted)
	value := style.New().Foreground(style.TokenForeground)
	y := 1
	line := func(st style.Style, s string) {
		x := 2
		for g := range tui.Graphemes(s) {
			sf.SetCell(x, y, g, st)
			x += sf.StringWidth(g)
		}
		y++
	}
	field := func(name, v string) {
		line(label, name)
		line(value, "  "+v)
		y++
	}
	line(title, "scene3d")
	y++
	if prod == "" {
		field("producer", "waiting for the GPU")
	} else {
		field("producer", prod)
		field("window draws with", driver)
		field("frames", path)
	}
	if p := st.size.Load(); p != nil {
		field("frame size", fmt.Sprintf("%d × %d px", p.X, p.Y))
	}
	field("frame rate", fmt.Sprintf("%.1f fps", math.Float64frombits(st.fps.Load())))
	field("render time", fmt.Sprintf("%.2f ms", float64(st.renderTime.Load())/1e6))
	if st.paused.Load() {
		line(title, "paused")
		y++
	}
	if err != nil {
		line(title, "error")
		line(value, err.Error())
		y++
	}
	line(label, "Space  pause")
	line(label, "q      quit")
}
