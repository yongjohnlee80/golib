package tui

import "testing"

// plainBackend is a backend with the required methods alone: none of TestBackend's capabilities.
type plainBackend struct{ Backend }

// App.Bell reaches a backend with a bell, once per call, and answers false for one without.
func TestTheBellReachesABackendWithOne(t *testing.T) {
	h := startApp(t, NewFlex(Vertical), 10, 2)
	var rang bool
	h.onLoop(func() {
		first := h.app.Bell()
		rang = h.app.Bell() && first // two calls, two rings: never one call read twice
	})
	if !rang || h.tb.Bells() != 2 {
		t.Fatalf("Bell answered %v, the backend rang %d times; want two rings", rang, h.tb.Bells())
	}

	tb := NewTestBackend(10, 2)
	p := runApp(t, NewApp(NewFlex(Vertical), WithBackend(plainBackend{tb}), WithMinFrameInterval(0)), tb)
	p.onLoop(func() { rang = p.app.Bell() })
	if rang || tb.Bells() != 0 {
		t.Fatalf("a backend without a bell: Bell answered %v, rang %d", rang, tb.Bells())
	}
}
