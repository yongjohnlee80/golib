package widget

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TerminalTabID is an opaque runtime identity, unrelated to a label or QML id.
// IDs are never reused, including after removal and across container rebuilds.
type TerminalTabID string

var terminalTabSequence atomic.Uint64

var (
	ErrTerminalTabUnknown    = errs.Sentinel(errs.ErrPrecondition, "unknown or removed terminal tab")
	ErrTerminalTabClosing    = errs.Sentinel(errs.ErrPrecondition, "terminal tab is closing")
	ErrTerminalTabsUnmounted = errs.Sentinel(errs.ErrPrecondition, "terminal tabs must be mounted to start a process")
)

// TerminalTab describes an instance without exposing its process or widget.
type TerminalTab struct {
	ID               TerminalTabID
	Label            string
	Generation       int
	Running, Closing bool
	CleanupError     error
	StartError       error
}

type terminalTab struct {
	id       TerminalTabID
	label    string
	terminal *Terminal
	closing  *TerminalTabRemovalResult
	err      error
}

// TerminalTabRemovalResult observes cleanup of every generation of the removed
// instance, including older closing or failed-cleanup generations after restart.
// Generation identifies the last attempt; Done/Wait cover the whole instance.
// Cancellation affects only the waiter, never the cleanup owner.
type TerminalTabRemovalResult struct {
	last  *TerminalStopResult
	owner *TerminalShutdownResult
}

func (r *TerminalTabRemovalResult) Generation() int                { return r.last.Generation() }
func (r *TerminalTabRemovalResult) Started() bool                  { return r.last.Started() }
func (r *TerminalTabRemovalResult) StartError() error              { return r.last.StartError() }
func (r *TerminalTabRemovalResult) Done() <-chan struct{}          { return r.owner.Done() }
func (r *TerminalTabRemovalResult) Wait(ctx context.Context) error { return r.owner.Wait(ctx) }

// TerminalTabs owns runtime terminal instances, independently of generic Tabs.
// Add creates only; Start is explicit. Select never starts a process. Every
// child stays mounted while the container is mounted; hiding/remounting preserves
// processes and scrollback. Removal waits for confirmed Close and reap. Failed
// cleanup retains the tab until an explicit ForceRemove acknowledges it.
//
// Mutations and inspection belong to the UI loop. ShutdownWithResult is the
// exception: it is concurrent-safe and remains observable after destruction.
type TerminalTabs struct {
	Base
	entries            []*terminalTab
	active             TerminalTabID
	bar, tab, selected style.Style
	look               style.Style
	empty              string // the body with no tab; "" is the default message
	// the look every tab takes (SetMinimumContrast, SetCursorColor): set, it overrides a tab's own
	minContrast   float64
	contrastSet   bool
	cursorColor   style.Color
	cursorColored bool
	onAdd         func()
	onMenu        func(TerminalTabID)
	onChanged     func()
	mu            sync.Mutex // lifetime registry and shutdown only; never loop-owned entries
	owned         []*Terminal
	shutdown      *TerminalShutdownResult
	watching      bool
}

// NewTerminalTabs creates a valid empty terminal container.
func NewTerminalTabs() *TerminalTabs {
	t := &TerminalTabs{}
	t.bar, t.tab, t.selected = defaultTabsStyles()
	return t
}

// SetActions configures the separate '+' and right-click strip menu targets.
// The host decides what to add and supplies its own contextual menu.
func (t *TerminalTabs) SetActions(add func(), menu func(TerminalTabID)) {
	t.onAdd, t.onMenu = add, menu
}

// SetOnChanged observes selection, membership and cleanup outcomes on the UI loop.
func (t *TerminalTabs) SetOnChanged(fn func()) { t.onChanged = fn }

func (t *TerminalTabs) changed() {
	if t.shuttingDown() {
		return
	}
	t.RequestLayout()
	t.MarkDirty()
	if t.onChanged != nil {
		t.onChanged()
	}
}

func (t *TerminalTabs) shuttingDown() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.shutdown != nil
}

