package widget

import (
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// ProgressBar renders determinate progress, an indeterminate sweep, or a
// one-cell spinner. Animation is driven by an addressed
// TickEvent registration (Context.Every) held ONLY while the
// bar is animating: it is registered on entering the indeterminate state and
// cancelled on leaving it (and at unmount, automatically), so a finished
// progress bar costs zero wakeups and the idle-app zero-byte guarantee
// holds.
type ProgressBar struct {
	Base
	progress      float64
	indeterminate bool
	frames        []string // spinner frames; nil = sweeping block
	interval      time.Duration
	phase         int
	cancel        func()

	filled style.Style
	empty  style.Style
}

var _ tui.Component = (*ProgressBar)(nil)

// ProgressBarOption customizes a ProgressBar under construction.
type ProgressBarOption func(*ProgressBar)

// WithSpinner switches the indeterminate rendering to a one-cell spinner
// cycling frames every interval. Empty frames or a non-positive interval
// panic.
func WithSpinner(frames []string, interval time.Duration) ProgressBarOption {
	if len(frames) == 0 {
		panic("widget: WithSpinner: no frames")
	}
	if interval <= 0 {
		panic("widget: WithSpinner: non-positive interval")
	}
	return func(p *ProgressBar) {
		p.frames = append([]string(nil), frames...)
		p.interval = interval
	}
}

// WithProgressStyles sets the filled and empty cell styles (defaults:
// filled style.TokenPrimary foreground, empty style.TokenTextMuted faint).
func WithProgressStyles(filled, empty style.Style) ProgressBarOption {
	return func(p *ProgressBar) { p.filled, p.empty = filled, empty }
}

// NewProgressBar builds a determinate bar at 0.
func NewProgressBar(opts ...ProgressBarOption) *ProgressBar {
	p := &ProgressBar{
		interval: 100 * time.Millisecond,
		filled:   style.New().Foreground(style.TokenPrimary),
		empty:    style.New().Foreground(style.TokenTextMuted).Faint(true),
	}
	for _, o := range opts {
		if o != nil {
			o(p)
		}
	}
	return p
}

// Init re-arms the animation timer when remounted in the indeterminate
// state.
func (p *ProgressBar) Init(ctx *tui.Context) {
	p.Base.Init(ctx)
	p.cancel = nil
	if p.indeterminate {
		p.startAnim()
	}
	ctx.OnUnmount(p.stopAnim)
}

// SetProgress switches to determinate mode at f (clamped to [0, 1]) and
// stops the animation timer.
func (p *ProgressBar) SetProgress(f float64) {
	p.progress = min(max(f, 0), 1)
	p.indeterminate = false
	p.stopAnim()
	p.MarkDirty()
}

// SetIndeterminate switches to the animated mode (sweeping block, or the
// WithSpinner variant) and registers the tick timer.
func (p *ProgressBar) SetIndeterminate() {
	if p.indeterminate {
		return
	}
	p.indeterminate = true
	p.startAnim()
	p.MarkDirty()
}

func (p *ProgressBar) startAnim() {
	if p.ctx == nil || p.cancel != nil {
		return
	}
	p.cancel = p.ctx.Every(p.interval)
}

func (p *ProgressBar) stopAnim() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
}

// HandleEvent consumes this bar's animation ticks.
func (p *ProgressBar) HandleEvent(ev tui.Event) bool {
	if te, ok := ev.(tui.TickEvent); ok && te.Owner == p.NodeID() {
		p.phase++
		p.MarkDirty()
		return true
	}
	return false
}

// Layout: one row; one cell for a spinner, greedy width otherwise.
func (p *ProgressBar) Layout(c tui.Constraints) tui.Size {
	if p.frames != nil {
		return c.Constrain(tui.Size{W: 1, H: 1})
	}
	return c.Constrain(tui.Size{W: boundedMax(c.MaxW, c.MinW), H: 1})
}

// eighth blocks for sub-cell resolution (index = eighths filled).
var eighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// Render paints the current mode.
func (p *ProgressBar) Render(s tui.Surface) {
	w := s.Size().W
	if w <= 0 {
		return
	}
	if p.frames != nil {
		if p.indeterminate {
			s.SetCell(0, 0, p.frames[p.phase%len(p.frames)], p.filled)
		} else {
			s.SetCell(0, 0, " ", p.empty)
		}
		return
	}
	if p.indeterminate {
		// Sweeping block: a w/4-cell block cycling across the bar.
		bw := max(w/4, 1)
		pos := p.phase % (w + bw)
		s.Fill(tui.Rect{X: 0, Y: 0, W: w, H: 1}, "░", p.empty)
		x0 := max(pos-bw, 0)
		x1 := min(pos, w)
		if x1 > x0 {
			s.Fill(tui.Rect{X: x0, Y: 0, W: x1 - x0, H: 1}, "█", p.filled)
		}
		return
	}
	// Determinate: full cells plus one partial block in eighths.
	cells := p.progress * float64(w)
	full := int(cells)
	frac := int((cells - float64(full)) * 8)
	s.Fill(tui.Rect{X: 0, Y: 0, W: w, H: 1}, "░", p.empty)
	if full > 0 {
		s.Fill(tui.Rect{X: 0, Y: 0, W: full, H: 1}, "█", p.filled)
	}
	if full < w && frac > 0 {
		s.SetCell(full, 0, eighths[frac], p.filled)
	}
}
