// Command demo runs golib/tui's demo application in a native window: the same component tree
// tui/examples/webdemo serves to a browser, on the gui backend instead of a terminal.
//
//	go run ./examples/demo
package main

import (
	"context"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/examples/demoapp"

	"github.com/yongjohnlee80/golib/gui"
)

func main() {
	gui.Main(func() error {
		ctx, quit := context.WithCancel(context.Background())
		defer quit()
		backend := gui.NewBackend(gui.WithTitle("golib/tui demo"))
		app := tui.NewApp(demoapp.New(quit, true), tui.WithBackend(backend))
		return app.Run(ctx)
	})
}
