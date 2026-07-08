package widget_test

// Composition checks (ADR-0007 §2.8, §5.9, §5.10): the sqlit and
// lazygit-shaped UIs assemble from the v1 inventory alone and their
// interaction scripts pass deterministically on TestBackend.

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// sqlitApp is the §2.8 sqlit shape: tables sidebar, query editor, results
// pane, status bar.
type sqlitApp struct {
	root    tui.Component
	tables  *widget.List[string]
	editor  *widget.TextArea
	results *widget.List[string]
	status  *widget.StatusBar

	ctx *tui.Context
}

func newSqlitApp() *sqlitApp {
	a := &sqlitApp{
		tables:  widget.NewList(widget.WithItems[string](nil, func(s string) string { return s })),
		editor:  widget.NewTextArea(widget.WithWrap(widget.WrapNone)),
		results: widget.NewList(widget.WithItems[string](nil, func(s string) string { return s })),
		status:  widget.NewStatusBar(),
	}
	main := widget.NewSplit(widget.Vertical,
		widget.NewBox(a.editor, widget.WithTitle("Query"), widget.WithStatus("F5 run")),
		widget.NewBox(a.results, widget.WithTitle("Results")),
		widget.WithRatio(0.4))
	body := widget.NewSplit(widget.Horizontal,
		widget.NewBox(a.tables, widget.WithTitle("Tables")),
		main, widget.WithRatio(0.25), widget.WithMinSizes(20, 40))
	dock := tui.NewDock()
	dock.Pin(tui.DockBottom, a.status)
	dock.Add(body)
	a.root = dock
	return a
}

func (a *sqlitApp) Init(ctx *tui.Context) {
	a.ctx = ctx
	ctx.Mount(a.root)
	a.status.SetLeft("sqlit")
	a.status.SetRight("Tab: focus · F5: run")
	// Async table fill (§2.6): owner = this controller.
	ctx.Go(func(context.Context) (any, error) {
		return []string{"users", "orders", "invoices"}, nil
	})
}

func (a *sqlitApp) Layout(c tui.Constraints) tui.Size {
	sz := a.ctx.LayoutChild(a.root, c)
	a.ctx.PlaceChild(a.root, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
	return sz
}

func (a *sqlitApp) Render(tui.Surface) {}

func (a *sqlitApp) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.TaskResult:
		if e.Err == nil {
			if rows, ok := e.Value.([]string); ok {
				a.tables.SetItems(rows)
				a.status.SetCenter(fmt.Sprintf("%d tables", len(rows)))
				return true
			}
		}
		return false
	case tui.KeyEvent:
		if e.Kind == tui.KeyPress && e.Code == tui.KeyF5 {
			// Query → results flow: run the editor's query.
			q := a.editor.Value()
			a.results.SetItems([]string{"row-1 · " + q, "row-2 · " + q})
			a.status.SetCenter("2 rows")
			return true
		}
	}
	return false
}

// TestSqlitComposition asserts §5.9: the composition builds and its
// interaction script (focus cycle, async table fill, query→results flow,
// status updates) passes deterministically.
func TestSqlitComposition(t *testing.T) {
	app := newSqlitApp()
	h := startApp(t, app, 80, 20)

	// Async table fill lands.
	h.waitFor("tables filled", func() bool { return strings.Contains(h.grid(), "invoices") })
	h.wantContains("3 tables")
	h.wantContains("Tables")
	h.wantContains("Query")
	h.wantContains("Results")
	h.wantContains("F5 run")

	// Focus cycle: Tables → Query → Results (document order), with the
	// focused Box border highlighted (TokenBorderFocused = ANSI 4).
	h.inject(tab())
	h.waitFor("tables pane focused", func() bool {
		a := cellAttrs(h, 0, 0)
		return a.FG.Kind == tui.CellColorANSI && a.FG.Index == 4
	})
	h.inject(tab()) // Query editor
	h.waitFor("query pane focused", func() bool {
		a := cellAttrs(h, 0, 0)
		return a.FG.Kind == tui.CellColorDefault // Tables pane un-highlights
	})

	// Type a query into the editor, then F5 (app-level keybinding —
	// TextArea has no submit key, Q5) runs it.
	h.inject(typeString("select 1")...)
	h.waitFor("query typed", func() bool { return strings.Contains(h.grid(), "select 1") })
	h.inject(key(tui.KeyF5))
	h.waitFor("results", func() bool { return strings.Contains(h.grid(), "row-2 · select 1") })
	h.wantContains("2 rows")

	// Third stop: Results pane.
	h.inject(tab())
	h.inject(key(tui.KeyDown), key(tui.KeyEnter))
	acts := record[widget.ActivateEvent](h)
	h.inject(key(tui.KeyEnter))
	h.waitFor("activate", func() bool { return acts.count() > 0 })
}

