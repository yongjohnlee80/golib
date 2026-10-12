package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A Modal is a pinned layer while it is one of the host's open dialogs, and an ordinary one
// otherwise: a panel is raised beneath an open dialog, never above it.
func TestAnOpenModalIsPinned(t *testing.T) {
	h := NewOverlayHost(NewText("beneath"))
	m := NewModal(NewText("dialog"))
	if h.pinned(m) {
		t.Fatal("a Modal the host has not opened is pinned")
	}
	h.modals = append(h.modals, m)
	if !h.pinned(m) {
		t.Fatal("an open Modal is not pinned")
	}
	if h.pinned(NewText("text")) {
		t.Fatal("a layer that is neither a dialog nor a float is pinned")
	}
}

// A hidden Float has no content rectangle; a shown one has the one its last layout gave it.
func TestAHiddenFloatHasNoContentRect(t *testing.T) {
	f := NewFloat(NewText("panel"))
	f.content = tui.Rect{X: 1, Y: 1, W: 5, H: 2}
	if got := f.ContentRect(); got != (tui.Rect{}) {
		t.Fatalf("a hidden float's content rect: %+v", got)
	}
	f.shown = true
	if got := f.ContentRect(); got != (tui.Rect{X: 1, Y: 1, W: 5, H: 2}) {
		t.Fatalf("a shown float's content rect: %+v", got)
	}
}
