package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// Each handle's pointer is the arrows of the way it drags, a corner's diagonal; a handle there is
// none of has the default.
func TestEachHandlesPointerIsTheWayItDrags(t *testing.T) {
	for h, want := range map[Handle]tui.PointerShape{
		HandleLeft: tui.PointerResizeEW, HandleRight: tui.PointerResizeEW, HandleVerticalDivider: tui.PointerResizeEW,
		HandleTop: tui.PointerResizeNS, HandleBottom: tui.PointerResizeNS, HandleHorizontalDivider: tui.PointerResizeNS,
		HandleTopLeft: tui.PointerResizeNWSE, HandleBottomRight: tui.PointerResizeNWSE,
		HandleTopRight: tui.PointerResizeNESW, HandleBottomLeft: tui.PointerResizeNESW,
		Handle(99): tui.PointerDefault,
	} {
		if got := h.pointerShape(); got != want {
			t.Errorf("%v: %v, want %v", h, got, want)
		}
	}
}

// A split that has had no room yet has no divider to shape.
func TestASplitWithNoRoomShapesNothing(t *testing.T) {
	s := NewSplit(Horizontal, NewText(""), NewText(""))
	if got := s.PointerShapes(nil); len(got) != 0 {
		t.Errorf("a split never laid out: %+v", got)
	}
}

// An HTML view never laid out has no links showing to shape.
func TestAnHTMLViewNeverLaidOutShapesNothing(t *testing.T) {
	v := NewHTMLView()
	v.SetHTML([]byte(`<a href="b.md">a link</a>`))
	if got := v.PointerShapes(nil); len(got) != 0 {
		t.Errorf("a view never laid out: %+v", got)
	}
}
