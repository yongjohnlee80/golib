package decl_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// recordingType is a consumer's type that records the widget it builds in shared state, and whose
// Destroyed clears the record when it is that widget: AutoDoc's GlobeMap view does this. live is
// read and written on the program's loop only.
func recordingType(live *tui.Component) tuidecl.Type {
	return tuidecl.Type{Name: "Globe", Children: tuidecl.NoChildren(),
		Build: func(tuidecl.Build) (tui.Component, []string, error) {
			w := widget.NewText("globe")
			*live = w
			return w, nil, nil
		},
		Destroyed: func(c tui.Component) {
			w := c.(*widget.Text) // a consumer's hook may assert its own widget, unchecked
			if *live == w {
				*live = nil
			}
		}}
}

// checkLiveRecord fails unless live is the widget the document's globe has now.
func checkLiveRecord(t *testing.T, s *decltest.Screen, live *tui.Component, after string) {
	t.Helper()
	onScreenLoop(t, s, func() {
		c, ok := s.Program.Find("globe")
		if !ok || *live == nil || *live != c {
			t.Errorf("after %s the consumer's record is %v, the globe mounted is %v (found %v); want the mounted one",
				after, *live, c, ok)
		}
	})
}

// A Program.Reload runs no consumer type's Build or Destroyed for anything but the tree it shows.
// Before, its keyId check mounted the new layout apart, Build and all, and destroyed it: a type
// recording its widget recorded the throwaway one and then cleared it, so after a reload that kept
// the node (AutoDoc's theme switch) the live widget was recorded nowhere.
func TestAReloadKeepsAConsumerTypesRecordOfItsLiveWidget(t *testing.T) {
	var live tui.Component
	layout := func(word string) []byte {
		return []byte("import tui 1.0\nWindow { Flex { direction: Tui.Vertical\n Text { text: \"" + word + "\" }\n Globe { id: globe } } }")
	}
	s := decltest.Run(t, 30, 4, tuidecl.LayoutSource("main.qml", layout("before")), tuidecl.Types(recordingType(&live)))
	s.WaitForText(t, "before")
	checkLiveRecord(t, s, &live, "the mount")
	var err error
	onScreenLoop(t, s, func() { _, err = s.Program.Reload(layout("after")) })
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	s.WaitForText(t, "after")
	checkLiveRecord(t, s, &live, "a reload")
}

// A hot reload, which checks its keyIds the same way (reloadSpec), keeps the record too.
func TestAHotReloadKeepsAConsumerTypesRecordOfItsLiveWidget(t *testing.T) {
	var live tui.Component
	d := newHotDir(t, hotLayout(`Text { text: "before" }
Globe { id: globe }`))
	s := d.run(tuidecl.Types(recordingType(&live)))
	s.WaitForText(t, "before")
	d.write("main.qml", hotLayout(`Text { text: "after" }
Globe { id: globe }`))
	s.WaitForText(t, "after")
	checkLiveRecord(t, s, &live, "a hot reload")
}
