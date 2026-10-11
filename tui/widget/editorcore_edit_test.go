package widget_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func pos(l, c int) widget.TextPos { return widget.TextPos{Line: l, Col: c} }

func rng(l1, c1, l2, c2 int) widget.TextRange {
	return widget.TextRange{From: pos(l1, c1), To: pos(l2, c2)}
}

func undo(c *widget.EditorCore) { c.HandleKey(tui.KeyEvent{Code: 'u', Text: "u"}) }

func TestInsertDeleteReplaceAndTheirPositions(t *testing.T) {
	c, _ := coreWith(t, "alpha\nbeta")
	end, err := c.Insert(pos(0, 5), " one\ntwo")
	if err != nil || c.Value() != "alpha one\ntwo\nbeta" || end != pos(1, 3) {
		t.Fatalf("Insert = %v, %v; text %q", end, err, c.Value())
	}
	if err := c.Delete(rng(1, 3, 0, 5)); err != nil || c.Value() != "alpha\nbeta" {
		t.Fatalf("Delete (a reversed range) = %v; text %q", err, c.Value())
	}
	if got := c.Cursor(); got != pos(0, 4) {
		t.Fatalf("cursor after Delete = %v, want (0,4): where the range began, on a cluster in Normal mode", got)
	}
	end, err = c.Replace(rng(1, 0, 1, 4), "gamma")
	if err != nil || c.Value() != "alpha\ngamma" || end != pos(1, 5) {
		t.Fatalf("Replace = %v, %v; text %q", end, err, c.Value())
	}
	// clusters, not bytes: é (e + combining acute) is one column
	c.SetValue("café au lait")
	if got := c.TextIn(rng(0, 3, 0, 4)); got != "é" {
		t.Fatalf("TextIn of one cluster = %q", got)
	}
	// clamped: past the end is the end
	if got := c.TextIn(rng(0, 8, 7, 99)); got != "lait" {
		t.Fatalf("TextIn past the end = %q, want lait", got)
	}
}

func TestEachEditIsOneUndoAndABatchIsOne(t *testing.T) {
	c, _ := coreWith(t, "a")
	var changes int
	c2 := widget.NewEditorCore(widget.CoreInitialText("a"), widget.CoreOnChange(func() { changes++ }))
	c2.Bind(nil, &fakeLayout{per: 1, page: 1})
	_, _ = c.Insert(pos(0, 1), "b")
	_, _ = c.Insert(pos(0, 2), "c")
	undo(c)
	if c.Value() != "ab" {
		t.Fatalf("one undo after two inserts = %q, want ab", c.Value())
	}
	err := c2.Batch(func() error {
		for _, s := range []string{"b", "c", "d"} {
			if _, err := c2.Insert(pos(0, 99), s); err != nil {
				return err
			}
		}
		return c2.Batch(func() error { _, err := c2.Insert(pos(0, 99), "e"); return err })
	})
	if err != nil || c2.Value() != "abcde" {
		t.Fatalf("Batch = %v; text %q", err, c2.Value())
	}
	if changes != 1 {
		t.Fatalf("a batch (nested) announced %d changes, want 1", changes)
	}
	undo(c2)
	if c2.Value() != "a" {
		t.Fatalf("one undo after a batch = %q, want a", c2.Value())
	}
	// a batch with no edits makes no undo step and no change
	_ = c2.Batch(func() error { return nil })
	if changes != 2 { // the undo announced one
		t.Fatalf("an empty batch announced a change (%d)", changes)
	}
	want := errors.New("stop")
	if err := c2.Batch(func() error { _, _ = c2.Insert(pos(0, 0), "x"); return want }); err != want || c2.Value() != "xa" {
		t.Fatalf("a failing batch = %v; text %q: the edits made stay", err, c2.Value())
	}
}

