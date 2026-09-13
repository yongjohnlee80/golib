package tui_test

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
)

type simpleComponent struct{}

func (s *simpleComponent) Init(ctx *tui.Context) {}
func (s *simpleComponent) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: 10, H: 1})
}
func (s *simpleComponent) Render(surf tui.Surface)       {}
func (s *simpleComponent) HandleEvent(ev tui.Event) bool { return false }

func ExampleExclusive() {
	opt := tui.Exclusive("search-query")
	fmt.Println(opt != nil)
	// Output:
	// true
}

func ExampleWithTrace() {
	var events []tui.TraceEvent
	opt := tui.WithTrace(func(ev tui.TraceEvent) {
		events = append(events, ev)
	})
	fmt.Println(opt != nil)
	// Output:
	// true
}

func ExampleNewApp() {
	backend := tui.NewTestBackend(80, 24)
	app := tui.NewApp(&simpleComponent{}, tui.WithBackend(backend))
	fmt.Println(app != nil)
	// Output:
	// true
}
