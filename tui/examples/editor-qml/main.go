// Command editor-qml is a small modal text editor whose screen is written in
// QML.
//
//	editor-qml [file]
//
// It mirrors github.com/yongjohnlee80/editor, which builds the same screen in
// Go. Here the layout is editor.qml and the Go is only behaviour: what Save does
// to a file, what the status line says, when the clock ticks.
package main

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/term"
)

func main() {
	path := ""
	if len(os.Args) > 1 {
		path = os.Args[1]
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// The tree needs a scheduler before the App it schedules onto exists — the
	// App is built around the tree's root. The clock's first tick is a second
	// away, so the pointer is set long before anything reads it; it is atomic so
	// that "long before" is a guarantee rather than a hope.
	var app atomic.Pointer[tui.App]
	schedule := func(fn func()) {
		if a := app.Load(); a != nil {
			a.Update(fn)
		}
	}

	var failed error
	host, root, err := New(Options{
		Path:     path,
		Schedule: schedule,
		Sink:     func(err error) { failed = err; cancel() },
		Quit:     cancel,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "editor-qml:", err)
		os.Exit(1)
	}
	defer host.Close()

	backend, err := term.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "editor-qml: cannot open the terminal:", err)
		os.Exit(1)
	}
	a := tui.NewApp(root, tui.WithBackend(backend))
	app.Store(a)

	if err := a.Run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "editor-qml:", err)
		os.Exit(1)
	}
	if failed != nil {
		fmt.Fprintln(os.Stderr, "editor-qml:", failed)
		os.Exit(1)
	}
}
