// Command editor-qml is a small modal text editor whose screen is written in
// QML.
//
//	editor-qml [-dev dir] [-lang ko_KR] [file]
//
// -dev reads the QML from dir — this directory, when run from it — instead of
// the copy built in, and follows the files: edit editor.qml, a theme or a
// dialog, save, and the running editor shows the change, keeping what is
// typed. Go code is compiled; changing it still means rebuilding.
//
// It mirrors github.com/yongjohnlee80/editor, which builds the same screen in
// Go. Here the layout is editor.qml and the Go is only behaviour: what Save does
// to a file, what the status line says, when the clock ticks.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/term"
)

func main() {
	dev := flag.String("dev", "", "read the QML from this directory and follow edits to it")
	lang := flag.String("lang", "", "the language to start in: en, ko_KR, pt_BR or zh_CN (default English)")
	flag.Parse()
	path := flag.Arg(0)
	if err := run(path, *dev, *lang); err != nil {
		fmt.Fprintln(os.Stderr, "editor-qml:", err)
		os.Exit(1)
	}
}

func run(path, dev, lang string) error {
	backend, err := term.Open()
	if err != nil {
		return fmt.Errorf("cannot open the terminal: %w", err)
	}
	host, err := New(Options{Path: path, Dev: dev, Language: lang, App: []tui.AppOption{tui.WithBackend(backend)}})
	if err != nil {
		return err
	}
	return host.Run(context.Background())
}