// Add snapshots terminal options and returns a fresh identity. It does not start
// or select the process (the first added tab becomes the selected empty page).
func (t *TerminalTabs) Add(label string, options ...TerminalOption) (TerminalTabID, error) {
	t.mu.Lock()
	if t.shutdown != nil {
		t.mu.Unlock()
		return "", ErrTerminalShutdown
	}
	terminal := NewTerminal(options...)
	terminal.cfg.args = append([]string(nil), terminal.cfg.args...)
	terminal.cfg.env = append([]string(nil), terminal.cfg.env...)
	terminal.SetDefaultLook(t.look)
	t.lookTab(terminal)
	onExit, onTitle := terminal.cfg.onExit, terminal.cfg.onTitle
	terminal.cfg.onExit = func(code int) {
		if t.shuttingDown() {
			return
		}
		if onExit != nil {
			onExit(code)
		}
		if !t.shuttingDown() {
			t.changed()
		}
	}
	terminal.cfg.onTitle = func(title string) {
		if t.shuttingDown() {
			return
		}
		if onTitle != nil {
			onTitle(title)
		}
		if !t.shuttingDown() {
			t.changed()
		}
	}
	t.owned = append(t.owned, terminal)
	t.mu.Unlock()
	id := TerminalTabID(fmt.Sprintf("terminal-%d", terminalTabSequence.Add(1)))
	e := &terminalTab{id: id, label: label, terminal: terminal}
	t.entries = append(t.entries, e)
	if t.active == "" {
		t.active = id
	}
	terminal.SetVisible(t.active == id)
	if t.ctx != nil && t.ctx.Mounted() {
		t.ctx.Mount(terminal)
	}
	t.changed()
	return id, nil
}

// SetLabel renames tab id: the strip shows label (an agent's codename, a shell's host).
func (t *TerminalTabs) SetLabel(id TerminalTabID, label string) error {
	e, err := t.find(id)
	if err != nil {
		return err
	}
	if e.label != label {
		e.label = label
		t.changed()
	}
	return nil
}

// SetEmptyText is what the body says with no tab ("" is "No terminals. Use + to add one."): an
// agents panel's "No agent running", say.
func (t *TerminalTabs) SetEmptyText(s string) {
	if t.empty != s {
		t.empty = s
		t.MarkDirty()
	}
}

// SetMinimumContrast gives every tab, those added later too, Terminal.SetMinimumContrast's ratio.
func (t *TerminalTabs) SetMinimumContrast(ratio float64) {
	t.minContrast, t.contrastSet = ratio, true
	t.lookTabs()
}

// SetCursorColor gives every tab, those added later too, Terminal.SetCursorColor's colour.
func (t *TerminalTabs) SetCursorColor(c style.Color) {
	t.cursorColor, t.cursorColored = c, true
	t.lookTabs()
}

func (t *TerminalTabs) lookTabs() {
	for _, e := range t.entries {
		t.lookTab(e.terminal)
	}
}

// lookTab gives terminal the container's look, where one is set.
func (t *TerminalTabs) lookTab(terminal *Terminal) {
	if t.contrastSet {
		terminal.SetMinimumContrast(t.minContrast)
	}
	if t.cursorColored {
		terminal.SetCursorColor(t.cursorColor)
	}
}

func (t *TerminalTabs) find(id TerminalTabID) (*terminalTab, error) {
	for _, e := range t.entries {
		if e.id == id {
			return e, nil
		}
	}
	return nil, ErrTerminalTabUnknown
}

// Tabs returns a detached snapshot; duplicate display labels are allowed.
func (t *TerminalTabs) Tabs() []TerminalTab {
	result := make([]TerminalTab, 0, len(t.entries))
	for _, e := range t.entries {
		r := e.terminal.CleanupResult()
		result = append(result, TerminalTab{ID: e.id, Label: e.label, Generation: r.Generation(),
			Running: e.terminal.Running(), Closing: e.closing != nil, CleanupError: e.err, StartError: r.StartError()})
	}
	return result
}

func (t *TerminalTabs) Active() TerminalTabID { return t.active }

