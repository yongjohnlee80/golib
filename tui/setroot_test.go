package tui

import (
	"strings"
	"testing"
	"time"
)

// setroot_test.go holds App.SetRoot to its promise: the old tree goes as an
// unmount does, the new one is mounted, laid out and painted, and it — not a
// wrapper around it — is the root that input with nothing focused reaches.

func TestSetRootReplacesTheTreeOnScreen(t *testing.T) {
	old := &probe{name: "old", fill: "o"}
	h := startApp(t, old, 4, 1)
	h.onLoop(func() {})
	waitCell(t, h, "o")

	next := &probe{name: "next", fill: "n"}
	h.app.SetRoot(next)
	waitCell(t, h, "n")
	if old.unmounts.Load() != 1 {
		t.Errorf("the old root was unmounted %d times, want 1", old.unmounts.Load())
	}
	h.onLoop(func() {
		if old.ctx.Ctx().Err() == nil {
			t.Error("the old root's context outlived it")
		}
	})

	// With nothing focused a key goes to the root: the new one.
	h.inject(keyEv('x'))
	for deadline := time.Now().Add(3 * time.Second); len(next.recorded()) == 0; time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the new root never received the key")
		}
	}

	// Setting the same root again changes nothing.
	h.app.SetRoot(next)
	h.sync()
	if next.unmounts.Load() != 0 || next.inits.Load() != 1 {
		t.Errorf("re-setting the root remounted it: %d unmounts, %d inits", next.unmounts.Load(), next.inits.Load())
	}
}

func TestSetRootBeforeRunIsWhatRunShows(t *testing.T) {
	tb := NewTestBackend(4, 1)
	app := NewApp(&probe{fill: "o"}, WithBackend(tb), WithMinFrameInterval(0))
	app.SetRoot(&probe{fill: "n"})
	h := runApp(t, app, tb)
	waitCell(t, h, "n")
}

func TestSetRootRefusesNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(asString(r), "nil component") {
			t.Fatalf("recovered %v", r)
		}
	}()
	NewApp(&probe{}, WithBackend(NewTestBackend(1, 1))).SetRoot(nil)
}

func asString(v any) string {
	if e, ok := v.(error); ok {
		return e.Error()
	}
	s, _ := v.(string)
	return s
}

func waitCell(t *testing.T, h *harness, want string) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if h.tb.Snapshot()[0][0].Content == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("cell (0,0) is %q, want %q", h.tb.Snapshot()[0][0].Content, want)
		}
	}
}
