package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestOverlayHostPanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewOverlayHost(nil) did not panic")
		}
	}()
	_ = widget.NewOverlayHost(nil)
}

func TestOverlayHostAttachPanicsOnNil(t *testing.T) {
	host := widget.NewOverlayHost(widget.NewText("base"))
	defer func() {
		if recover() == nil {
			t.Fatal("OverlayHost.Attach(nil) did not panic")
		}
	}()
	host.Attach(nil)
}

func TestOverlayHostRendersBase(t *testing.T) {
	base := widget.NewText("hello from base")
	host := widget.NewOverlayHost(base)
	sh := newShell(host)
	h := startApp(t, sh, 30, 5)
	h.settle()
	h.wantContains("hello from base")
}

