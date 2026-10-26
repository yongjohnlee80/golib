package widget

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
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
	// ErrTerminalModeUnavailable is a mode the tab cannot take now: normal mode with no program.
	ErrTerminalModeUnavailable = errs.Sentinel(errs.ErrPrecondition, "terminal tab cannot take that mode: no program is running")
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
	entries []*terminalTab
	active  TerminalTabID
	strip   tabStrip // the row of tabs over the terminals (tab_strip.go)
	look    style.Style
	empty   string // the body with no tab; "" is the default message
	// defaults are what every tab takes, those added later too (SetMinimumContrast and the rest):
	// each one set overrides a tab's own; nil is not set
	defaults   [terminalDefaultCount]func(*Terminal)
	makeView   func(*Terminal) TerminalView      // SetTerminalViews: each tab's own view; nil for none
	onMode     func(TerminalTabID, TerminalMode) // SetOnMode: every tab's mode changes, by its identity
	normalHint string                            // SetNormalHint: at the strip's end while the active tab is in normal mode
	onChanged  func()
	mu         sync.Mutex // lifetime registry and shutdown only; never loop-owned entries
	owned      []*Terminal
	shutdown   *TerminalShutdownResult
	watching   bool
}

// NewTerminalTabs creates a valid empty terminal container.
func NewTerminalTabs() *TerminalTabs {
	t := &TerminalTabs{}
	t.strip = tabStrip{base: &t.Base, anchor: t, layer: "terminaltabs", owner: t}
	t.strip.bar, t.strip.tab, t.strip.selected = defaultTabsStyles()
	return t
}

func (t *TerminalTabs) stripTabs() []stripTab {
	out := make([]stripTab, len(t.entries))
	for i, e := range t.entries {
		out[i] = stripTab{id: string(e.id), label: terminalTabLabel(e)}
	}
	return out
}

func (t *TerminalTabs) stripActive() string   { return string(t.active) }
func (t *TerminalTabs) stripChoose(id string) { _ = t.Select(TerminalTabID(id)) }

