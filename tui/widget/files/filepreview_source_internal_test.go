package files

import (
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/tui/widget/editor"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

func TestFactoryPreviewOwnsStatesAndPaletteDoesNotRenewSource(t *testing.T) {
	var traces []*widgettest.SourceTrace
	d := highlight.Definition{Name: "Preview source", Extensions: []string{"*.custom"}, SourceFactory: func(*highlight.Catalog) highlight.Source {
		trace := &widgettest.SourceTrace{T: t, Refs: map[highlight.State]int{}}
		traces = append(traces, trace)
		return highlight.Source{Highlighter: trace, States: trace}
	}}
	catalog := highlight.NewRepository(d).Snapshot()
	preview := NewFilePreview(FilePaneStyles{}, false)
	preview.SetSourceHighlighting(catalog, editor.SyntaxStyles{})
	files := FileSource{FS: fstest.MapFS{"one.custom": &fstest.MapFile{Data: []byte("one\ntwo")}, "two.custom": &fstest.MapFile{Data: []byte("next")}}}
	preview.Show(files, "one.custom", false)
	f := preview.view.Core().BeginHighlight(0)
	f.Styles(1)
	f.Close()
	old := traces[len(traces)-1]
	count := len(traces)
	preview.SetSourceHighlighting(catalog, editor.SyntaxStyles{})
	if len(traces) != count {
		t.Fatal("palette rebuilt semantic source")
	}
	preview.Show(files, "two.custom", false)
	for id, n := range old.Refs {
		if n != 0 {
			t.Fatal("preview switch leaked old roots", id, n)
		}
	}
}
