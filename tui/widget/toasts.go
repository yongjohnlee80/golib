package widget

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Toasts shows notifications stacked in a corner of the screen, as a desktop's toasts do: each a
// small box with its message and how long ago it came ("now", "15s ago").
//
//	╭──────────────────── now ─╮
//	│ saved notes/plan.md      │
//	╰──────────────────────────╯
//	╭─────────────────── 12s ago ─╮
//	│ embedding ██████░░░░ 612/1000 │
//	╰─────────────────────────────╯
//	 NORMAL  autodoc · kb                    ● semantic search   ← the status line, left clear
//
// # Lifetime
//
//   - A toast is shown when it is posted, if fewer than the maximum are showing (WithToastMax,
//     default 3); otherwise it waits in a queue, in order, and is shown when one goes. Its age
//     counts from when it was posted, so one shown late says so ("15s ago").
//   - A finished toast lingers (WithToastLinger, default 3s) from when it is showing and finished,
//     then goes.
//   - An ongoing toast (Toast.Ongoing: a long task's progress) stays until it is posted again
//     finished, or Done is called; then it lingers. Posting a toast with an ID already posted
//     replaces its message in place, so a progress toast updates where it is.
//
// # Placement
//
// It is a Float's content, attached to an OverlayHost with AttachTopmost, so it draws over the page
// and over any dialog opened after it, without taking a place in the layout; the Float is shown on
// the first post. It never takes the keyboard.
// The corner is WithToastCorner (default BottomRight); the newest toast is nearest it. WithToastMargin
// keeps rows clear at that edge, so a status line under the stack stays readable: those rows are
// not painted, and what is under them shows.
//
// # Concurrency
//
// Loop-goroutine-owned, like every widget: Post and Done run on the App's loop (App.Update).
type Toasts struct {
	Base
	float   *Float
	showing []*toastItem // oldest first; the newest is nearest the corner
	queue   []*toastItem
	corner  Anchor
	linger  time.Duration
	max     int
	margin  int
	width   int // a toast's widest, border included
	now     func() time.Time
	st      style.Style
	muted   style.Style
	stop    func() // the tick, while anything is showing or waiting
}

var _ tui.Component = (*Toasts)(nil)

// Toast is one notification.
type Toast struct {
	// ID names it: a post with an ID already posted replaces that toast's message in place. ""
	// is a toast of its own.
	ID string
	// Text is the message; it wraps to the toast's width.
	Text string
	// At is when it happened; zero is when it is posted.
	At time.Time
	// Ongoing keeps it showing until it is posted again without it, or Done: a task's progress.
	Ongoing bool
}

type toastItem struct {
	Toast
	finished  time.Time // when it was finished and showing; zero while ongoing or waiting
	finishedT bool      // it has finished (possibly while waiting)
}

// ToastsOption configures Toasts under construction.
type ToastsOption func(*Toasts)

// WithToastCorner is the corner the toasts stack in: TopLeft, TopRight, BottomLeft or BottomRight.
func WithToastCorner(a Anchor) ToastsOption { return func(t *Toasts) { t.corner = a } }

// WithToastLinger is how long a finished toast stays; at least a second.
func WithToastLinger(d time.Duration) ToastsOption {
	return func(t *Toasts) { t.linger = max(d, time.Second) }
}

// WithToastMax is how many toasts show at once; the others wait. At least one.
func WithToastMax(n int) ToastsOption { return func(t *Toasts) { t.max = max(n, 1) } }

// WithToastMargin keeps rows clear at the corner's edge: a status line's.
func WithToastMargin(rows int) ToastsOption { return func(t *Toasts) { t.margin = max(rows, 0) } }

// WithToastWidth is a toast's widest, border included (default 48 columns); it is never wider than
// the screen.
func WithToastWidth(cols int) ToastsOption { return func(t *Toasts) { t.width = max(cols, 8) } }

// WithToastClock replaces the clock (tests).
func WithToastClock(now func() time.Time) ToastsOption { return func(t *Toasts) { t.now = now } }