// ActiveTabIndex and TabSpans describe only strip chrome for native renderers.
// Runtime operations continue to use opaque IDs, never presentation indexes.
func (t *TerminalTabs) ActiveTabIndex() int {
	for i, e := range t.entries {
		if e.id == t.active {
			return i
		}
	}
	return -1
}
func (t *TerminalTabs) TabSpans() []TabSpan {
	spans := make([]TabSpan, 0, len(t.entries)+1)
	x := 0
	for _, e := range t.entries {
		w := t.measure(terminalTabLabel(e))
		spans = append(spans, TabSpan{X: x, W: w})
		x += w + 1
	}
	if t.onAdd != nil {
		spans = append(spans, TabSpan{X: x, W: 3})
		x += 4
	}
	if t.onMenu != nil && t.active != "" {
		spans = append(spans, TabSpan{X: x, W: 3})
	}
	return spans
}

func (*TerminalTabs) AccessibleRole() tui.AccessibleRole { return tui.RolePageTabList }

// Select focuses only the selected child within this owned container.
func (t *TerminalTabs) Select(id TerminalTabID) error {
	if t.shuttingDown() {
		return ErrTerminalShutdown
	}
	e, err := t.find(id)
	if err != nil {
		return err
	}
	t.active = id
	for _, page := range t.entries {
		page.terminal.SetVisible(page == e && page.closing == nil)
	}
	if t.ctx != nil && t.ctx.Mounted() {
		if e.closing == nil {
			t.ctx.FocusComponent(e.terminal)
		} else {
			t.ctx.RequestFocus()
		}
	}
	t.changed()
	return nil
}

// Start starts an existing configured tab. Mount is required so startup errors
// are returned immediately rather than hidden in a pending pre-mount request.
func (t *TerminalTabs) Start(id TerminalTabID) error {
	if t.shuttingDown() {
		return ErrTerminalShutdown
	}
	e, err := t.find(id)
	if err != nil {
		return err
	}
	if e.closing != nil {
		return ErrTerminalTabClosing
	}
	if t.ctx == nil || !t.ctx.Mounted() {
		return ErrTerminalTabsUnmounted
	}
	err = e.terminal.Start()
	t.changed()
	return err
}

// SendText queues literal text for this exact runtime identity. Closing,
// removed and nonrunning targets reject the entire send without side effects.
func (t *TerminalTabs) SendText(id TerminalTabID, text string) error {
	if t.shuttingDown() {
		return ErrTerminalShutdown
	}
	e, err := t.find(id)
	if err != nil {
		return err
	}
	if e.closing != nil {
		return ErrTerminalTabClosing
	}
	return e.terminal.SendText(text)
}

// Remove requests cleanup and returns its generation handle immediately.
// Repeated calls re-await the same cleanup, never retry PTY.Close. A successful
// result removes the tab on the UI loop, even if its drawer is hidden. Cleanup
// errors keep the closing tab visible; cancellation of Wait affects only that
// waiter. Retain the handle to observe completion after the UI loop ends.
func (t *TerminalTabs) Remove(id TerminalTabID) (*TerminalTabRemovalResult, error) {
	if t.shuttingDown() {
		return nil, ErrTerminalShutdown
	}
	e, err := t.find(id)
	if err != nil {
		return nil, err
	}
	if e.closing != nil {
		return e.closing, nil
	}
	last := e.terminal.StopWithResult()
	e.closing = &TerminalTabRemovalResult{last: last, owner: e.terminal.ShutdownWithResult()}
	e.terminal.SetVisible(false)
	if t.ctx != nil && t.ctx.Mounted() && t.ctx.FocusWithin(e.terminal) {
		t.ctx.RequestFocus()
	}
	r := e.closing
	finish := func(err error) {
		if t.shuttingDown() {
			return
		}
		current, found := t.find(id)
		if found != nil || current != e || current.closing != r {
			return
		}
		if err != nil {
			e.err = err
			t.changed()
			return
		}
		t.detach(e)
	}
	select {
	case <-r.Done():
		finish(r.Wait(context.Background()))
	default:
		app := t.ctx.App() // a real started process always has an App
		go func() {
			err := r.Wait(context.Background())
			app.Update(func() { finish(err) })
		}()
	}
	t.changed()
	return r, nil
}

