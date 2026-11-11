package gui

import (
	"testing"

	"github.com/yongjohnlee80/gio/unit"
)

// TestResolvedOptionsAreWhatTheWindowOpensWith: ResolveOptions reads, before any window, what a
// list of options asks for, the way NewBackend resolves it: unset decorations are the platform's
// choice, a later option overrides an earlier one, a nil option is none, and the window's own
// option list (windowOptions) follows the same values.
func TestResolvedOptionsAreWhatTheWindowOpensWith(t *testing.T) {
	t.Parallel()
	if on, set := ResolveOptions().Decorations(); set || on {
		t.Fatalf("no option: decorations on=%v set=%v, want the platform's choice (unset)", on, set)
	}
	if on, set := ResolveOptions(WithDecorations(false)).Decorations(); !set || on {
		t.Fatalf("WithDecorations(false): on=%v set=%v", on, set)
	}
	if on, set := ResolveOptions(WithDecorations(false), nil, WithDecorations(true)).Decorations(); !set || !on {
		t.Fatalf("a later WithDecorations(true): on=%v set=%v", on, set)
	}
	o := ResolveOptions(WithTitle("AutoDoc"), WithSize(800, 600), WithZeroCopyFrames())
	if o.Title() != "AutoDoc" {
		t.Errorf("title %q", o.Title())
	}
	if w, h := o.Size(); w != unit.Dp(800) || h != unit.Dp(600) {
		t.Errorf("size %vx%v", w, h)
	}
	if !o.ZeroCopyFrames() || ResolveOptions().ZeroCopyFrames() {
		t.Errorf("zero-copy frames: asked %v, default %v", o.ZeroCopyFrames(), ResolveOptions().ZeroCopyFrames())
	}
	if d := ResolveOptions(); d.Title() != defaultConfig().title {
		t.Errorf("the default title %q, want %q", d.Title(), defaultConfig().title)
	}
	// the window opens with what ResolveOptions read: NewBackend's configuration is resolve's
	b := NewBackend(WithDecorations(false), WithTitle("AutoDoc"))
	if b.cfg.decorated || !b.cfg.decoration || b.cfg.title != "AutoDoc" {
		t.Fatalf("NewBackend's configuration: decorated=%v set=%v title=%q", b.cfg.decorated, b.cfg.decoration, b.cfg.title)
	}
}
