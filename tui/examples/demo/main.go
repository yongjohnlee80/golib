// Command demo is the ADR-0001 §5.3 acceptance demo for golib/tui: a split
// layout with focus cycling, a text input, an async-filled list (App.Go), a
// BufferView streaming colored output, and a status bar — running on
// term.Open when stdout is a terminal. Its full interaction script runs
// deterministically against tui.TestBackend in demo_test.go (no PTY).
//
// Keys: Tab / Shift+Tab cycle the panes (framework traversal), Enter
// submits the input, Alt+arrows resize the splits, Ctrl+C quits.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/term"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// demo is the root controller component: it mounts the widget tree, wires
// the Bus subscriptions, and owns the async tasks (ADR-0005 App.Go).
type demo struct {
	root   tui.Component
	items  *widget.List[string]
	input  *widget.TextInput
	log    *widget.BufferView
	status *widget.StatusBar

	quit      func()
	stream    bool // periodic colored ticker into the log (off in tests)
	submitted int

	ctx *tui.Context
}

// newDemo assembles the §5.3 layout:
//
//	┌ Items ─┐┌ Input ────────────┐
//	│ (list) ││ (text input)      │
//	│        │└───────────────────┘
//	│        │┌ Log ──────────────┐
//	│        ││ (buffer view)     │
//	└────────┘└───────────────────┘
//	 status bar ───────────────────
func newDemo(quit func(), stream bool) *demo {
	d := &demo{
		items:  widget.NewList(widget.WithItems[string](nil, func(s string) string { return s })),
		input:  widget.NewTextInput(widget.WithPlaceholder("type and press Enter")),
		log:    widget.NewBufferView(),
		status: widget.NewStatusBar(),
		quit:   quit,
		stream: stream,
	}
	right := widget.NewSplit(widget.Vertical,
		widget.NewBox(d.input, widget.WithTitle("Input")),
		widget.NewBox(d.log, widget.WithTitle("Log"), widget.WithStatus("End: follow")),
		widget.WithRatio(0.25), widget.WithMinSizes(3, 4))
	body := widget.NewSplit(widget.Horizontal,
		widget.NewBox(d.items, widget.WithTitle("Items")),
		right, widget.WithRatio(0.3), widget.WithMinSizes(12, 20))
	dock := tui.NewDock()
	dock.Pin(tui.DockBottom, d.status)
	dock.Add(body)
	d.root = widget.NewOverlayHost(dock)
	return d
}

// Init mounts the tree, seeds the status bar, subscribes to input submits,
// and starts the async work.
func (d *demo) Init(ctx *tui.Context) {
	d.ctx = ctx
	ctx.Mount(d.root)
	d.status.SetLeft("golib/tui demo")
	d.status.SetCenter("Tab: focus · Enter: submit · Ctrl+C: quit")
	d.status.SetRight("loading…")

	// Submits append to the log and clear the input (Bus, ADR-0005 §2.7).
	tui.SubscribeScoped(ctx, func(ev widget.SubmitEvent) {
		if ev.Owner != d.input.NodeID() || ev.Value == "" {
			return
		}
		d.submitted++
		fmt.Fprintf(d.log.Writer(), "\x1b[32m✓\x1b[0m submitted: \x1b[1m%s\x1b[0m\n", ev.Value)
		d.input.SetValue("")
		d.status.SetRight(fmt.Sprintf("%d submitted", d.submitted))
	})

	// Async list fill (ADR-0001 §5.3): the TaskResult is addressed back to
	// this controller.
	ctx.Go(func(context.Context) (any, error) {
		return []string{"alpha", "beta", "gamma", "delta", "epsilon"}, nil
	})

	if d.stream {
		w := d.log.Writer()
		ctx.Go(func(c context.Context) (any, error) {
			t := time.NewTicker(400 * time.Millisecond)
			defer t.Stop()
			for i := 1; ; i++ {
				select {
				case <-c.Done():
					return nil, nil
				case at := <-t.C:
					fmt.Fprintf(w, "\x1b[36m%s\x1b[0m tick \x1b[33m#%d\x1b[0m\n",
						at.Format("15:04:05"), i)
				}
			}
		})
	}
}

func (d *demo) Layout(c tui.Constraints) tui.Size {
	sz := d.ctx.LayoutChild(d.root, c)
	d.ctx.PlaceChild(d.root, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
	return sz
}

func (d *demo) Render(tui.Surface) {}

// HandleEvent: the async fill result, and the global quit key.
func (d *demo) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.TaskResult:
		if e.Err != nil {
			d.status.SetRight("load failed: " + e.Err.Error())
			return true
		}
		if items, ok := e.Value.([]string); ok {
			d.items.SetItems(items)
			d.status.SetRight(fmt.Sprintf("%d items", len(items)))
			return true
		}
		return true // the stream task's nil result
	case tui.KeyEvent:
		if e.Kind == tui.KeyPress && e.Code == 'c' && e.Mods&tui.ModCtrl != 0 {
			d.quit()
			return true
		}
	}
	return false
}

func main() {
	backend, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui demo: cannot open the terminal:", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := tui.NewApp(newDemo(cancel, true), tui.WithBackend(backend))
	if err := app.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "tui demo:", err)
		os.Exit(1)
	}
}
