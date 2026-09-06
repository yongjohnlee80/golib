// Command demo runs the golib TUI demo on a terminal.
//
// The component tree lives in tui/examples/demoapp so the SAME code can also be
// driven by tui/web (see tui/examples/webdemo). That split is acceptance:
// one component tree, two backends, no changes to the tree.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/examples/demoapp"
	"github.com/yongjohnlee80/golib/tui/term"
)

func main() {
	backend, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui demo: cannot open the terminal:", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app := tui.NewApp(demoapp.New(cancel, true), tui.WithBackend(backend))
	if err := app.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "tui demo:", err)
		os.Exit(1)
	}
}
