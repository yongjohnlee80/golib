// Command docks shows a dock style and scoped Shortcuts: two panels from one style file
// (docks/Panel.qml), each with its own d, which wins over the Window's d while focus is in the
// panel. F2 and F3 open them; Ctrl+Q quits.
package main

import (
	"context"
	"embed"
	"fmt"
	"os"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/term"
)

//go:embed main.qml
var layout []byte

// styles are the program's dock styles, one component file each.
//
//go:embed docks
var styles embed.FS

// programOptions are the whole program: its document, its dock styles, and what the document
// reaches of it. say puts a line in the status.
func programOptions(say func(string), quit func()) []tuidecl.ProgramOption {
	files := tuidecl.NewListModel("name")
	files.Reset([]tuidecl.Row{{"name": "notes.md"}, {"name": "todo.md"}, {"name": "README.md"}})
	notes := tuidecl.NewListModel("name")
	notes.Reset([]tuidecl.Row{{"name": "ship the docks example"}, {"name": "write the README"}})
	return []tuidecl.ProgramOption{
		tuidecl.LayoutSource("main.qml", layout),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Components(styles, "docks", "demo.docks", "1.0"),
		tuidecl.Sources(map[string]any{"App.status": "", "App.files": files, "App.notes": notes}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{
			"App.say":  func(args []qml.SpecValue) error { say(args[0].Raw); return nil },
			"App.quit": func([]qml.SpecValue) error { quit(); return nil },
		}),
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "docks:", err)
		os.Exit(1)
	}
}

func run() error {
	backend, err := term.Open()
	if err != nil {
		return fmt.Errorf("cannot open the terminal: %w", err)
	}
	var p *tuidecl.Program
	say := func(s string) { _ = p.Set("App.status", s) }
	quit := func() { p.Quit() }
	p, err = tuidecl.NewProgram(append(programOptions(say, quit), tuidecl.AppOptions(tui.WithBackend(backend)))...)
	if err != nil {
		return err
	}
	return p.Run(context.Background())
}
