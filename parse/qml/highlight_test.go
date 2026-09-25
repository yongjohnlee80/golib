package qml_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// highlight_test.go holds the QML highlighter to its styles — one line at a
// time — and to its one hard rule: it never refuses.

// styled renders a line as "text:style" runs, uncovered bytes skipped, so a
// failure reads as the line does.
func styled(line string, spans []highlight.Span) string {
	var b strings.Builder
	for _, s := range spans {
		fmt.Fprintf(&b, "[%s:%s]", line[s.Start:s.End], s.Style)
	}
	return b.String()
}

func highlightOne(t *testing.T, line string, prev highlight.State) (string, highlight.State) {
	t.Helper()
	spans, next := qml.Highlighter().HighlightBlock(line, prev)
	return styled(line, spans), next
}

func TestTheQMLHighlighterStylesEachKind(t *testing.T) {
	for line, want := range map[string]string{
		`import QtQuick.Controls 2.15 as C`:      "[import:keyword][QtQuick.Controls:import][2.15:float][as:keyword][C:import]",
		`import "dialogs"`:                       `[import:keyword]["dialogs":string]`,
		`Rectangle { width: 100; color: "red" }`: `[Rectangle:dataType][width:attribute][::operator][100:decVal][;:operator][color:attribute][::operator]["red":string]`,
		`property int count: 0x1f`:               "[property:keyword][int:dataType][count:attribute][::operator][0x1f:baseN]",
		`onClicked: App.save(true)`:              "[onClicked:attribute][::operator][App:dataType][.:operator][save:function][true:constant]",
		`if (a >= 2.5e3) return null`:            "[if:controlFlow][>=:operator][2.5e3:float][return:controlFlow][null:constant]",
		`text: "a\"b\n"`:                         `[text:attribute][::operator]["a:string][\":specialChar][b:string][\n:specialChar][":string]`,
		`x // TODO: fix`:                         "[// :comment][TODO:alert][: fix:comment]",
		`signal done(string path)`:               "[signal:keyword][done:function][string:dataType]",
	} {
		if got, _ := highlightOne(t, line, 0); got != want {
			t.Errorf("%s\n got %s\nwant %s", line, got, want)
		}
	}
}

// TestAConstructLeftOpenCarriesToTheNextLine — Qt's block state.
func TestAConstructLeftOpenCarriesToTheNextLine(t *testing.T) {
	got, state := highlightOne(t, "width: 1 /* a", 0)
	if !strings.HasSuffix(got, "[/* a:comment]") || state == 0 {
		t.Fatalf("an open block comment: %s, state %d", got, state)
	}
	got, state2 := highlightOne(t, "still */ height: 2", state)
	if !strings.HasPrefix(got, "[still */:comment][height:attribute]") || state2 != 0 {
		t.Fatalf("the comment closing: %s, state %d", got, state2)
	}
	got, state = highlightOne(t, "text: `a ${b}", 0)
	if !strings.HasSuffix(got, "[`a ${b}:string]") || state == 0 {
		t.Fatalf("an open template: %s, state %d", got, state)
	}
	if got, state = highlightOne(t, "c` + d", state); !strings.HasPrefix(got, "[c`:string][+:operator]") || state != 0 {
		t.Fatalf("the template closing: %s, state %d", got, state)
	}
	// A plain string ends at the line: nothing carries.
	if _, state = highlightOne(t, `text: "open`, 0); state != 0 {
		t.Fatalf("an unterminated plain string carried state %d", state)
	}
}

// TestTheHighlighterNeverRefuses: half-typed and hostile input alike — every
// span in order, inside the line, never overlapping, and it returns.
func TestTheHighlighterNeverRefuses(t *testing.T) {
	inputs := []string{"", " ", "Text {", `"`, "`", "/*", "*/", "0x", "1e", "1e+", ".", "..", "$",
		"é中文 { ok: 1 }", "\xff\xfe", "a\\", `"\`, "import", "import ", "::", "}}}}", "@#!`"}
	for _, in := range inputs {
		for _, prev := range []highlight.State{0, 1, 2, 99} {
			spans, _ := qml.Highlighter().HighlightBlock(in, prev)
			last := 0
			for _, s := range spans {
				if s.Start < last || s.End <= s.Start || s.End > len(in) {
					t.Errorf("%q (state %d): bad span %+v after %d in %+v", in, prev, s, last, spans)
				}
				last = s.End
			}
		}
	}
}
