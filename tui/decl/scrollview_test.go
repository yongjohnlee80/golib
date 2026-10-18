package decl_test

import (
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// A ScrollView holds exactly one child, and refuses none or two by name.
func TestAScrollViewHoldsOneChild(t *testing.T) {
	for doc, want := range map[string]string{
		`ScrollView { }`:                     "ScrollView needs exactly 1 child, got 0",
		"ScrollView { Text { }\n Text { } }": "ScrollView needs exactly 1 child, got 2",
	} {
		_, err := mountDoc(t, "import tui 1.0\n"+doc)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n err = %v\nwant %q", doc, err, want)
		}
	}
}

// A ScrollView's one child is shown from its top, a column narrower for the indicator.
func TestAScrollViewShowsItsChild(t *testing.T) {
	s := decltest.Run(t, 20, 4,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow {\n"+
			" ScrollView { Flex { Text { text: \"top\" }\n Text { text: \"two\" }\n Text { text: \"three\" }\n"+
			" Text { text: \"four\" }\n Text { text: \"five\" }\n Text { text: \"bottom\" } } } }")))
	s.WaitForText(t, "top")
	if !strings.HasPrefix(row(s, 0), "top") || strings.Contains(s.String(), "bottom") {
		t.Fatalf("want the first rows shown from the top:\n%s", s)
	}
}
