package widget

import "testing"

func TestEditRegister(t *testing.T) {
	var r editRegister
	if r.holds() || r.yankAllowed() {
		t.Fatal("a zero register holds something or allows yanking")
	}
	r.set("", true) // an empty line, yanked whole
	if !r.holds() {
		t.Fatal("an empty line-wise yank is not pasteable")
	}
	r.set("word", false)
	if text, lw := r.content(); text != "word" || lw {
		t.Fatalf("content %q %v; want word, charwise", text, lw)
	}
	r.yank = true
	if !r.yankAllowed() {
		t.Fatal("yank on is not allowed")
	}
}
