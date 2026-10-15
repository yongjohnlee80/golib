package decl_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func key(code rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: code} }

// scores is a two-column result: names and numbers that sort differently as text.
func scores() *tuidecl.ListModel {
	m := tuidecl.NewListModel("name", "n")
	m.SetColumns(tuidecl.Column{Role: "name", Title: "NAME"}, tuidecl.Column{Role: "n", Title: "N"})
	m.Reset([]tuidecl.Row{{"name": "carol", "n": 41}, {"name": "alice", "n": 9}, {"name": "bob", "n": 100}})
	return m
}

func runCells(t *testing.T, doc string, m tuidecl.ItemModel, rec *recorder) *decltest.Screen {
	t.Helper()
	return decltest.Run(t, 40, 8,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": m}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.use": rec.handler}))
}

// A cellCursor TableView moves by cell, sorts the loaded rows, and reports the MODEL's row in
// activated and currentIndexChanged whatever the sort shows.
func TestACellCursorTableViewSortsAndReportsTheModelsRow(t *testing.T) {
	m, rec := scores(), &recorder{}
	s := runCells(t, `TableView { model: App.rows; cellCursor: true; sortable: true; onActivated: App.use(index) }`, m, rec)
	s.WaitFor(t, "the rows", func(sc string) bool { return strings.Contains(sc, "carol") && strings.Contains(sc, "100") })
	s.Keys(t, key(tui.KeyTab), decltest.Rune('l'), decltest.Rune('s')) // focus, the N field, sort by it
	s.WaitFor(t, "sorted by N", func(string) bool {
		return rowOfText(s, "alice") < rowOfText(s, "carol") && rowOfText(s, "carol") < rowOfText(s, "bob")
	})
	s.Keys(t, decltest.Rune('g'), key(tui.KeyEnter)) // the first row shown: alice, the model's row 1
	s.WaitFor(t, "activated", func(string) bool { return len(rec.all()) == 1 })
	if got := rec.all()[0].Raw; got != "1" {
		t.Errorf("activated(index) = %s, want 1, alice's row in the model", got)
	}
}

// pagedRows is a model with more rows than it has loaded: a Fetcher, as a query's result is.
// FetchMore loads the next page later, as a host's answer arrives.
type pagedRows struct {
	*tuidecl.ListModel
	total, page, asked int
	loading, pending   bool // pending: asked before the host could answer
	post               func(func())
}

func (p *pagedRows) CanFetchMore(tuidecl.Index) bool { return p.Len() < p.total }
func (p *pagedRows) FetchMore(tuidecl.Index) {
	if p.post == nil {
		p.pending = true // the view asks while the document is built; the host answers once it can
		return
	}
	if p.loading {
		return
	}
	p.loading, p.asked = true, p.asked+1
	p.post(func() {
		var rows []tuidecl.Row
		for i := p.Len(); i < min(p.Len()+p.page, p.total); i++ {
			rows = append(rows, tuidecl.Row{"name": fmt.Sprintf("row%d", i), "n": i})
		}
		p.loading = false
		p.Insert(p.Len(), rows...)
	})
}

// A model that is a Fetcher is asked for rows as the cursor nears the end, and the rows that
// arrive are shown below the ones loaded.
func TestACellCursorTableViewAsksAFetcherForMoreRows(t *testing.T) {
	m, rec := &pagedRows{ListModel: tuidecl.NewListModel("name", "n"), total: 300, page: 100}, &recorder{}
	m.SetColumns(tuidecl.Column{Role: "name", Title: "NAME"}, tuidecl.Column{Role: "n", Title: "N"})
	var s *decltest.Screen
	s = decltest.RunWith(t, 40, 8, func(p *tuidecl.Program) error {
		m.post = func(fn func()) { p.Post(fn) }
		if m.pending {
			p.Post(func() { m.FetchMore(tuidecl.Index{}) })
		}
		return nil
	},
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			"TableView { model: App.rows; cellCursor: true; onCurrentIndexChanged: App.use(index) }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": tuidecl.ItemModel(m)}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.use": rec.handler}))
	s.WaitFor(t, "the first page", func(sc string) bool { return strings.Contains(sc, "row0") })
	s.Keys(t, key(tui.KeyTab), decltest.Rune('G')) // to the last row loaded: within a page of the end
	s.WaitFor(t, "the last page", func(string) bool {
		var n int
		onScreenLoop(t, s, func() { n = m.Len() })
		return n == 300
	})
	if m.asked < 2 {
		t.Errorf("asked %d times for 300 rows in pages of 100", m.asked)
	}
	// rows arriving are not a new result: the cursor stays on the row it was on
	got := rec.all()
	if len(got) == 0 || got[len(got)-1].Raw != "199" {
		t.Errorf("currentIndexChanged went %v; want it to end on 199, where G put the cursor", got)
	}
}

