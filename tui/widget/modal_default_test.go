package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// yesNo is a Yes/No dialog over body: No the default when noDefault, and disabled when noOff.
func yesNo(t *testing.T, body tui.Component, noDefault, noOff bool) (*harness, func() string) {
	t.Helper()
	var log []string
	note := func(s string) func() { return func() { log = append(log, s) } }
	no := widget.NewButton("No", widget.WithRole(widget.ButtonRoleReject), widget.WithDefault(noDefault), widget.WithOnActivate(note("no")))
	if noOff {
		no.SetEnabled(false)
	}
	md := widget.NewModal(body, widget.WithButtons(
		widget.NewButton("Yes", widget.WithRole(widget.ButtonRoleAccept), widget.WithOnActivate(note("yes"))), no))
	host := widget.NewOverlayHost(widget.NewText(""))
	h := startApp(t, host, 60, 12)
	h.onLoop(func() {
		if err := md.Open(host); err != nil {
			t.Fatalf("Open: %v", err)
		}
	})
	h.settle()
	read := func() string {
		var s string
		h.onLoop(func() { s = strings.Join(log, ",") })
		return s
	}
	return h, read
}

// A dialog whose body takes no focus starts on its default button, so Enter answers the default:
// a Yes/No defaulting to No answers Enter with No. With no default, or a disabled one, it starts on
// its first enabled button, as before.
func TestATextDialogStartsOnItsDefaultButton(t *testing.T) {
	for _, c := range []struct {
		name             string
		noDefault, noOff bool
		want             string
	}{
		{"No the default", true, false, "no"},
		{"no default", false, false, "yes"},
		{"No the default, disabled", true, true, "yes"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, read := yesNo(t, widget.NewText("Delete the file?"), c.noDefault, c.noOff)
			defer h.stop()
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
			h.waitFor("the answer", func() bool { return read() != "" })
			if got := read(); got != c.want {
				t.Errorf("Enter answered %q, want %q", got, c.want)
			}
		})
	}
}

// A body with a field still starts in the field, its Enter answering the default.
func TestADialogWithAFieldStillStartsInIt(t *testing.T) {
	field := widget.NewTextInput()
	h, read := yesNo(t, field, true, false)
	defer h.stop()
	h.inject(keyText("q"), tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("the answer", func() bool { return read() != "" })
	var typed string
	h.onLoop(func() { typed = field.Value() })
	if got := read(); got != "no" || typed != "q" {
		t.Errorf("Enter answered %q with the field holding %q, want no and q", got, typed)
	}
}
