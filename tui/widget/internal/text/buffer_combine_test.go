package text

import "testing"

// An inserted combining mark extends the preceding grapheme instead of
// creating a second cursor column. The next typed rune must not slice beyond
// the recomposed line. Both Editor and TextArea use this buffer.
func TestInsertingACombiningMarkKeepsTheNextClusterInBounds(t *testing.T) {
	b := NewBuffer()
	b.InsertText("e")
	b.InsertText("\u0301")
	if b.Col != 1 {
		t.Fatalf("combining mark created a phantom cursor column: %d", b.Col)
	}
	b.InsertText("c")
	if got := b.Value(); got != "e\u0301c" {
		t.Fatalf("insertion broke the composed text: %q", got)
	}
	if b.Col != 2 {
		t.Fatalf("cursor after composed grapheme and c = %d, want 2", b.Col)
	}
}