func TestACellCursorTableViewRefusesWhatItCannotShow(t *testing.T) {
	for doc, want := range map[string]string{
		`TableView { model: App.rows; sortable: true }`:                                           "sortable needs cellCursor",
		`TableView { model: App.rows; cellCursor: true; delegate: Text { text: model.display } }`: "takes no delegate",
		`TableView { model: App.rows; cellCursor: "yes" }`:                                        "TableView.cellCursor",
		`TableView { model: App.rows; cellCursor: true; onContextMenuRequested: App.use(index) }`: "has no contextMenuRequested",
	} {
		err := tuidecl.Check(
			tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+doc)),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Sources(map[string]any{"App.rows": scores()}),
			tuidecl.Handlers(map[string]decl.HandlerFunc{"App.use": (&recorder{}).handler}))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", doc, err, want)
		}
	}
}

// A style wraps a standard type's Build without replacing it: a TableView read privately keeps its
// node, and the wrap sees the DataTable a cellCursor view shows.
func TestAStyleWrapsATableViewItCannotReplace(t *testing.T) {
	var wrapped *widget.DataTable
	st := tuidecl.Style{Name: "probe", Wrap: map[string]func(tui.Component){
		"TableView": func(c tui.Component) { wrapped = tuidecl.TableOf(c) },
	}}
	s := decltest.Run(t, 40, 8,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nTableView { model: App.rows; cellCursor: true }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": scores()}),
		tuidecl.WithStyle(st))
	s.WaitForText(t, "carol")
	if wrapped == nil {
		t.Fatal("the wrap did not see the cell-cursor view's DataTable")
	}
	err := tuidecl.Check(tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nText {}")),
		tuidecl.WithStyle(tuidecl.Style{Name: "bad", Wrap: map[string]func(tui.Component){"NoSuchType": func(tui.Component) {}}}))
	if err == nil || !strings.Contains(err.Error(), "NoSuchType") {
		t.Errorf("a wrap of an unknown type: err %v", err)
	}
}

// A cell-cursor TableView wears the document's palette, shows its declared columns, takes its
// currentIndex from the document, and reads it back as the model's row.
func TestACellCursorTableViewWearsThePaletteAndKeepsCurrentIndex(t *testing.T) {
	rec := &recorder{}
	s := runCells(t, `Window {
    palette.base: "#0000aa"; palette.text: "#ffff55"
    TableView {
        id: v; model: App.rows; cellCursor: true; currentIndex: 2
        TableViewColumn { role: "name"; title: "WHO"; width: 8 }
    }
    Shortcut { sequence: "Ctrl+G"; onActivated: App.use(v.currentIndex) }
}`, scores(), rec)
	s.WaitFor(t, "the declared column", func(sc string) bool {
		return strings.Contains(sc, "WHO") && strings.Contains(sc, "bob") && !strings.Contains(sc, "100")
	})
	if got := viewCell(t, s, "WHO").BG; got != (tui.CellColor{Kind: tui.CellColorRGB, B: 0xaa}) {
		t.Errorf("header background %+v, want the document base", got)
	}
	s.Keys(t, decltest.Ctrl('g'))
	s.WaitFor(t, "currentIndex read", func(string) bool { return len(rec.all()) == 1 })
	if got := rec.all()[0].Raw; got != "2" {
		t.Errorf("currentIndex = %s, want 2 as the document set it", got)
	}
}
