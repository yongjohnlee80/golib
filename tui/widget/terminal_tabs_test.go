package widget

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/pty"
)

func addTestTerminal(t *testing.T, tabs *TerminalTabs, label string, p *resultProc) TerminalTabID {
	t.Helper()
	id, err := tabs.Add(label, WithStarter(func(pty.Cmd) (TerminalProcess, error) { return p, nil }))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestTerminalTabsCreateSelectAndHideNeverStartOrReplaceInstances(t *testing.T) {
	tabs := NewTerminalTabs()
	p1, p2 := newResultProc(), newResultProc()
	defer close(p1.release)
	defer close(p2.release)
	a, b := addTestTerminal(t, tabs, "same", p1), addTestTerminal(t, tabs, "same", p2)
	if a == b {
		t.Fatal("duplicate labels shared a runtime identity")
	}
	if err := tabs.Start(a); !errors.Is(err, ErrTerminalTabsUnmounted) {
		t.Fatalf("pre-mount start: %v", err)
	}
	switcher := NewTabs(WithTab("terminals", tabs), WithTab("other", NewText("other")))
	h := startAppInternal(t, switcher, 40, 8)
	defer h.stopInternal()
	h.onLoopInternal(func() {
		if err := tabs.Select(b); err != nil {
			t.Error(err)
		}
		for _, tab := range tabs.Tabs() {
			if tab.Running {
				t.Error("selection or mount started a process")
			}
		}
		if err := tabs.Start(a); err != nil {
			t.Error(err)
		}
		if err := tabs.Start(b); err != nil {
			t.Error(err)
		}
		if !tabs.ctx.FocusWithin(pickTerminal(tabs, b)) {
			t.Error("selection focus escaped its parent")
		}
		switcher.Select(1)
		for _, tab := range tabs.Tabs() {
			if !tab.Running {
				t.Error("drawer hiding stopped a process")
			}
		}
	})
	if _, err := io.WriteString(p1.w, "hidden output\r\n"); err != nil {
		t.Fatal(err)
	}
	h.onLoopInternal(func() {
		switcher.Select(0)
		if tabs.Active() != b {
			t.Error("remount changed selected identity")
		}
		if p1.closes.Load() != 0 || p2.closes.Load() != 0 {
			t.Error("hide/remount requested cleanup")
		}
	})
}

func pickTerminal(tabs *TerminalTabs, id TerminalTabID) *Terminal {
	e, _ := tabs.find(id)
	return e.terminal
}

func TestTerminalTabsRemovalRetainsErrorsAndRejectsClosingAndStaleTargets(t *testing.T) {
	tabs := NewTerminalTabs()
	p := newResultProc()
	want := errors.New("hangup failed")
	p.closeErr = want
	a := addTestTerminal(t, tabs, "agent", p)
	h := startAppInternal(t, tabs, 40, 8)
	defer h.stopInternal()
	var first, again *TerminalTabRemovalResult
	h.onLoopInternal(func() {
		if err := tabs.Start(a); err != nil {
			t.Error(err)
		}
		first, _ = tabs.Remove(a)
		again, _ = tabs.Remove(a)
		if first != again {
			t.Error("repeated removal replaced cleanup result")
		}
		if err := tabs.SendText(a, "do not send"); !errors.Is(err, ErrTerminalTabClosing) {
			t.Errorf("closing send: %v", err)
		}
		if err := tabs.SendPaste(a, "do not paste"); !errors.Is(err, ErrTerminalTabClosing) {
			t.Errorf("closing paste: %v", err)
		}
		if err := tabs.Start(a); !errors.Is(err, ErrTerminalTabClosing) {
			t.Errorf("closing start: %v", err)
		}
		if err := tabs.Select(a); err != nil {
			t.Error(err)
		}
		if tabs.ctx.FocusWithin(pickTerminal(tabs, a)) {
			t.Error("closing process still receives input focus")
		}
	})
	awaitResultEvent(t, p.entered)
	close(p.release)
	ctxWait, cancelWait := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWait()
	if err := first.Wait(ctxWait); !errors.Is(err, want) {
		t.Fatalf("cleanup failure: %v", err)
	}
	h.onLoopInternal(func() {
		if len(tabs.Tabs()) != 1 {
			t.Error("failed cleanup dropped its tab")
		}
		if err := tabs.ForceRemove(a); err != nil {
			t.Error(err)
		}
		if tabs.Active() != "" || len(tabs.listChildren()) != 0 {
			t.Error("last removal did not produce an empty container")
		}
		if err := tabs.SendText(a, "stale"); !errors.Is(err, ErrTerminalTabUnknown) {
			t.Errorf("stale target: %v", err)
		}
		b, err := tabs.Add("agent")
		if err != nil || b == a {
			t.Error("removed identity was reused")
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tabs.ShutdownWithResult().Wait(ctx); !errors.Is(err, want) {
		t.Fatalf("force removal hid shutdown error: %v", err)
	}
}

func TestTerminalTabsShutdownRequestsAllCleanupAndSurvivesEndedLoop(t *testing.T) {
	tabs := NewTerminalTabs()
	p1, p2 := newResultProc(), newResultProc()
	a, b := addTestTerminal(t, tabs, "one", p1), addTestTerminal(t, tabs, "two", p2)
	h := startAppInternal(t, tabs, 40, 8)
	stop := sync.OnceFunc(h.stopInternal)
	defer stop()
	h.onLoopInternal(func() {
		if err := tabs.Start(a); err != nil {
			t.Error(err)
		}
		if err := tabs.Start(b); err != nil {
			t.Error(err)
		}
	})
	stop()
	r := tabs.ShutdownWithResult()
	awaitResultEvent(t, p1.entered)
	awaitResultEvent(t, p2.entered) // the first remains blocked
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !errors.Is(r.Wait(ctx), context.Canceled) {
		t.Error("cancelled shutdown wait claimed success")
	}
	close(p1.release)
	close(p2.release)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if r != tabs.ShutdownWithResult() {
		t.Error("shutdown replaced its completion handle")
	}
}

func TestTerminalTabsNeverStartedRemovalAndDistinctStripTargets(t *testing.T) {
	tabs := NewTerminalTabs()
	var added int
	var menu TerminalTabID
	tabs.SetActions(func() { added++ }, func(id TerminalTabID) { menu = id })
	a, _ := tabs.Add("one")
	b, _ := tabs.Add("two")
	h := startAppInternal(t, tabs, 40, 8)
	defer h.stopInternal()
	h.onLoopInternal(func() {
		tabs.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: 7})
		if menu != b || tabs.Active() != a {
			t.Error("menu target selected or addressed the wrong page")
		}
		tabs.HandleEvent(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 13})
		if added != 1 || tabs.Active() != a {
			t.Error("plus target overlapped a page")
		}
		r, err := tabs.Remove(a)
		if err != nil || r.Started() || r.StartError() != nil {
			t.Error("never-started tab removal invented a process")
		}
		if tabs.Active() != b || len(tabs.Tabs()) != 1 {
			t.Error("first removal failed to select remaining page")
		}
		if _, err := tabs.Remove(b); err != nil {
			t.Error(err)
		}
		if tabs.Active() != "" || len(tabs.Tabs()) != 0 {
			t.Error("last removal was not empty")
		}
		tabs.HandleEvent(tui.KeyEvent{Code: tui.KeyPageDown, Mods: tui.ModCtrl})
	})
}

type heldInputWriter struct {
	entered, release chan struct{}
	once             sync.Once
}

func TestTerminalTabsRemovalJoinsOlderGenerationsAfterRestart(t *testing.T) {
	tabs := NewTerminalTabs()
	p1, p2 := newResultProc(), newResultProc()
	want := errors.New("older generation cleanup failed")
	p1.closeErr = want
	n := 0
	id, _ := tabs.Add("restarted", WithStarter(func(pty.Cmd) (TerminalProcess, error) {
		n++
		if n == 1 {
			return p1, nil
		}
		return p2, nil
	}))
	h := startAppInternal(t, tabs, 60, 8)
	defer h.stopInternal()
	h.onLoopInternal(func() {
		if err := tabs.Start(id); err != nil {
			t.Error(err)
		}
	})
	p1.end()
	awaitResultEvent(t, p1.entered)
	pumpsGone(t, pickTerminal(tabs, id))
	h.syncInternal()
	var r *TerminalTabRemovalResult
	h.onLoopInternal(func() {
		if err := tabs.Start(id); err != nil {
			t.Error(err)
		}
		var err error
		r, err = tabs.Remove(id)
		if err != nil {
			t.Error(err)
		}
	})
	awaitResultEvent(t, p2.entered)
	close(p2.release)
	select {
	case <-r.Done():
		t.Error("latest-generation success hid older pending cleanup")
	default:
	}
	close(p1.release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := r.Wait(ctx); !errors.Is(err, want) {
		t.Fatalf("older failure was discarded: %v", err)
	}
	h.onLoopInternal(func() {
		if len(tabs.Tabs()) != 1 {
			t.Error("older failed generation allowed confirmed removal")
		}
	})
}

func TestTerminalTabsKeyboardSelectionAndFailedStartAreExplicit(t *testing.T) {
	tabs := NewTerminalTabs()
	want := errors.New("start refused")
	starts, adds := 0, 0
	a, _ := tabs.Add("bad", WithStarter(func(pty.Cmd) (TerminalProcess, error) { starts++; return nil, want }))
	b, _ := tabs.Add("unstarted")
	tabs.SetActions(func() { adds++ }, nil)
	h := startAppInternal(t, tabs, 40, 8)
	defer h.stopInternal()
	h.onLoopInternal(func() {
		tabs.ctx.RequestFocus()
		tabs.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyPageDown, Mods: tui.ModCtrl})
		if tabs.Active() != b {
			t.Error("subtree next did not change selection")
		}
		tabs.ctx.RequestFocus()
		tabs.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyLeft})
		if tabs.Active() != a || starts != 0 {
			t.Error("bar navigation started a process")
		}
		tabs.ctx.RequestFocus()
		tabs.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: '+', Text: "+"})
		if adds != 1 {
			t.Error("keyboard plus did not request addition")
		}
		if err := tabs.Start(a); !errors.Is(err, want) {
			t.Errorf("start failure: %v", err)
		}
		if err := tabs.Select(b); err != nil {
			t.Error(err)
		}
		if err := tabs.Select(a); err != nil {
			t.Error(err)
		}
		if starts != 1 {
			t.Error("selection retried a failed process")
		}
		if err := tabs.SendText(a, "ignored"); !errors.Is(err, ErrTerminalNotRunning) {
			t.Errorf("nonrunning send: %v", err)
		}
		if !errors.Is(tabs.Tabs()[0].StartError, want) {
			t.Error("list lost startup failure")
		}
	})
}

