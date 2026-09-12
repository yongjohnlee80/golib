package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Tests for capabilities and hierarchical focus seeding (focusFirst).

func TestCapabilities_FocusFirst(t *testing.T) {
	t.Run("box container child receives seeded focus", func(t *testing.T) {
		input := widget.NewTextInput()
		panel := widget.NewBox(input, widget.WithTitle("Panel"))
		flt := widget.NewFloat(panel, widget.WithModal(true))
		host := widget.NewOverlayHost(widget.NewText("base"))
		host.Attach(flt)
		sh := newShell(host)
		h := startApp(t, sh, 40, 10)

		h.onLoop(flt.Show)
		h.settle()

		h.inject(typeString("hello")...)
		h.barrier(sh)

		var val string
		h.onLoop(func() { val = input.Value() })
		if val != "hello" {
			t.Fatalf("expected focusFirst to seed focus to TextInput inside Box, got %q", val)
		}
	})

	t.Run("split childLister pane receives seeded focus", func(t *testing.T) {
		inputA := widget.NewTextInput()
		inputB := widget.NewTextInput()
		sp := widget.NewSplit(widget.Horizontal, inputA, inputB)
		flt := widget.NewFloat(sp, widget.WithModal(true))
		host := widget.NewOverlayHost(widget.NewText("base"))
		host.Attach(flt)
		sh := newShell(host)
		h := startApp(t, sh, 40, 10)

		h.onLoop(flt.Show)
		h.settle()

		h.inject(typeString("splitA")...)
		h.barrier(sh)

		var valA, valB string
		h.onLoop(func() {
			valA = inputA.Value()
			valB = inputB.Value()
		})
		if valA != "splitA" || valB != "" {
			t.Fatalf("expected focusFirst to seed focus to pane A, got valA=%q valB=%q", valA, valB)
		}
	})

	t.Run("tabs bar receives focus and switches via keyboard", func(t *testing.T) {
		tabInput1 := widget.NewTextInput()
		tabInput2 := widget.NewTextInput()
		tabs := widget.NewTabs(
			widget.WithTab("One", tabInput1),
			widget.WithTab("Two", tabInput2),
		)
		flt := widget.NewFloat(tabs, widget.WithModal(true))
		host := widget.NewOverlayHost(widget.NewText("base"))
		host.Attach(flt)
		sh := newShell(host)
		h := startApp(t, sh, 40, 10)

		h.onLoop(flt.Show)
		h.settle()

		// Tabs bar takes focus; pressing ']' switches tab
		h.inject(typeString("]")...)
		h.barrier(sh)

		var active int
		h.onLoop(func() { active = tabs.Active() })
		if active != 1 {
			t.Fatalf("expected tabs bar to receive focus and switch to tab 1 on ']', got active=%d", active)
		}
	})

	t.Run("unfocusable content fallback", func(t *testing.T) {
		staticText := widget.NewText("static display only")
		flt := widget.NewFloat(staticText, widget.WithModal(true))
		host := widget.NewOverlayHost(widget.NewText("base"))
		host.Attach(flt)
		sh := newShell(host)
		h := startApp(t, sh, 40, 10)

		h.onLoop(flt.Show)
		h.settle()

		// Float layer itself handles fallback focus trap
		h.inject(key(tui.KeyEscape))
		h.settle()
		h.wantNotContains("static display only")
	})
}
