package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/core"
)

func TestOverlayHostPanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewOverlayHost(nil) did not panic")
		}
	}()
	_ = core.NewOverlayHost(nil)
}

func TestOverlayHostAttachPanicsOnNil(t *testing.T) {
	host := core.NewOverlayHost(core.NewText("base"))
	defer func() {
		if recover() == nil {
			t.Fatal("OverlayHost.Attach(nil) did not panic")
		}
	}()
	host.Attach(nil)
}

func TestOverlayHostRendersBase(t *testing.T) {
	base := core.NewText("hello from base")
	host := core.NewOverlayHost(base)
	sh := newShell(host)
	h := startApp(t, sh, 30, 5)
	h.settle()
	h.wantContains("hello from base")
}