// ForceRemove explicitly abandons waiting for a closing tab (or acknowledges
// its cleanup error). Its process remains owned by ShutdownWithResult; hiding a
// failed cleanup does not discard the lifetime result or reuse its identity.
func (t *TerminalTabs) ForceRemove(id TerminalTabID) error {
	if t.shuttingDown() {
		return ErrTerminalShutdown
	}
	e, err := t.find(id)
	if err != nil {
		return err
	}
	if e.closing == nil {
		return ErrTerminalTabClosing
	}
	t.detach(e)
	return nil
}

func (t *TerminalTabs) detach(e *terminalTab) {
	hadFocus := t.ctx != nil && t.ctx.Mounted() && t.ctx.FocusWithin(e.terminal)
	if t.ctx != nil && t.ctx.Mounted() {
		t.ctx.Unmount(e.terminal)
	}
	for i, page := range t.entries {
		if page == e {
			t.entries = append(t.entries[:i], t.entries[i+1:]...)
			break
		}
	}
	if t.active == e.id {
		t.active = ""
		if len(t.entries) > 0 {
			t.active = t.entries[0].id
			t.entries[0].terminal.SetVisible(t.entries[0].closing == nil)
		}
	}
	if hadFocus {
		if active, err := t.find(t.active); err == nil && active.closing == nil {
			t.ctx.FocusComponent(active.terminal)
		} else {
			t.ctx.RequestFocus()
		}
	}
	if e.closing != nil {
		select {
		case <-e.closing.Done():
			if e.closing.Wait(context.Background()) == nil {
				t.mu.Lock()
				for i, terminal := range t.owned {
					if terminal == e.terminal {
						t.owned = append(t.owned[:i], t.owned[i+1:]...)
						break
					}
				}
				t.mu.Unlock()
			}
		default:
		}
	}
	t.changed()
}

// ShutdownWithResult requests every owned terminal's destruction before waiting
// for any of them. It is idempotent and safe after App.Done or QML destruction;
// no completion callback targets a dead loop, and cancellation is not success.
func (t *TerminalTabs) ShutdownWithResult() *TerminalShutdownResult {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.shutdown != nil {
		return t.shutdown
	}
	r := &TerminalShutdownResult{completion: terminalCompletion{done: make(chan struct{})}}
	t.shutdown = r
	results := make([]*TerminalShutdownResult, 0, len(t.owned))
	for _, terminal := range t.owned {
		results = append(results, terminal.ShutdownWithResult())
	}
	go func() {
		var failures []error
		for _, result := range results {
			if err := result.Wait(context.Background()); err != nil {
				failures = append(failures, err)
			}
		}
		r.completion.err = errors.Join(failures...)
		close(r.completion.done)
	}()
	return r
}

func (t *TerminalTabs) Init(ctx *tui.Context) {
	t.Base.Init(ctx)
	for _, e := range t.entries {
		e.terminal.SetVisible(e.id == t.active && e.closing == nil)
		ctx.Mount(e.terminal)
	}
	if !t.watching {
		t.watching = true
		app := ctx.App()
		go func() { <-app.Done(); t.ShutdownWithResult() }()
	}
}

func (*TerminalTabs) AcceptsFocus() bool { return true }
func (t *TerminalTabs) listChildren() []tui.Component {
	if e, err := t.find(t.active); err == nil && e.closing == nil {
		return []tui.Component{e.terminal}
	}
	return nil
}

// SetStyles and SetDefaultLook preserve palette changes across existing and new tabs.
func (t *TerminalTabs) SetStyles(bar, tab, selected style.Style) {
	t.bar, t.tab, t.selected = defaultTabsStyles()
	t.bar, t.tab, t.selected = bar.Inherit(t.bar), tab.Inherit(t.tab), selected.Inherit(t.selected)
	t.MarkDirty()
}
func (t *TerminalTabs) SetDefaultLook(look style.Style) {
	t.look = look
	for _, e := range t.entries {
		e.terminal.SetDefaultLook(look)
	}
}

