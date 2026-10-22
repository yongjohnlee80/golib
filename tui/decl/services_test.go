package decl_test

import (
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// dock is a consumer's own minimized-window collector: it collects and presents as
// MinimizedWindows does, and is not one.
type dock struct{ *widget.MinimizedWindows }

// A WindowTaskbar presents any collector that presents its entries (widget.WindowEntries), not
// only a *widget.MinimizedWindows. Before, it asserted the concrete type and refused a consumer's
// own.
func TestAWindowTaskbarTakesAConsumersCollector(t *testing.T) {
	s := decltest.Run(t, 30, 3, tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nFlex { WindowTaskbar { }\n Text { text: \"body\" } }")),
		tuidecl.AdapterOptions(tuidecl.WithWindowCollector(dock{widget.NewMinimizedWindows()})))
	s.WaitForText(t, "body")
}

// greeter and word are a consumer's own services: a pointer, and an interface.
type greeter struct{ hello string }
type word interface{ Word() string }
type plain string

func (p plain) Word() string { return string(p) }

// A builder takes the adapter's services by type (Service[T]), given with WithService: a
// pointer, and an interface, registered as itself. One never given is absent.
func TestABuilderTakesItsServicesByType(t *testing.T) {
	var missing bool
	sign := tuidecl.Type{Name: "Sign", Children: tuidecl.NoChildren(),
		Build: func(b tuidecl.Build) (tui.Component, []string, error) {
			g, ok := tuidecl.Service[*greeter](b)
			w, wok := tuidecl.Service[word](b)
			if !ok || !wok {
				return nil, nil, fmt.Errorf("services missing: greeter %v, word %v", ok, wok)
			}
			_, has := tuidecl.Service[*testing.T](b)
			missing = !has
			return widget.NewText(g.hello + " " + w.Word()), nil, nil
		}}
	s := decltest.Run(t, 30, 3, tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nSign { }")),
		tuidecl.Types(sign),
		tuidecl.AdapterOptions(tuidecl.WithService(&greeter{hello: "hello"}), tuidecl.WithService[word](plain("world"))))
	s.WaitForText(t, "hello world")
	if !missing {
		t.Error("Service[*testing.T] was found, though never given")
	}
}
