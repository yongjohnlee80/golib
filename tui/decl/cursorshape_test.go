package decl_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// declShapeHost is a TestBackend that shapes the pointer, keeping the last frame's runs.
type declShapeHost struct {
	*tui.TestBackend
	mu   sync.Mutex
	last []tui.PointerShapePlacement
}

func (b *declShapeHost) PlacePointerShapes(ps []tui.PointerShapePlacement) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.last = append([]tui.PointerShapePlacement(nil), ps...)
}

func (b *declShapeHost) at(x, y int) tui.PointerShape {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := tui.PointerDefault
	for _, p := range b.last {
		if y == p.Y && x >= p.X && x < p.X+p.W {
			s = p.Shape
		}
	}
	return s
}

// A ListView's cursorShape is the pointer over its rows, in Qt's names; one it doesn't know is
// refused, with the names it does.
func TestAListViewsCursorShapeIsOverItsRows(t *testing.T) {
	m := tuidecl.NewListModel("label")
	m.Reset([]tuidecl.Row{{"label": "a"}, {"label": "b"}})
	doc := func(shape string) tuidecl.ProgramOption {
		return tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
			"ListView { model: App.items; textRole: \"label\"; cursorShape: "+shape+" }"))
	}
	sh := &declShapeHost{TestBackend: tui.NewTestBackend(20, 4)}
	p, err := tuidecl.NewProgram(doc("Tui.PointingHandCursor"), tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.items": m}), tuidecl.AppOptions(tui.WithBackend(sh), tui.WithMinFrameInterval(0)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = p.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	deadline := time.Now().Add(5 * time.Second)
	for sh.at(0, 0) != tui.PointerHand || sh.at(19, 1) != tui.PointerHand || sh.at(0, 2) != tui.PointerDefault {
		if time.Now().After(deadline) {
			t.Fatalf("rows 0, 1, 2: %v, %v, %v; want a hand over the two rows alone", sh.at(0, 0), sh.at(19, 1), sh.at(0, 2))
		}
		time.Sleep(5 * time.Millisecond)
	}

	_, err = tuidecl.NewProgram(doc(`"hand"`), tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.items": m}), tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(20, 4))))
	if err == nil || !strings.Contains(err.Error(), "Tui.PointingHandCursor") {
		t.Errorf("cursorShape: \"hand\": %v, want it refused with the names it takes", err)
	}
}

// A ListView's cursorShape follows its binding: when the source it reads moves, the rows' pointer
// changes with the next frame, back to the arrow and to the hand again.
func TestAListViewsBoundCursorShapeFollowsItsSource(t *testing.T) {
	m := tuidecl.NewListModel("label")
	m.Reset([]tuidecl.Row{{"label": "a"}, {"label": "b"}})
	sh := &declShapeHost{TestBackend: tui.NewTestBackend(20, 4)}
	p, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+
		"ListView { model: App.items; textRole: \"label\"; cursorShape: App.linky ? Tui.PointingHandCursor : Tui.ArrowCursor }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.items": m, "App.linky": true}),
		tuidecl.AppOptions(tui.WithBackend(sh), tui.WithMinFrameInterval(0)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = p.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	rows := func(want tui.PointerShape, what string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for sh.at(0, 0) != want || sh.at(19, 1) != want {
			if time.Now().After(deadline) {
				t.Fatalf("%s: rows 0, 1: %v, %v; want %v", what, sh.at(0, 0), sh.at(19, 1), want)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	rows(tui.PointerHand, "the first frame")
	p.Post(func() { _ = p.Set("App.linky", false) })
	rows(tui.PointerDefault, "the source cleared")
	p.Post(func() { _ = p.Set("App.linky", true) })
	rows(tui.PointerHand, "the source set again")
}
