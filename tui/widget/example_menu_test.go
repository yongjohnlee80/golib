package widget_test

// A COMPILE-CHECKED example of the Menu composition, in an EXTERNAL test
// package so it can only use the published API.
//
// The package documentation and the README both show this arrangement in prose
// code blocks, which the compiler never reads: an example there can go on
// naming a renamed constant or a removed option for as long as nobody tries it.
// This file is the same arrangement as real code, so a change to the public
// surface breaks the build rather than the documentation.

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// newFileAction and quitAction are the application's own actions. A row carries
// one; the Menu never interprets it.
type newFileAction struct{}

func (newFileAction) ActionID() tui.ActionID { return "file.new" }

type quitAction struct{}

func (quitAction) ActionID() tui.ActionID { return "app.quit" }

// ExampleNewMenu builds the full arrangement: a model, a dispatch executor, a
// bar to place it along an edge, and the OverlayHost the levels need.
func ExampleNewMenu() {
	// The dispatch table is the application's; the Menu only needs a function.
	commands := map[tui.ActionID]func() bool{
		"file.new": func() bool { fmt.Println("new file"); return true },
		"app.quit": func() bool { fmt.Println("quit"); return true },
	}

	menu := widget.NewMenu(
		widget.WithActionExecutor(func(inv tui.ActionInvocation) bool {
			run, ok := commands[inv.Action.ActionID()]
			if !ok {
				return false // unhandled: the menu stays open
			}
			return run()
		}))

	if err := menu.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", newFileAction{}),
			widget.NewSeparator("sep"),
			widget.NewCheck("wrap", "Wrap lines", nil),
		}),
		widget.NewCommand("quit", "Quit", quitAction{}),
	}); err != nil {
		fmt.Println("model rejected:", err)
		return
	}

	// The bar places the menu along an edge. It wraps a Menu that already
	// exists and configures it only while mounted.
	bar := widget.NewMenuBar(menu, widget.WithBarPlacement(widget.BarPlacementTop))

	// A Menu's levels are anchored overlay layers, so whichever of the two is
	// mounted must sit inside an OverlayHost. The nearest enclosing one holds
	// them, so nesting hosts is well defined.
	root := tui.NewFlex(tui.Vertical)
	root.Add(bar)
	root.AddWeighted(widget.NewText("content"), 1)
	host := widget.NewOverlayHost(root)

	fmt.Println(bar.Placement(), host != nil, menu.OpenLevels())
	// Output: top true 0
}

// ExampleMenu_Open shows the error contract: Open reports what happened rather
// than leaving a caller waiting for a popup that is not coming.
func ExampleMenu_Open() {
	menu := widget.NewMenu()
	if err := menu.SetModel([]widget.MenuItemModel{
		widget.NewCommand("quit", "Quit", nil),
	}); err != nil {
		fmt.Println(err)
		return
	}
	// Not a submenu, so there is nothing to open.
	fmt.Println(menu.Open("quit") != nil)
	// Unmounted, so there is no host to open it on.
	fmt.Println(menu.Open("nope") != nil)
	// Output:
	// true
	// true
}
