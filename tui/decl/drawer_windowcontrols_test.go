package decl_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// requests counts the window-control requests a document hands its host, by name.
type requests struct {
	mu sync.Mutex
	n  map[string]int
}

func (r *requests) handlers() map[string]decl.HandlerFunc {
	r.n = map[string]int{}
	out := map[string]decl.HandlerFunc{}
	for _, name := range []string{"minimize", "maximize", "close"} {
		out["App."+name] = func([]qml.SpecValue) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.n[name]++
			return nil
		}
	}
	return out
}

func (r *requests) get(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n[name]
}

// drawerControlsDoc is a 40 by 10 Window with a non-modal Drawer dr across the top, its Frame's
// title row on row 0, its windowControls as given, each request handed to the host.
func drawerControlsDoc(controls string) []byte {
	return []byte(`import tui 1.0
import demo 1.0
Window {
 Text { text: "page" }
 Drawer { id: dr; modal: false; edge: Tui.Top; size: 50; windowControls: "` + controls + `"
  onMinimizeRequested: App.minimize(); onMaximizeRequested: App.maximize(); onCloseRequested: App.close()
  Frame { title: "DR"; Text { text: "drawer" } } }
}`)
}

// pressAt presses and releases the left button at (x, 0).
func pressAt(t *testing.T, s *decltest.Screen, x int) {
	t.Helper()
	s.Keys(t, tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: 0},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: 0})
}

// A Drawer's windowControls put the listed icons on its Frame's title row, in the title bar's order
// whatever the order written, and none unlisted. A press on one emits its request and does nothing
// else: the Drawer stays open, for the host to decide.
func TestADrawersWindowControlsRequestAndNeverAct(t *testing.T) {
	r := &requests{}
	s := decltest.Run(t, 40, 10, tuidecl.LayoutSource("main.qml", drawerControlsDoc("close minimize")),
		tuidecl.Singleton("demo", "1.0", "App"), tuidecl.Handlers(r.handlers()))
	call(t, s, "dr", "open")
	// 38 inner columns: the two controls take 32 to 37, a border cell before the corner
	s.WaitFor(t, "_ then x at the title row's end", func(sc string) bool {
		row := []rune(strings.Split(sc, "\n")[0])
		return len(row) >= 38 && string(row[32:38]) == " _  x "
	})
	if row := strings.Split(s.String(), "\n")[0]; strings.Contains(row, "+") {
		t.Errorf("an unlisted maximize was drawn: %q", row)
	}
	pressAt(t, s, 36)
	s.WaitFor(t, "x: closeRequested alone", func(string) bool { return r.get("close") == 1 && r.get("minimize") == 0 })
	pressAt(t, s, 33)
	s.WaitFor(t, "_: minimizeRequested", func(string) bool { return r.get("minimize") == 1 && r.get("close") == 1 })
	if !strings.Contains(s.String(), "drawer") {
		t.Errorf("a request acted: the Drawer is gone\n%s", s.String())
	}
	if n := r.get("maximize"); n != 0 {
		t.Errorf("maximizeRequested %d times; no + was pressed", n)
	}
}

// windowControls are refused at build: on a Drawer whose content is no Frame, and with a word that
// is no control, or one written twice.
func TestADrawersWindowControlsAreRefusedAtBuild(t *testing.T) {
	for name, tc := range map[string]struct {
		doc  string
		want string
	}{
		"content no Frame": {`import tui 1.0
Window { Text { text: "page" }
 Drawer { windowControls: "close"; Text { text: "drawer" } } }`, "a Drawer's windowControls need a Frame child"},
		"an unknown word": {`import tui 1.0
Window { Text { text: "page" }
 Drawer { windowControls: "close shut"; Frame { title: "t"; Text { text: "d" } } } }`, `"shut": want minimize, maximize or close`},
		"a word twice": {`import tui 1.0
Window { Text { text: "page" }
 Drawer { windowControls: "close close"; Frame { title: "t"; Text { text: "d" } } } }`, `"close" twice`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte(tc.doc)),
				tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(30, 4))))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("built with %v; want refused, saying %q", err, tc.want)
			}
		})
	}
	// "" is none, and builds
	if _, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
Window { Text { text: "page" }
 Drawer { windowControls: ""; Text { text: "drawer" } } }`)),
		tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(30, 4)))); err != nil {
		t.Errorf("windowControls: \"\" (none) refused: %v", err)
	}
}