// WithToastStyle sets a toast's look, and the muted look of its border and age.
func WithToastStyle(st, muted style.Style) ToastsOption {
	return func(t *Toasts) { t.st, t.muted = st, muted }
}

// NewToasts builds the stack and the Float it shows in; Float is what an OverlayHost attaches.
func NewToasts(opts ...ToastsOption) *Toasts {
	t := &Toasts{corner: BottomRight, linger: 3 * time.Second, max: 3, width: 48, now: time.Now,
		st:    style.New().Background(style.TokenPanel).Foreground(style.TokenForeground),
		muted: style.New().Background(style.TokenPanel).Foreground(style.TokenTextMuted)}
	for _, o := range opts {
		if o != nil {
			o(t)
		}
	}
	t.float = NewFloat(t, WithAnchor(t.corner))
	return t
}

// Float is the layer the toasts show in, for OverlayHost.AttachTopmost.
func (t *Toasts) Float() *Float { return t.float }

// SetCorner moves the stack to another corner.
func (t *Toasts) SetCorner(a Anchor) {
	t.corner = a
	t.float.SetAnchor(a)
	t.RequestLayout()
}

// SetMargin changes the rows kept clear at the corner's edge: a status line shown or hidden.
func (t *Toasts) SetMargin(rows int) {
	t.margin = max(rows, 0)
	t.RequestLayout()
}

// SetLinger changes how long a finished toast stays, from the next one finished on.
func (t *Toasts) SetLinger(d time.Duration) { t.linger = max(d, time.Second) }

// Post shows a toast, or queues it, or replaces the message of the one posted with its ID.
func (t *Toasts) Post(n Toast) {
	now := t.now()
	if n.At.IsZero() {
		n.At = now
	}
	if it := t.find(n.ID); it != nil {
		it.Text, it.Ongoing = n.Text, n.Ongoing
		if !n.Ongoing && !it.finishedT {
			t.finish(it, now)
		}
		t.changed()
		return
	}
	it := &toastItem{Toast: n}
	if !n.Ongoing {
		it.finishedT = true
	}
	if len(t.showing) < t.max {
		t.show(it, now)
	} else {
		t.queue = append(t.queue, it)
	}
	if !t.float.Shown() && t.float.ctx != nil {
		t.float.Show()
	}
	t.tick()
	t.changed()
}

// Done finishes the ongoing toast posted with id: it lingers, then goes.
func (t *Toasts) Done(id string) {
	if it := t.find(id); it != nil && !it.finishedT {
		it.Ongoing = false
		t.finish(it, t.now())
		t.changed()
	}
}

// Len is how many toasts are showing, and how many wait.
func (t *Toasts) Len() (showing, waiting int) { return len(t.showing), len(t.queue) }

func (t *Toasts) find(id string) *toastItem {
	if id == "" {
		return nil
	}
	for _, it := range slices.Concat(t.showing, t.queue) {
		if it.ID == id {
			return it
		}
	}
	return nil
}

func (t *Toasts) show(it *toastItem, now time.Time) {
	t.showing = append(t.showing, it)
	if it.finishedT {
		it.finished = now // its linger starts when it shows
	}
}

func (t *Toasts) finish(it *toastItem, now time.Time) {
	it.finishedT = true
	if slices.Contains(t.showing, it) {
		it.finished = now
	}
}

// expire takes out the finished toasts that have lingered, and shows the waiting ones in their
// place.
func (t *Toasts) expire() {
	now := t.now()
	kept := t.showing[:0]
	for _, it := range t.showing {
		if it.finishedT && now.Sub(it.finished) >= t.linger {
			continue
		}
		kept = append(kept, it)
	}
	gone := len(kept) != len(t.showing)
	t.showing = kept
	for len(t.showing) < t.max && len(t.queue) > 0 {
		it := t.queue[0]
		t.queue = t.queue[1:]
		t.show(it, now)
		gone = true
	}
	if gone {
		t.RequestLayout()
	}
	if len(t.showing) == 0 && len(t.queue) == 0 && t.stop != nil {
		t.stop()
		t.stop = nil
	}
}

