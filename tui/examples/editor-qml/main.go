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

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/term"
)

func main() {
	path := ""
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	if err := run(path); err != nil {
		fmt.Fprintln(os.Stderr, "editor-qml:", err)
		os.Exit(1)
	}
}

func run(path string) error {
	backend, err := term.Open()
	if err != nil {
		return fmt.Errorf("cannot open the terminal: %w", err)
	}
	host, err := New(Options{Path: path, App: []tui.AppOption{tui.WithBackend(backend)}})
	if err != nil {
		return err
	}
	return host.Run(context.Background())
}