func (t *TerminalTabs) Layout(c tui.Constraints) tui.Size {
	w, h := boundedMax(c.MaxW, max(c.MinW, 1)), boundedMax(c.MaxH, max(c.MinH, 1))
	if e, err := t.find(t.active); err == nil && e.closing == nil && h > 1 {
		t.ctx.LayoutChild(e.terminal, tui.Tight(tui.Size{W: w, H: h - 1}))
		t.ctx.PlaceChild(e.terminal, tui.Rect{Y: 1, W: w, H: h - 1})
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

func terminalTabLabel(e *terminalTab) string {
	label := e.label
	if e.err != nil {
		label += " !"
	} else if e.closing != nil {
		label += " …"
	}
	return cellLabel(label)
}

func (t *TerminalTabs) Render(s tui.Surface) {
	w := s.Size().W
	if w <= 0 || s.Size().H == 0 {
		return
	}
	s.Fill(tui.Rect{W: w, H: 1}, " ", t.bar)
	if s.Size().H > 1 {
		message := ""
		if e, err := t.find(t.active); err != nil {
			message = cmp.Or(t.empty, "No terminals. Use + to add one.")
		} else if e.err != nil {
			message = "Cleanup failed: " + e.err.Error()
		} else if e.closing != nil {
			message = "Closing terminal…"
		}
		if message != "" {
			drawText(s, 0, 1, truncate(message, w, s.StringWidth), t.tab)
		}
	}
	x := 0
	for _, e := range t.entries {
		label, st := terminalTabLabel(e), t.tab
		if e.id == t.active {
			st = t.selected
		}
		if x >= w {
			break
		}
		drawText(s, x, 0, truncate(label, w-x, s.StringWidth), st)
		x += s.StringWidth(label) + 1
	}
	if t.onAdd != nil && x < w {
		drawText(s, x, 0, truncate(" + ", w-x, s.StringWidth), t.tab)
	}
	if t.onAdd != nil {
		x += 4
	}
	if t.onMenu != nil && t.active != "" && x < w {
		drawText(s, x, 0, truncate(" ≡ ", w-x, s.StringWidth), t.tab)
	}
}

func (t *TerminalTabs) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		if e.Kind == tui.KeyRelease {
			return false
		}
		if t.focused() && e.Mods.Chord() == 0 {
			if e.Text == "+" && t.onAdd != nil {
				t.onAdd()
				return true
			}
			if e.Code == tui.KeyEnter && t.onMenu != nil && t.active != "" {
				t.onMenu(t.active)
				return true
			}
		}
		if len(t.entries) == 0 {
			return false
		}
		delta := 0
		if e.Mods.Chord() == tui.ModCtrl {
			if e.Code == tui.KeyPageUp {
				delta = -1
			}
			if e.Code == tui.KeyPageDown {
				delta = 1
			}
		} else if t.focused() && e.Mods.Chord() == 0 {
			if e.Code == tui.KeyLeft || e.Code == '[' {
				delta = -1
			}
			if e.Code == tui.KeyRight || e.Code == ']' {
				delta = 1
			}
		}
		if delta == 0 {
			return false
		}
		for i, page := range t.entries {
			if page.id == t.active {
				_ = t.Select(t.entries[(i+delta+len(t.entries))%len(t.entries)].id)
				return true
			}
		}
	case tui.MouseEvent:
		if e.Kind != tui.MousePress || e.Y != 0 {
			return false
		}
		x := 0
		for _, page := range t.entries {
			w := t.measure(terminalTabLabel(page))
			if e.X >= x && e.X < x+w {
				if e.Button == tui.MouseLeft {
					_ = t.Select(page.id)
				} else if e.Button == tui.MouseRight && t.onMenu != nil {
					t.onMenu(page.id)
				}
				return true
			}
			x += w + 1
		}
		if e.X >= x && e.X < x+3 && e.Button == tui.MouseLeft && t.onAdd != nil {
			t.onAdd()
			return true
		}
		if t.onAdd != nil {
			x += 4
		}
		if e.X >= x && e.X < x+3 && e.Button == tui.MouseLeft && t.onMenu != nil && t.active != "" {
			t.onMenu(t.active)
			return true
		}
	}
	return false
}