func TestEditsAreRefusedReadOnly(t *testing.T) {
	c, _ := coreWith(t, "keep", widget.CoreReadOnly(true))
	if _, err := c.Insert(pos(0, 0), "x"); !errors.Is(err, widget.ErrReadOnly) {
		t.Fatalf("Insert on a read-only core = %v", err)
	}
	if err := c.Delete(rng(0, 0, 0, 2)); !errors.Is(err, widget.ErrReadOnly) {
		t.Fatalf("Delete on a read-only core = %v", err)
	}
	if _, err := c.Replace(rng(0, 0, 0, 2), "x"); !errors.Is(err, widget.ErrReadOnly) || c.Value() != "keep" {
		t.Fatalf("Replace on a read-only core = %v; text %q", err, c.Value())
	}
}

func TestReadFromWriteToRoundTrip(t *testing.T) {
	c, _ := coreWith(t, "old")
	_, _ = c.Insert(pos(0, 0), "x")
	n, err := c.ReadFrom(strings.NewReader("one\r\ntwo\rthree\n"))
	if err != nil || n != 15 || c.Value() != "one\ntwo\nthree\n" {
		t.Fatalf("ReadFrom = %d, %v; text %q", n, err, c.Value())
	}
	if c.Modified() || c.CanUndo() {
		t.Fatalf("after ReadFrom: modified %v, can undo %v; want a new, unmodified document", c.Modified(), c.CanUndo())
	}
	var b bytes.Buffer
	if n, err := c.WriteTo(&b); err != nil || n != int64(b.Len()) || b.String() != "one\ntwo\nthree\n" {
		t.Fatalf("WriteTo = %d, %v; wrote %q", n, err, b.String())
	}
	if _, err := c.ReadFrom(failingReader{}); err == nil || c.Value() != "one\ntwo\nthree\n" {
		t.Fatalf("a failed read = %v and left %q; want the document as it was", err, c.Value())
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("disk gone") }

func TestModifiedFollowsEditsUndoAndSave(t *testing.T) {
	c, _ := coreWith(t, "saved")
	if c.Modified() {
		t.Fatal("a new document is modified")
	}
	_, _ = c.Insert(pos(0, 5), "!")
	if !c.Modified() {
		t.Fatal("an edit left the document unmodified")
	}
	undo(c)
	if c.Modified() {
		t.Fatal("undoing back to the saved text left it modified")
	}
	_, _ = c.Insert(pos(0, 5), "?")
	c.MarkSaved()
	undo(c)
	if !c.Modified() {
		t.Fatal("undoing past the save left it unmodified")
	}
	// a fresh edit after undoing never reuses the saved revision
	_, _ = c.Insert(pos(0, 5), "?")
	if !c.Modified() {
		t.Fatal("re-typing the saved text by a new edit reads as saved; revisions must be fresh")
	}
	// typed edits count too
	c.SetValue("k")
	keys(c, "x")
	if !c.Modified() {
		t.Fatal("a typed delete left the document unmodified")
	}
}

func TestSelectShowsTheRangeAsVisual(t *testing.T) {
	c, _ := coreWith(t, "hello world\nnext")
	c.Select(rng(0, 6, 1, 0)) // "world\n"
	r, ok := c.Selection()
	if !ok || c.Mode() != widget.ModeVisual || r != rng(0, 6, 0, 11) {
		t.Fatalf("Selection = %v, %v in %v; want world as Visual", r, ok, c.Mode())
	}
	if got := c.SelectedText(); got != "world" {
		t.Fatalf("SelectedText = %q", got)
	}
	_, _ = c.Insert(pos(1, 0), ">")
	if c.Mode() != widget.ModeNormal || c.Value() != "hello world\n>next" {
		t.Fatalf("an edit over a selection: mode %v, text %q", c.Mode(), c.Value())
	}
	c.Select(rng(0, 0, 0, 5))
	c.Unselect()
	if _, ok := c.Selection(); ok {
		t.Fatal("Unselect left a selection")
	}
}