// tick runs the clock once a second while anything shows or waits: the ages move, and the
// lingering go.
func (t *Toasts) tick() {
	if t.stop != nil || t.ctx == nil {
		return
	}
	t.stop = t.ctx.Every(time.Second)
}

func (t *Toasts) changed() {
	t.RequestLayout()
	t.MarkDirty()
}

// Init starts the clock for what was posted before the stack was mounted.
func (t *Toasts) Init(ctx *tui.Context) {
	t.Base.Init(ctx)
	t.stop = nil
	if len(t.showing)+len(t.queue) > 0 {
		t.tick()
	}
}

// HandleEvent is the clock: each tick ages the toasts and lets the lingering go.
func (t *Toasts) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.TickEvent); ok {
		t.expire()
		t.MarkDirty()
		return true
	}
	return false
}

// age is how long ago a toast came, as it says it.
func age(d time.Duration) string {
	switch {
	case d < 5*time.Second:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh ago", int(d.Hours()))
}

// box is one toast laid out: its width and its lines of text.
func (t *Toasts) box(it *toastItem, maxW int) (w int, lines []string) {
	inner := max(min(t.width, maxW)-4, 1) // border and a column of padding each side
	for _, para := range strings.Split(it.Text, "\n") {
		lines = append(lines, wrapLine(para, inner, t.measure)...)
	}
	wide := t.measure(age(t.now().Sub(it.At))) + 4 // the age in the top border
	for _, l := range lines {
		wide = max(wide, t.measure(l))
	}
	return min(wide, inner) + 4, lines
}

// Layout is the stack's size: the widest toast, and every toast's height with the margin.
func (t *Toasts) Layout(c tui.Constraints) tui.Size {
	if len(t.showing) == 0 {
		return c.Constrain(tui.Size{})
	}
	maxW := boundedMax(c.MaxW, t.width)
	w, h := 0, t.margin
	for _, it := range t.showing {
		bw, lines := t.box(it, maxW)
		w, h = max(w, bw), h+len(lines)+2
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

// Render paints the toasts, the newest nearest the corner, each at the corner's side of the stack;
// the margin rows are left unpainted.
func (t *Toasts) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || len(t.showing) == 0 {
		return
	}
	bottom := t.corner == BottomLeft || t.corner == BottomRight || t.corner == Bottom
	right := t.corner == TopRight || t.corner == BottomRight || t.corner == Right
	type placed struct {
		it    *toastItem
		w     int
		lines []string
	}
	var boxes []placed
	for _, it := range t.showing {
		w, lines := t.box(it, sz.W)
		boxes = append(boxes, placed{it, w, lines})
	}
	if !bottom {
		slices.Reverse(boxes) // at the top, the newest is first
	}
	y := 0
	if !bottom {
		y = t.margin
	}
	for _, b := range boxes {
		x := 0
		if right {
			x = sz.W - b.w
		}
		t.paint(s, x, y, b.w, b.lines, age(t.now().Sub(b.it.At)))
		y += len(b.lines) + 2
	}
}

// paint draws one toast's box at (x, y).
func (t *Toasts) paint(s tui.Surface, x, y, w int, lines []string, when string) {
	h := len(lines) + 2
	s.Fill(tui.Rect{X: x, Y: y, W: w, H: h}, " ", t.st)
	top := "╭" + strings.Repeat("─", max(w-2, 0)) + "╮"
	drawText(s, x, y, top, t.muted)
	label := " " + when + " "
	if lw := t.measure(label); lw+3 <= w {
		drawText(s, x+w-2-lw, y, label, t.muted)
	}
	for i, l := range lines {
		drawText(s, x, y+1+i, "│", t.muted)
		drawText(s, x+2, y+1+i, l, t.st)
		drawText(s, x+w-1, y+1+i, "│", t.muted)
	}
	drawText(s, x, y+h-1, "╰"+strings.Repeat("─", max(w-2, 0))+"╯", t.muted)
}
