package decl_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// A Text's font.bold and font.italic draw it so, as Qt's font group does.
func TestATextsFontIsBoldOrItalic(t *testing.T) {
	s := decltest.Run(t, 30, 4, tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window { Flex { Text { text: "LABEL"; font.bold: true } Text { text: "note"; font.italic: true } Text { text: "plain" } } }`)))
	s.WaitForText(t, "LABEL")
	s.WaitForText(t, "plain")
	snap := s.Backend.Snapshot()
	find := func(word string) (bold, italic bool) {
		for _, row := range snap {
			for x := range row {
				if row[x].Content == string(word[0]) && x+len(word) <= len(row) {
					ok := true
					for i := range word {
						if row[x+i].Content != string(word[i]) {
							ok = false
							break
						}
					}
					if ok {
						return row[x].Attrs.Mask&tui.AttrBold != 0, row[x].Attrs.Mask&tui.AttrItalic != 0
					}
				}
			}
		}
		t.Fatalf("%q not on screen", word)
		return
	}
	if b, i := find("LABEL"); !b || i {
		t.Errorf("LABEL: bold %v italic %v", b, i)
	}
	if b, i := find("note"); b || !i {
		t.Errorf("note: bold %v italic %v", b, i)
	}
	if b, i := find("plain"); b || i {
		t.Errorf("plain: bold %v italic %v", b, i)
	}
}
