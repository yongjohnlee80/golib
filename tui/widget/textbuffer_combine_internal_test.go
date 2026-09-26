package widget

import "testing"

// An inserted combining mark extends the preceding grapheme instead of
// creating a second cursor column. The next typed rune must not slice beyond
// the recomposed line. Both Editor and TextArea use this buffer.
func TestInsertingACombiningMarkKeepsTheNextClusterInBounds(t *testing.T) {
	b := newTextBuffer()
	b.insertText("e")
	b.insertText("\u0301")
	if b.col != 1 {
		t.Fatalf("combining mark created a phantom cursor column: %d", b.col)
	}
	b.insertText("c")
	if got := b.value(); got != "e\u0301c" {
		t.Fatalf("insertion broke the composed text: %q", got)
	}
	if b.col != 2 {
		t.Fatalf("cursor after composed grapheme and c = %d, want 2", b.col)
	}
}
