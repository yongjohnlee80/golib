package sgr

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/style"
)

// Every attribute term emits is read back by its code, ended by its off code, and the table names
// every attribute a mask carries.
func TestAttrsReadBackAsEmitted(t *testing.T) {
	var all style.AttrMask
	for _, a := range Attrs {
		if got, ok := Set(a.On); !ok || got != a.Attr {
			t.Errorf("SGR %d sets %b, want %b", a.On, got, a.Attr)
		}
		if got, ok := Clear(a.Off); !ok || got&a.Attr == 0 {
			t.Errorf("SGR %d clears %b, want it to end %b", a.Off, got, a.Attr)
		}
		all |= a.Attr
	}
	if all != style.AttrInvisible<<1-1 {
		t.Errorf("the table carries %b, want every attribute", all)
	}
	if got, _ := Clear(22); got != style.AttrBold|style.AttrFaint {
		t.Errorf("SGR 22 clears %b, want bold and faint", got)
	}
	for code, want := range map[int]style.AttrMask{6: style.AttrBlink, 21: style.AttrUnderline} {
		if got, ok := Set(code); !ok || got != want {
			t.Errorf("SGR %d sets %b, want %b", code, got, want)
		}
	}
	if _, ok := Set(30); ok {
		t.Error("a colour code reads as an attribute")
	}
}
