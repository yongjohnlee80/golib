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
	vimKeys       bool
	vimKeysSet    bool
	cellFace      string
	faceSet       bool
	hostKeys      func(tui.KeyEvent) bool
	hostKeysSet   bool
	onAdd         func()
	onMenu        func(TerminalTabID)
	menuRows      func(TerminalTabID) []MenuItemModel // SetMenu: the strip's own menu
	addRows       func() []MenuItemModel              // SetAddMenu: what + offers
	menuOpen      *popupLayer
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

// TerminalTabsMenuAction is a row of the strip's menus: when the row is chosen, Run runs with the
// tab the menu was opened for (SetMenu), or with no tab (SetAddMenu's +).
type TerminalTabsMenuAction struct {
	ID  tui.ActionID
	Run func(id TerminalTabID)
}

// ActionID implements tui.Action.
func (a TerminalTabsMenuAction) ActionID() tui.ActionID { return a.ID }

// SetMenu gives the strip a menu of its own, opened over the strip (it needs an OverlayHost above
// it) by ≡, by a right press on a tab, on the strip's blank or on a body no terminal fills, and by
// Enter on the focused strip: build returns its rows for the tab it opens for (the active one off
// a tab; "" with none), each time it opens, so they are that tab's as it stands. ≡ shows with no
// tab too. A row's action
// is a TerminalTabsMenuAction; a row with children is a submenu ("Go to ›" every tab, say). Unset,
// those open SetActions' menu callback instead; nil unsets it.
func (t *TerminalTabs) SetMenu(build func(id TerminalTabID) []MenuItemModel) { t.menuRows = build }

// SetAddMenu makes the strip's + open a menu, under it, instead of calling SetActions' add
// callback: build returns its rows each time it opens (the profiles to start, say). A row's action
// is a TerminalTabsMenuAction, run with no tab (the zero TerminalTabID); nil unsets it.
func (t *TerminalTabs) SetAddMenu(build func() []MenuItemModel) { t.addRows = build }

// hasMenu is whether the strip has a menu to open, its own or the host's.
func (t *TerminalTabs) hasMenu() bool { return t.menuRows != nil || t.onMenu != nil }

// menuShown is whether ≡ is on the strip: the strip's own menu (SetMenu) always, with no tab too
// (its rows for no tab: add one, hide the panel); the host's callback only for a tab.
func (t *TerminalTabs) menuShown() bool {
	return t.menuRows != nil || t.onMenu != nil && t.active != ""
}

// liveBody is whether a tab's terminal fills the body now: not with no tab, nor while the active
// one closes (the body says so).
func (t *TerminalTabs) liveBody() bool {
	e, err := t.find(t.active)
	return err == nil && e.closing == nil
}

// hasAdd is whether the strip shows +: SetAddMenu's menu, or the host's add callback.
func (t *TerminalTabs) hasAdd() bool { return t.addRows != nil || t.onAdd != nil }

// add is + chosen at at, in the strip's own frame: SetAddMenu's menu, else the host's callback.
func (t *TerminalTabs) add(at tui.Point) {
	if t.addRows != nil {
		t.popMenu("", t.addRows(), at)
	} else if t.onAdd != nil {
		t.onAdd()
	}
}

// openMenu opens the menu for tab id at at, in the strip's own frame: SetMenu's, else the host's
// callback.
func (t *TerminalTabs) openMenu(id TerminalTabID, at tui.Point) {
	if t.menuRows == nil {
		if t.onMenu != nil {
			t.onMenu(id)
		}
		return
	}
	t.popMenu(id, t.menuRows(id), at)
}

// popMenu shows items over the strip at at, a chosen row's action run with id.
func (t *TerminalTabs) popMenu(id TerminalTabID, items []MenuItemModel, at tui.Point) {
	host, ok := hostFor[popupHost](t.ctx)
	if !ok || len(items) == 0 {
		return
	}
	t.closeMenu()
	var layer *popupLayer
	menu := NewMenu(WithMenuVimNavigation(true), WithActionExecutor(func(inv tui.ActionInvocation) bool {
		act, ok := inv.Action.(TerminalTabsMenuAction)
		if !ok || act.Run == nil {
			return false
		}
		if layer != nil {
			layer.requestClose(DismissAccept)
		}
		act.Run(id)
		return true
	}))
	if err := menu.SetModel(items); err != nil {
		return
	}
	box := NewBox(menu, WithStyle(style.New().Background(style.TokenPanel)))
	l, err := host.openPopupAt(LayerID(fmt.Sprintf("terminaltabs.menu.%d", t.ctx.ID())), t, box, at, func(DismissReason) {
		if t.menuOpen == layer {
			t.menuOpen = nil
		}
	})
	if err != nil {
		return
	}
	layer, t.menuOpen = l, l
}