// SetActions configures the separate '+' and right-click strip menu targets.
// The host decides what to add and supplies its own contextual menu.
func (t *TerminalTabs) SetActions(add func(), menu func(TerminalTabID)) {
	t.strip.onAdd, t.strip.onMenu = add, nil
	if menu != nil {
		t.strip.onMenu = func(id string) { menu(TerminalTabID(id)) }
	}
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

// liveTab is tab id while the tabs are not shutting down: ErrTerminalShutdown, or find's error.
func (t *TerminalTabs) liveTab(id TerminalTabID) (*terminalTab, error) {
	if t.shuttingDown() {
		return nil, ErrTerminalShutdown
	}
	return t.find(id)
}

// openTab is liveTab, refusing a tab that is closing (ErrTerminalTabClosing): what a runtime
// operation on the tab's process requires.
func (t *TerminalTabs) openTab(id TerminalTabID) (*terminalTab, error) {
	e, err := t.liveTab(id)
	if err == nil && e.closing != nil {
		return nil, ErrTerminalTabClosing
	}
	return e, err
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
	onMode := terminal.cfg.onMode
	terminal.cfg.onMode = func(m TerminalMode) {
		if onMode != nil {
			onMode(m)
		}
		if t.onMode != nil {
			t.onMode(id, m)
		}
		t.MarkDirty() // the strip's normal-mode hint follows the mode
	}
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

func (a TerminalTabsMenuAction) stripRun() func(string) {
	if a.Run == nil {
		return nil
	}
	return func(id string) { a.Run(TerminalTabID(id)) }
}

// SetMenu gives the strip a menu of its own, opened over the strip (it needs an OverlayHost above
// it) by ≡, by a right press on a tab, on the strip's blank or on a body no terminal fills, and by
// Enter on the focused strip: build returns its rows for the tab it opens for (the active one off
// a tab; "" with none), each time it opens, so they are that tab's as it stands. ≡ shows with no
// tab too. A row's action
// is a TerminalTabsMenuAction; a row with children is a submenu ("Go to ›" every tab, say). Unset,
// those open SetActions' menu callback instead; nil unsets it.
func (t *TerminalTabs) SetMenu(build func(id TerminalTabID) []MenuItemModel) {
	t.strip.menuRows = nil
	if build != nil {
		t.strip.menuRows = func(id string) []MenuItemModel { return build(TerminalTabID(id)) }
	}
}

// SetAddMenu makes the strip's + open a menu, under it, instead of calling SetActions' add
// callback: build returns its rows each time it opens (the profiles to start, say). A row's action
// is a TerminalTabsMenuAction, run with no tab (the zero TerminalTabID); nil unsets it.
func (t *TerminalTabs) SetAddMenu(build func() []MenuItemModel) { t.strip.addRows = build }

// liveBody is whether a tab's terminal fills the body now: not with no tab, nor while the active
// one closes (the body says so).
func (t *TerminalTabs) liveBody() bool {
	e, err := t.find(t.active)
	return err == nil && e.closing == nil
}

func (t *TerminalTabs) closeMenu() { t.strip.closeMenu() }

// MenuOpen reports whether the strip's own menu (SetMenu, SetAddMenu) is showing.
func (t *TerminalTabs) MenuOpen() bool { return t.strip.menuOpen != nil }

// addX is where the strip's + starts: after the last tab.
func (t *TerminalTabs) addX() int { return t.strip.addX() }

// tabX is where tab id's label starts on the strip.
func (t *TerminalTabs) tabX(id TerminalTabID) int { return t.strip.tabX(string(id)) }

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

// terminalDefault is a setting TerminalTabs gives every tab: its index in defaults, and the order
// they are applied in.
type terminalDefault int

const (
	defaultContrast terminalDefault = iota
	defaultCursorColor
	defaultVimKeys
	defaultModeKey
	defaultCellFace
	defaultHostKeys
	defaultModeless
	terminalDefaultCount
)

// setDefault records how every tab takes setting d, and gives it to the tabs there are.
func (t *TerminalTabs) setDefault(d terminalDefault, apply func(*Terminal)) {
	t.defaults[d] = apply
	t.lookTabs()
}

// SetMinimumContrast gives every tab, those added later too, Terminal.SetMinimumContrast's ratio.
func (t *TerminalTabs) SetMinimumContrast(ratio float64) {
	t.setDefault(defaultContrast, func(tm *Terminal) { tm.SetMinimumContrast(ratio) })
}

// SetCursorColor gives every tab, those added later too, Terminal.SetCursorColor's colour.
func (t *TerminalTabs) SetCursorColor(c style.Color) {
	t.setDefault(defaultCursorColor, func(tm *Terminal) { tm.SetCursorColor(c) })
}

// SetVimKeys gives every tab, those added later too, Terminal.SetVimKeys: an editor's keymap
// changing reaches the shells already running.
func (t *TerminalTabs) SetVimKeys(on bool) {
	t.setDefault(defaultVimKeys, func(tm *Terminal) { tm.SetVimKeys(on) })
}

// SetModeKey gives every tab, those added later too, Terminal.SetModeKey: the key that toggles
// their input and normal mode.
func (t *TerminalTabs) SetModeKey(match func(tui.KeyEvent) bool) {
	t.setDefault(defaultModeKey, func(tm *Terminal) { tm.SetModeKey(match) })
}

// SetModelessScroll gives every tab, those added later too, Terminal.SetModelessScroll: the wheel's
// scrollback view ends at the next key, for a user who never learnt normal mode.
func (t *TerminalTabs) SetModelessScroll(on bool) {
	t.setDefault(defaultModeless, func(tm *Terminal) { tm.SetModelessScroll(on) })
}

// SetOnMode calls fn on the UI loop with a tab's identity whenever its mode changes, for every tab,
// those added later too, after the tab's own WithOnMode; a host shows the mode, or how to leave it.
// nil removes it.
func (t *TerminalTabs) SetOnMode(fn func(TerminalTabID, TerminalMode)) { t.onMode = fn }

// SetNormalHint shows text at the strip's right end while the active tab is in normal mode, in the
// room the tabs leave: how to get back to typing, for a user who never learnt normal mode. "" (the
// default) shows nothing.
func (t *TerminalTabs) SetNormalHint(text string) {
	t.normalHint = text
	t.MarkDirty()
}

// SetHostKeys gives every tab, those added later too, Terminal.SetHostKeys: the keys their programs
// never see, left to the host.
func (t *TerminalTabs) SetHostKeys(match func(tui.KeyEvent) bool) {
	t.setDefault(defaultHostKeys, func(tm *Terminal) { tm.SetHostKeys(match) })
}

// SetCellFace gives every tab, those added later too, Terminal.SetCellFace: their cells in family,
// the strip's and its menus' in the window's.
func (t *TerminalTabs) SetCellFace(family string) {
	t.setDefault(defaultCellFace, func(tm *Terminal) { tm.SetCellFace(family) })
}

// SetTerminalViews gives each tab, those added later too, a view of its own, made for it by newView
// (Terminal.SetTerminalView): its text drawn at a cell size of its own. nil takes the views off,
// each told Detached. Views already set are replaced.
func (t *TerminalTabs) SetTerminalViews(newView func(*Terminal) TerminalView) {
	t.makeView = newView
	for _, e := range t.entries {
		e.terminal.SetTerminalView(nil)
		if newView != nil {
			e.terminal.SetTerminalView(newView(e.terminal))
		}
	}
}

func (t *TerminalTabs) lookTabs() {
	for _, e := range t.entries {
		t.lookTab(e.terminal)
	}
}

// lookTab gives terminal the container's defaults, where one is set, in their order.
func (t *TerminalTabs) lookTab(terminal *Terminal) {
	for _, apply := range t.defaults {
		if apply != nil {
			apply(terminal)
		}
	}
	if t.makeView != nil && terminal.TerminalView() == nil { // a tab's view is made once, here or by SetTerminalViews
		terminal.SetTerminalView(t.makeView(terminal))
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
func (t *TerminalTabs) ActiveTabIndex() int { return t.strip.activeIndex() }
func (t *TerminalTabs) TabSpans() []TabSpan { return t.strip.spans() }

func (*TerminalTabs) AccessibleRole() tui.AccessibleRole { return tui.RolePageTabList }

// Select focuses only the selected child within this owned container.
func (t *TerminalTabs) Select(id TerminalTabID) error {
	e, err := t.liveTab(id)
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
	e, err := t.openTab(id)
	if err != nil {
		return err
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
	e, err := t.openTab(id)
	if err != nil {
		return err
	}
	return e.terminal.SendText(text)
}

// SendPaste is Terminal.SendPaste for this exact runtime identity, refused as SendText is.
func (t *TerminalTabs) SendPaste(id TerminalTabID, text string) error {
	e, err := t.openTab(id)
	if err != nil {
		return err
	}
	return e.terminal.SendPaste(text)
}

// SetMode is Terminal.SetMode for this exact runtime identity: a closing or removed tab refuses it
// with its error, and a mode the Terminal cannot take (Normal with no program) with
// ErrTerminalModeUnavailable.
func (t *TerminalTabs) SetMode(id TerminalTabID, m TerminalMode) error {
	e, err := t.openTab(id)
	if err != nil {
		return err
	}
	if !e.terminal.SetMode(m) {
		return ErrTerminalModeUnavailable
	}
	return nil
}

// Signal is Terminal.Signal for this exact runtime identity: a closing or removed tab refuses it.
func (t *TerminalTabs) Signal(id TerminalTabID, sig os.Signal) error {
	e, err := t.openTab(id)
	if err != nil {
		return err
	}
	return e.terminal.Signal(sig)
}

// Remove requests cleanup and returns its generation handle immediately.
// Repeated calls re-await the same cleanup, never retry PTY.Close. A successful
// result removes the tab on the UI loop, even if its drawer is hidden. Cleanup
// errors keep the closing tab visible; cancellation of Wait affects only that
// waiter. Retain the handle to observe completion after the UI loop ends.
func (t *TerminalTabs) Remove(id TerminalTabID) (*TerminalTabRemovalResult, error) {
	e, err := t.liveTab(id)
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
	e, err := t.liveTab(id)
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
	e.terminal.SetTerminalView(nil) // its view told Detached: a closed tab's view posts nothing more
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
	results := make([]*TerminalShutdownResult, 0, len(t.owned))
	for _, terminal := range t.owned {
		results = append(results, terminal.ShutdownWithResult())
	}
	t.shutdown = joinShutdowns(results)
	return t.shutdown
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
	t.strip.bar, t.strip.tab, t.strip.selected = tabsStyles(bar, tab, selected)
	t.MarkDirty()
}
func (t *TerminalTabs) SetDefaultLook(look style.Style) {
	t.look = look
	for _, e := range t.entries {
		e.terminal.SetDefaultLook(look)
	}
}

func (t *TerminalTabs) Layout(c tui.Constraints) tui.Size {
	w, h := fillW(c), fillH(c)
	if t.ctx != nil { // narrower than its tabs, the strip scrolls them between < and >
		t.ctx.AfterLayout(tabWindowCommitKey, func() { t.strip.commitWindow(w) })
	}
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
	t.strip.render(s, w)
	t.renderNormalHint(s, w)
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
			drawText(s, 0, 1, truncate(message, w, s.StringWidth), t.strip.tab)
		}
	}
}

// KeyScope is the key-map scope of a TerminalTabs' strip.
func (t *TerminalTabs) KeyScope() keymap.Scope { return TabStripScope }

// KeyBindings is the strip's commands as m binds them (tui.KeyBinder).
func (t *TerminalTabs) KeyBindings(m *keymap.Map) []keymap.Binding {
	return tui.BindingsOf(m, tabStripCommands...)
}

func (t *TerminalTabs) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		return t.strip.key(e)
	case tui.MouseEvent:
		if e.Kind != tui.MousePress {
			return false
		}
		if e.Y != 0 {
			// a right press on the body with no terminal in it (no tab, or one closing) opens the
			// strip's own menu where it was, as a right press on the strip does; a live terminal's
			// body is the terminal's
			if e.Button == tui.MouseRight && t.strip.menuRows != nil && !t.liveBody() {
				t.strip.openMenu(string(t.active), tui.Point{X: e.X, Y: e.Y})
				return true
			}
			return false
		}
		return t.strip.press(e)
	}
	return false
}

// renderNormalHint draws SetNormalHint's text at the strip's right end, before its window controls,
// while the active tab is in normal mode, in the room the strip's own cells leave: cut when there is
// too little, none in fewer than four cells.
func (t *TerminalTabs) renderNormalHint(s tui.Surface, w int) {
	if t.normalHint == "" {
		return
	}
	e, err := t.find(t.active)
	if err != nil || e.terminal.Mode() != TerminalNormal {
		return
	}
	used, end := 0, w // the hint sits between the tabs' cells and the window controls, if any
	for _, c := range t.strip.row() {
		if c.kind == cellControl {
			end = min(end, c.x-1)
			continue
		}
		used = max(used, c.x+s.StringWidth(c.label))
	}
	room := end - used - 1
	if room < 4 {
		return
	}
	text := truncate(t.normalHint, room, s.StringWidth)
	drawText(s, end-s.StringWidth(text), 0, text, t.strip.tab)
}
