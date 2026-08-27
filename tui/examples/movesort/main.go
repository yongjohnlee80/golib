// Command movesort demonstrates identity-preserving reorder: each row is
// a live component with a per-mount counter and a local tick clock
// (Context.Every). Pressing s or ↑/↓ reorders via Flex.Move — mounts
// stay at 1, the focused row keeps focus, the tick cadence never
// stutters. Pressing R reorders via Remove+Add: mounts increment (fresh
// Init), focus is yanked by repair, and the timers are cancelled and
// re-armed — the amputation Move exists to avoid. Struct fields (tick
// counts) survive BOTH paths: only node-scoped state dies on remount.
package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/term"
)

// row is a keyed, focusable child whose visible state — tick count,
// mount count — dies on remount and survives Move.
type row struct {
	label  string
	mounts int
	ticks  int
	ctx    *tui.Context
}

func (r *row) Key() any           { return r.label } // Keyer: sort preserves identity
func (r *row) AcceptsFocus() bool { return true }    // Focusable: Tab between rows

func (r *row) Init(ctx *tui.Context) {
	r.ctx = ctx
	r.mounts++       // re-entrant across remounts: this is how you SEE a remount
	ctx.Every(clock) // tick event addressed to this node; unmount cancels it
}

func (r *row) Layout(c tui.Constraints) tui.Size {
	return tui.Size{W: c.MaxW, H: 1}
}

func (r *row) Render(s tui.Surface) {
	st := style.New()
	if r.ctx.Focused() {
		st = st.Reverse(true)
	}
	sz := s.Size()
	s.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: 1}, " ", st)
	text := fmt.Sprintf(" %-8s mounts:%d  tick:%d", r.label, r.mounts, r.ticks)
	for i, ch := range []rune(text) {
		if i >= sz.W {
			break
		}
		s.SetCell(i, 0, string(ch), st)
	}
}

func (r *row) HandleEvent(ev tui.Event) bool {
	if _, ok := ev.(tui.TickEvent); ok {
		r.ticks++
		r.ctx.MarkDirty()
		return true
	}
	return false
}

const clock = 400 * time.Millisecond

// app is the root controller: owns the flex of rows and the s/R keys.
type app struct {
	flex *tui.Flex
	rows []*row
	quit func()
	ctx  *tui.Context
}

func newApp(quit func()) *app {
	a := &app{flex: tui.NewFlex(tui.Vertical), quit: quit}
	// Deliberately unsorted (Z-A) so the first render is the "before".
	for _, label := range []string{"zebra", "mango", "kiwi", "grape", "apple"} {
		r := &row{label: label}
		a.rows = append(a.rows, r)
		a.flex.Add(r)
	}
	a.flex.Add(&hint{text: "↑/↓: move focused row  ·  s: sort (Move)  ·  R: sort (Remove+Add)  ·  Tab: focus  ·  q: quit"})
	return a
}

// label is a static, unfocusable text line.
type hint struct{ text string }

func (h *hint) Init(*tui.Context)                 {}
func (h *hint) Layout(c tui.Constraints) tui.Size { return tui.Size{W: c.MaxW, H: 1} }
func (h *hint) HandleEvent(tui.Event) bool        { return false }
func (h *hint) Render(s tui.Surface) {
	for i, ch := range []rune(h.text) {
		if i >= s.Size().W {
			break
		}
		s.SetCell(i, 0, string(ch), style.New().Bold(true))
	}
}

func (a *app) Init(ctx *tui.Context) {
	a.ctx = ctx
	ctx.Mount(a.flex)
}

func (a *app) Layout(c tui.Constraints) tui.Size {
	sz := a.ctx.LayoutChild(a.flex, c)
	a.ctx.PlaceChild(a.flex, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
	return sz
}

func (a *app) Render(tui.Surface) {}

func (a *app) HandleEvent(ev tui.Event) bool {
	e, ok := ev.(tui.KeyEvent)
	if !ok || e.Kind != tui.KeyPress {
		return false
	}
	switch e.Code {
	case 'q':
		a.quit()
	case tui.KeyUp, tui.KeyDown:
		// Move the focused row one step; identity is preserved, so the
		// row you were looking at IS the row that travels.
		i := a.focusedRow()
		if i < 0 {
			return false
		}
		to := i + 1
		if e.Code == tui.KeyUp {
			to = i - 1
		}
		if to < 0 || to >= len(a.rows) {
			return true
		}
		r := a.rows[i]
		a.rows = slices.Insert(slices.Delete(a.rows, i, i+1), to, r)
		a.flex.Move(r, to)
		return true
	case 's':
		// Sort A→Z by Move: same mounts, same NodeIDs, ticking continues.
		sorted := slices.Clone(a.rows)
		slices.SortFunc(sorted, func(x, y *row) int { return cmpStr(x.label, y.label) })
		for to, r := range sorted {
			a.flex.Move(r, to)
		}
	case 'R':
		// The naive reorder: Remove+Add. Every row mounts afresh —
		// mounts increments, focus is repaired away from whichever row
		// held it, timers are cancelled and restart their cadence.
		// (ticks survives: it lives on the struct, not the node.)
		sorted := slices.Clone(a.rows)
		slices.SortFunc(sorted, func(x, y *row) int { return cmpStr(x.label, y.label) })
		for _, r := range a.rows {
			a.flex.Remove(r)
		}
		a.rows = sorted
		a.flex.Add(a.rows[0], a.rows[1], a.rows[2], a.rows[3], a.rows[4])
	}
	return false
}

// focusedRow returns the index of the row currently holding focus, or -1.
func (a *app) focusedRow() int {
	for i, r := range a.rows {
		if r.ctx != nil && r.ctx.Focused() {
			return i
		}
	}
	return -1
}

func cmpStr(x, y string) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

func main() {
	backend, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "movesort: cannot open the terminal:", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tui.NewApp(newApp(cancel), tui.WithBackend(backend)).Run(ctx)
}