// closeMenu closes the strip's menu if it is open.
func (t *TerminalTabs) closeMenu() {
	if l := t.menuOpen; l != nil {
		t.menuOpen = nil
		l.close(DismissProgrammatic)
	}
}

// MenuOpen reports whether the strip's own menu (SetMenu, SetAddMenu) is showing.
func (t *TerminalTabs) MenuOpen() bool { return t.menuOpen != nil }

// addX is where the strip's + starts: after the last tab.
func (t *TerminalTabs) addX() int {
	x := 0
	for _, e := range t.entries {
		x += t.measure(terminalTabLabel(e)) + 1
	}
	return x
}

// tabX is where tab id's label starts on the strip.
func (t *TerminalTabs) tabX(id TerminalTabID) int {
	x := 0
	for _, e := range t.entries {
		if e.id == id {
			return x
		}
		x += t.measure(terminalTabLabel(e)) + 1
	}
	return 0
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

// SetVimKeys gives every tab, those added later too, Terminal.SetVimKeys: an editor's keymap
// changing reaches the shells already running.
func (t *TerminalTabs) SetVimKeys(on bool) {
	t.vimKeys, t.vimKeysSet = on, true
	t.lookTabs()
}

// SetHostKeys gives every tab, those added later too, Terminal.SetHostKeys: the keys their programs
// never see, left to the host.
func (t *TerminalTabs) SetHostKeys(match func(tui.KeyEvent) bool) {
	t.hostKeys, t.hostKeysSet = match, true
	t.lookTabs()
}

// SetCellFace gives every tab, those added later too, Terminal.SetCellFace: their cells in family,
// the strip's and its menus' in the window's.
func (t *TerminalTabs) SetCellFace(family string) {
	t.cellFace, t.faceSet = family, true
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
	if t.vimKeysSet {
		terminal.SetVimKeys(t.vimKeys)
	}
	if t.faceSet {
		terminal.SetCellFace(t.cellFace)
	}
	if t.hostKeysSet {
		terminal.SetHostKeys(t.hostKeys)
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
	if t.hasAdd() {
		spans = append(spans, TabSpan{X: x, W: 3})
		x += 4
	}
	if t.menuShown() {
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

// SendPaste is Terminal.SendPaste for this exact runtime identity, refused as SendText is.
func (t *TerminalTabs) SendPaste(id TerminalTabID, text string) error {
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
	return e.terminal.SendPaste(text)
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
	if t.hasAdd() && x < w {
		drawText(s, x, 0, truncate(" + ", w-x, s.StringWidth), t.tab)
	}
	if t.hasAdd() {
		x += 4
	}
	if t.menuShown() && x < w {
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
			if e.Text == "+" && t.hasAdd() {
				t.add(tui.Point{X: t.addX(), Y: 1})
				return true
			}
			if e.Code == tui.KeyEnter && t.menuShown() {
				t.openMenu(t.active, tui.Point{X: t.tabX(t.active), Y: 1})
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
		if e.Kind != tui.MousePress {
			return false
		}
		if e.Y != 0 {
			// a right press on the body with no terminal in it (no tab, or one closing) opens the
			// strip's own menu where it was, as a right press on the strip does; a live terminal's
			// body is the terminal's
			if e.Button == tui.MouseRight && t.menuRows != nil && !t.liveBody() {
				t.openMenu(t.active, tui.Point{X: e.X, Y: e.Y})
				return true
			}
			return false
		}
		x := 0
		for _, page := range t.entries {
			w := t.measure(terminalTabLabel(page))
			if e.X >= x && e.X < x+w {
				if e.Button == tui.MouseLeft {
					_ = t.Select(page.id)
				} else if e.Button == tui.MouseRight && t.hasMenu() {
					t.openMenu(page.id, tui.Point{X: e.X, Y: 1})
				}
				return true
			}
			x += w + 1
		}
		if e.X >= x && e.X < x+3 && e.Button == tui.MouseLeft && t.hasAdd() {
			t.add(tui.Point{X: x, Y: 1})
			return true
		}
		if t.hasAdd() {
			x += 4
		}
		if e.X >= x && e.X < x+3 && e.Button == tui.MouseLeft && t.menuShown() {
			t.openMenu(t.active, tui.Point{X: x, Y: 1})
			return true
		}
		// a right press anywhere else on the strip (its blank, +, ≡) opens the strip's own menu for
		// the active tab (or none), where it was
		if e.Button == tui.MouseRight && t.menuRows != nil {
			t.openMenu(t.active, tui.Point{X: e.X, Y: 1})
			return true
		}
	}
	return false
}