func TestTerminalTabsDestroyedOwnerSuppressesLateExitAndChangeCallbacks(t *testing.T) {
	tabs := NewTerminalTabs()
	p := newResultProc()
	exits, changes, beforeShutdown := 0, 0, 0
	tabs.SetOnChanged(func() { changes++ })
	id, _ := tabs.Add("owner", WithStarter(func(pty.Cmd) (TerminalProcess, error) { return p, nil }), WithOnExit(func(int) { exits++ }))
	h := startAppInternal(t, tabs, 40, 8)
	defer h.stopInternal()
	var terminal *Terminal
	var shutdown *TerminalShutdownResult
	h.onLoopInternal(func() {
		if err := tabs.Start(id); err != nil {
			t.Error(err)
		}
		terminal = pickTerminal(tabs, id)
		beforeShutdown = changes
		// Exit is queued while this loop turn still runs. Destruction must
		// invalidate the owner callbacks before that queued exit is delivered.
		p.end()
		shutdown = tabs.ShutdownWithResult()
	})
	awaitResultEvent(t, p.entered)
	close(p.release)
	pumpsGone(t, terminal) // the output reader has posted its exit to the loop
	h.syncInternal()
	h.onLoopInternal(func() {
		if exits != 0 || changes != beforeShutdown {
			t.Errorf("destroyed owner received callbacks: exits=%d changes=%d want=%d", exits, changes, beforeShutdown)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown.Wait(ctx); err != nil {
		t.Fatal(err)
	}
}

func (w *heldInputWriter) Write(b []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(b), nil
}

func TestTerminalInputBudgetIncludesInflightAndRejectsWholeSend(t *testing.T) {
	w := &heldInputWriter{entered: make(chan struct{}), release: make(chan struct{})}
	iq := newInputQueue(w)
	if err := iq.offer([]byte(strings.Repeat("a", TerminalInputLimit))); err != nil {
		t.Fatal(err)
	}
	awaitResultEvent(t, w.entered)
	if err := iq.offer([]byte("must not fit")); !errors.Is(err, ErrTerminalInputFull) {
		t.Fatalf("in-flight budget ignored: %v", err)
	}
	iq.mu.Lock()
	queued := len(iq.q)
	iq.mu.Unlock()
	if queued != 0 {
		t.Error("rejected send queued a partial payload")
	}
	iq.close()
	close(w.release)
	awaitResultEvent(t, iq.done)
	if err := iq.offer([]byte("closed")); !errors.Is(err, ErrTerminalNotRunning) {
		t.Fatalf("closed queue accepted input: %v", err)
	}
}

// SendPaste reaches a running tab by its identity, and is refused as SendText is: an unknown tab, a
// stopped one.
func TestTerminalTabsSendAPasteByIdentity(t *testing.T) {
	tabs := NewTerminalTabs()
	p := newResultProc()
	defer close(p.release)
	a := addTestTerminal(t, tabs, "a", p)
	if err := tabs.SendPaste("nope", "x"); !errors.Is(err, ErrTerminalTabUnknown) {
		t.Errorf("an unknown tab: %v", err)
	}
	if err := tabs.SendPaste(a, "x"); !errors.Is(err, ErrTerminalNotRunning) {
		t.Errorf("a tab not started: %v", err)
	}
}

// SetModeKey reaches every tab, those already there and those added later.
func TestTerminalTabsModeKeyReachesEveryTab(t *testing.T) {
	tabs := NewTerminalTabs()
	before := newResultProc()
	defer close(before.release)
	addTestTerminal(t, tabs, "before", before)
	tabs.SetModeKey(func(e tui.KeyEvent) bool { return e.Code == 't' && e.Mods.Chord() == tui.ModCtrl })
	after := newResultProc()
	defer close(after.release)
	addTestTerminal(t, tabs, "after", after)
	for _, e := range tabs.entries {
		if e.terminal.cfg.modeKey == nil || !e.terminal.isModeKey(tui.KeyEvent{Code: 't', Mods: tui.ModCtrl}) {
			t.Errorf("tab %q has no mode key", e.label)
		}
	}
}

// SetModelessScroll reaches every tab, those already there and those added later; SetOnMode reports
// each tab's mode changes by its identity.
func TestTerminalTabsModelessScrollAndOnModeReachEveryTab(t *testing.T) {
	tabs := NewTerminalTabs()
	before := newResultProc()
	defer close(before.release)
	first := addTestTerminal(t, tabs, "before", before)
	tabs.SetModelessScroll(true)
	var seen []string
	tabs.SetOnMode(func(id TerminalTabID, m TerminalMode) { seen = append(seen, string(id)+":"+m.String()) })
	after := newResultProc()
	defer close(after.release)
	second := addTestTerminal(t, tabs, "after", after)
	for _, e := range tabs.entries {
		if !e.terminal.cfg.modeless {
			t.Errorf("tab %q is not modeless", e.label)
		}
	}
	for _, e := range tabs.entries {
		e.terminal.setMode(TerminalNormal)
		e.terminal.setMode(TerminalInput)
	}
	want := []string{string(first) + ":NORMAL", string(first) + ":TERMINAL", string(second) + ":NORMAL", string(second) + ":TERMINAL"}
	if strings.Join(seen, " ") != strings.Join(want, " ") {
		t.Errorf("modes reported %v, want %v", seen, want)
	}
}