// TestLazygitComposition asserts §5.10: the 5-panel lazygit shape builds;
// the modal Float traps focus, dims, dismisses on Esc with DismissEvent;
// BufferView streams a fake subprocess script with ANSI colors intact.
func TestLazygitComposition(t *testing.T) {
	statusBox := widget.NewBox(widget.NewList(widget.WithItems([]string{"master → origin"}, func(s string) string { return s })), widget.WithTitle("Status"))
	filesBox := widget.NewBox(widget.NewList(widget.WithItems([]string{"M main.go"}, func(s string) string { return s })), widget.WithTitle("Files"))
	branchesBox := widget.NewBox(widget.NewList(widget.WithItems([]string{"master"}, func(s string) string { return s })), widget.WithTitle("Branches"))
	commitsBox := widget.NewBox(widget.NewList(widget.WithItems([]string{"abc123 init"}, func(s string) string { return s })), widget.WithTitle("Commits"))
	view := widget.NewBufferView()
	mainBox := widget.NewBox(view, widget.WithTitle("Diff"))
	hints := widget.NewStatusBar()

	left := widget.NewSplit(widget.Vertical, statusBox,
		widget.NewSplit(widget.Vertical, filesBox,
			widget.NewSplit(widget.Vertical, branchesBox, commitsBox)))
	dock := tui.NewDock()
	dock.Pin(tui.DockBottom, hints)
	dock.Add(widget.NewSplit(widget.Horizontal, left, mainBox, widget.WithRatio(0.35)))

	msgInput := widget.NewTextInput(widget.WithPlaceholder("commit message"))
	dialog := widget.NewFloat(widget.NewBox(msgInput, widget.WithTitle("Commit message")),
		widget.WithModal(true), widget.WithDimBackground(true))
	host := widget.NewOverlayHost(dock)
	host.Attach(dialog)

	h := startApp(t, host, 80, 24)
	dismissed := record[widget.DismissEvent](h)
	h.onLoop(func() { hints.SetLeft("c: commit · q: quit") })
	h.settle()

	for _, panel := range []string{"Status", "Files", "Branches", "Commits", "Diff"} {
		h.wantContains(panel)
	}

	// Fake `git log --color=always` through the Writer handle (§2.6).
	var w io.Writer
	h.onLoop(func() { w = view.Writer() })
	if _, err := w.Write([]byte("\x1b[33mcommit abc123\x1b[0m\nAuthor: dev\n")); err != nil {
		t.Fatalf("stream write: %v", err)
	}
	h.waitFor("git output", func() bool { return strings.Contains(h.grid(), "commit abc123") })
	// Color intact: find the 'c' of "commit" and assert ANSI yellow (3).
	// Byte offsets map to columns rune-wise (all cells here are width 1).
	found := false
	for y, row := range strings.Split(h.grid(), "\n") {
		x := strings.Index(row, "commit abc123")
		if x < 0 {
			continue
		}
		a := cellAttrs(h, utf8.RuneCountInString(row[:x]), y)
		if a.FG.Kind != tui.CellColorANSI || a.FG.Index != 3 {
			t.Fatalf("streamed SGR lost: %+v", a.FG)
		}
		found = true
	}
	if !found {
		t.Fatalf("streamed line not found")
	}

	// Modal dialog: dim + trap + Esc dismiss.
	h.onLoop(dialog.Show)
	h.settle()
	h.wantContains("Commit message")
	h.wantContains("░")
	h.inject(typeString("fix: things")...)
	h.waitFor("dialog input filled", func() bool {
		var msg string
		h.onLoop(func() { msg = msgInput.Value() })
		return msg == "fix: things"
	})
	// Tab cannot escape the trap: further typing still lands in the dialog.
	h.inject(tab(), tab())
	h.inject(typeString("!")...)
	h.waitFor("trap holds", func() bool {
		var msg string
		h.onLoop(func() { msg = msgInput.Value() })
		return msg == "fix: things!"
	})
	h.inject(key(tui.KeyEscape))
	h.waitFor("dialog dismissed", func() bool { return dismissed.count() == 1 })
	h.waitFor("dialog gone", func() bool { return !strings.Contains(h.grid(), "Commit message") })
	h.wantNotContains("░")
}
