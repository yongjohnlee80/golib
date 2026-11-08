package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
	txt "github.com/yongjohnlee80/golib/tui/widget/internal/text"
)

// VIM REPLACE MODE — R: typed clusters overwrite the text, as Vim's Replace mode has them (its
// replace stack, src/edit.c replace_push, replace_pop and replace_do_bs). Each typed cluster pushes
// what it overwrote, or that it appended past the line's end; Backspace pops: the cluster comes
// back, an appended one goes, a line break typed is joined again. With the stack empty, before
// where this Replace began, Backspace only moves left. Enter breaks the line and overwrites
// nothing. Ctrl-w and Ctrl-u are Backspace over the word or back to the indent, so they restore
// too. An arrow key starts a new stack, as it starts a new Insert step. A count types the text
// again, overwriting, as many times more on Esc (3Rab on 123456789 gives ababab789).
//
// Replace is Insert underneath, with this state: its keys, the escape chord, undo, . and Ctrl-o
// are Insert's. Mode reports ModeReplace while it lasts.

// replaceKind is what a typed cluster did.
type replaceKind uint8

const (
	replOverwrote replaceKind = iota // took the place of orig
	replAppended                     // went past the line's end
	replBreak                        // a line break
)

type replaceEntry struct {
	kind replaceKind
	orig string
}

// replaceState is a Replace session's.
type replaceState struct {
	stack     []replaceEntry
	ln, col   int // where it began
	count     int
	keys      []replaceKeyed // what was typed, for the count to type again
	replaying bool
}

// replaceKeyed is one thing typed in Replace: text (a line break as "\n"), or a Backspace.
type replaceKeyed struct {
	text string
	back bool
}

// enterReplace is R, count times.
func (c *EditorCore) enterReplace(count int) {
	b := &c.buf
	c.replace = &replaceState{ln: b.Ln, col: b.Col, count: max(count, 1)}
	c.enterInsert()
	c.reveal()
	c.markDirty()
}

// replaceText types s over the text: a cluster each, a line break inserted.
func (c *EditorCore) replaceText(s string) {
	b := &c.buf
	r := c.replace
	if !r.replaying {
		r.keys = append(r.keys, replaceKeyed{text: s})
	}
	c.beginGroup()
	for _, cl := range txt.Clusters(strings.ReplaceAll(s, "\r\n", "\n")) {
		if cl == "\n" {
			b.InsertText("\n")
			r.stack = append(r.stack, replaceEntry{kind: replBreak})
			continue
		}
		cs := b.LineClusters(b.Ln)
		if b.Col >= len(cs) {
			b.InsertText(cl)
			r.stack = append(r.stack, replaceEntry{kind: replAppended})
			continue
		}
		r.stack = append(r.stack, replaceEntry{kind: replOverwrote, orig: cs[b.Col]})
		b.SetLine(b.Ln, strings.Join(cs[:b.Col], "")+cl+strings.Join(cs[b.Col+1:], ""))
		b.Col++
	}
	c.edited()
}

// replaceBackspace pops the replace stack: what the last cluster typed overwrote comes back, an
// appended one goes, a break is joined; with none, the cursor only moves left.
func (c *EditorCore) replaceBackspace() {
	b := &c.buf
	r := c.replace
	if !r.replaying {
		r.keys = append(r.keys, replaceKeyed{back: true})
	}
	if len(r.stack) == 0 {
		if b.Col > 0 {
			b.Col--
			b.Desired = -1
			c.markDirty()
		}
		return
	}
	e := r.stack[len(r.stack)-1]
	r.stack = r.stack[:len(r.stack)-1]
	c.beginGroup()
	switch e.kind {
	case replBreak:
		if b.Col == 0 && b.Ln > 0 {
			b.DeleteRegion(txt.Pos{Ln: b.Ln - 1, Col: len(b.LineClusters(b.Ln - 1))}, txt.Pos{Ln: b.Ln, Col: 0})
		}
	case replAppended:
		if b.Col > 0 {
			b.DeleteRegion(txt.Pos{Ln: b.Ln, Col: b.Col - 1}, txt.Pos{Ln: b.Ln, Col: b.Col})
		}
	default:
		if cs := b.LineClusters(b.Ln); b.Col > 0 && b.Col <= len(cs) {
			b.SetLine(b.Ln, strings.Join(cs[:b.Col-1], "")+e.orig+strings.Join(cs[b.Col:], ""))
			b.Col--
		}
	}
	c.edited()
}

// replaceRestart starts a new stack where the cursor is (an arrow key moved it): Backspace no
// longer goes back past here, and the count is spent.
func (c *EditorCore) replaceRestart() {
	if r := c.replace; r != nil {
		r.stack, r.keys, r.ln, r.col, r.count = nil, nil, c.buf.Ln, c.buf.Col, 1
	}
}

// replaceDone is Esc out of Replace: what was typed, typed again count-1 times, as Vim repeats the
// inserted text: line breaks and Backspaces too (3Ra<CR>b gives a, ba, ba, b).
func (c *EditorCore) replaceDone() {
	r := c.replace
	if r == nil || r.count <= 1 {
		return
	}
	r.replaying = true
	for range r.count - 1 {
		for _, k := range r.keys {
			if k.back {
				c.replaceBackspace()
			} else {
				c.replaceText(k.text)
			}
		}
	}
}

// replaceKey is Replace's own keys: Enter breaks the line, Backspace pops the stack, Tab
// overwrites as typed text does; an arrow key starts a new stack, and Insert's handling moves.
func (c *EditorCore) replaceKey(k tui.KeyEvent, r insertKey) bool {
	if r.qual&kit.NonTextMods != 0 {
		return false
	}
	switch r.act {
	case ActNewline:
		c.dotInsertKey(k)
		c.replaceText("\n")
		return true
	case ActBackspace:
		c.dotInsertKey(k)
		c.replaceBackspace()
		return true
	case ActInsertTab:
		c.dotInsertKey(k)
		c.replaceText("\t")
		return true
	case ActLeft, ActRight, ActUp, ActDown, ActLineStart, ActLineEnd, ActPageUp, ActPageDown:
		c.replaceRestart()
	}
	return false
}
