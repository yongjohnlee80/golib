package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// flow_test.go: Flow in a document, Qt's wrapping row. A row of buttons too wide for the window
// wraps rather than being clipped, every button whole and clickable; a reload inserts into it.

const flowDoc = `import tui 1.0
import demo 1.0
Window {
 Flex { direction: Tui.Vertical
  Flow { spacing: 1; rowSpacing: 0
   Button { text: "one"; onClicked: App.log("one") }
   Button { text: "two"; onClicked: App.log("two") }
   Button { text: "three"; onClicked: App.log("three") }
  }
  Text { text: "under" }
 }
}`

func runFlow(t *testing.T, doc string, width int) (*decltest.Screen, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := decltest.Run(t, width, 6, tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "under")
	return s, rec
}

// rowOf is the screen row text first appears on, or -1.
func rowOf(s *decltest.Screen, text string) int {
	for y, line := range strings.Split(s.String(), "\n") {
		if strings.Contains(line, text) {
			return y
		}
	}
	return -1
}

func TestAFlowOfButtonsWrapsInANarrowWindow(t *testing.T) {
	wide, _ := runFlow(t, flowDoc, 40)
	if r1, r3, ru := rowOf(wide, "one"), rowOf(wide, "three"), rowOf(wide, "under"); r1 != r3 || ru != r1+1 {
		t.Errorf("wide: one on row %d, three on %d, under on %d; want one line, then under\n%s", r1, r3, ru, wide.String())
	}
	narrow, rec := runFlow(t, flowDoc, 12)
	r1, r2, r3, ru := rowOf(narrow, "one"), rowOf(narrow, "two"), rowOf(narrow, "three"), rowOf(narrow, "under")
	if r1 < 0 || r2 < 0 || r3 <= r1 || ru <= r3 {
		t.Fatalf("narrow: one %d, two %d, three %d, under %d; want three wrapped below, under below it\n%s", r1, r2, r3, ru, narrow.String())
	}
	// the wrapped button is whole and clickable where it was placed
	clickAt(t, narrow, "three")
	narrow.WaitFor(t, "three clicked", func(string) bool { return strings.Contains(logged(rec), "three") })
}

// A reload that adds a button to a Flow inserts it in place: the Flow's own Add, in order.
func TestAReloadInsertsIntoAFlow(t *testing.T) {
	s, _ := runFlow(t, flowDoc, 40)
	reloadScreen(t, s, strings.Replace(flowDoc, `Button { text: "two";`, `Button { text: "new"; onClicked: App.log("new") }
   Button { text: "two";`, 1))
	s.WaitForText(t, "new")
	line := strings.Split(s.String(), "\n")[rowOf(s, "one")]
	if !(strings.Index(line, "one") < strings.Index(line, "new") && strings.Index(line, "new") < strings.Index(line, "two")) {
		t.Errorf("after the reload the row reads %q; want one, new, two in order", line)
	}
}
