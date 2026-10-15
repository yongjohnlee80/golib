package widget

import "testing"

// Detach leaves no buffer holding the editor: not the one it shows, nor one it left a place in
// before, which stays open.
func TestDetachLeavesNoBufferHoldingTheEditor(t *testing.T) {
	e := NewEditorCore(CoreInitialText("a"))
	a := e.Buffer()
	b := NewBuffer(BufferText("b"))
	e.SetBuffer(b)
	if _, ok := a.s.places[e]; !ok {
		t.Fatal("A kept no place for the editor that left it")
	}
	e.Detach()
	if _, ok := a.s.places[e]; ok {
		t.Fatal("A still holds the detached editor's place")
	}
	if _, ok := b.s.places[e]; ok || len(b.s.editors) != 0 {
		t.Fatalf("B still holds the detached editor: places %v, editors %d", b.s.places, len(b.s.editors))
	}
	if len(e.placed) != 0 {
		t.Fatalf("the detached editor still lists %d buffers", len(e.placed))
	}
}
