package widget

import (
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// revFeed types s into c: plain text as keys, and <esc> <cr> <redo> (Ctrl-r) and <paste:TEXT> (\n
// for a line break) as those.
func revFeed(c *EditorCore, s string) {
	for len(s) > 0 {
		if s[0] != '<' {
			c.HandleKey(tui.KeyEvent{Code: rune(s[0]), Text: s[:1]})
			s = s[1:]
			continue
		}
		end := strings.IndexByte(s, '>')
		tok := s[:end+1]
		switch {
		case tok == "<esc>":
			c.HandleKey(tui.KeyEvent{Code: tui.KeyEscape})
		case tok == "<cr>":
			c.HandleKey(tui.KeyEvent{Code: tui.KeyEnter})
		case tok == "<redo>":
			c.HandleKey(tui.KeyEvent{Code: 'r', Mods: tui.ModCtrl})
		case strings.HasPrefix(tok, "<paste:"):
			c.HandlePaste(strings.ReplaceAll(tok[len("<paste:"):end], `\n`, "\n"))
		}
		s = s[end+1:]
	}
}

// revSteps are the edits, moves, undos and redos a random session is made of. Each is one step;
// {w} is a random word, {c} a random letter.
var revSteps = []string{
	"x", "dd", "2dd", "p", "P", "J", "yyp", "D", "~", "r{c}", "cw{w}<esc>", "o{w}<esc>",
	"O{w}<cr>{w}<esc>", "i{w}<esc>", "A {w}<esc>", "i<paste:{w}\\n{w}\\n>{w}<esc>", "Vjd", "ggdG",
	".", ".", "u", "u", "u", "<redo>", "<redo>", "j", "k", "l", "h", "G", "gg", "w", "$", "0",
	"api", // the API's own edits: Insert and Replace
}

// TestUndoRestoresEachRevisionsText: an editor's revision names its text, and undo and redo restore
// both together, so whenever the editor is at a revision it has been at before, its text must be
// the text it had then. Seeded random sessions of a few hundred steps (edits of every kind, '.',
// undo and redo between them, the API's edits) check it at every change and after every step: the
// interleavings no scripted case thought of.
//
// The test reads only the revision, the text and the keys, so it holds for any history that keeps
// whole snapshots of the text, the reference this one must match.
func TestUndoRestoresEachRevisionsText(t *testing.T) {
	words := []string{"alpha", "beta", "", "gamma delta", "x"}
	for seed := range 24 {
		r := rand.New(rand.NewPCG(uint64(seed), 0x5eed))
		texts := map[uint64]string{}
		var c *EditorCore
		var step string
		check := func(when string) {
			if want, ok := texts[c.rev]; ok && want != c.Value() {
				t.Fatalf("seed %d, %s %q: at revision %d the text is\n%q\nbut it was\n%q",
					seed, when, step, c.rev, c.Value(), want)
			}
			texts[c.rev] = c.Value()
		}
		c = NewEditorCore(CoreModal(true), CoreOnChange(func() { check("a change in") }),
			CoreInitialText("one two\nthree\n\nfour five six\nseven"))
		check("the start")
		undos := 0
		for range 300 {
			step = revSteps[r.IntN(len(revSteps))]
			if step == "api" {
				ln := r.IntN(len(c.buf.textLines))
				if r.IntN(2) == 0 {
					_, _ = c.Insert(TextPos{Line: ln}, words[r.IntN(len(words))]+"\n")
				} else {
					_, _ = c.Replace(TextRange{From: TextPos{Line: ln}, To: TextPos{Line: ln, Col: 1}}, "Z")
				}
				check("after")
				continue
			}
			if step == "u" {
				undos++
			}
			s := strings.ReplaceAll(step, "{w}", words[r.IntN(len(words))])
			s = strings.ReplaceAll(s, "{c}", string(rune('a'+r.IntN(26))))
			revFeed(c, s)
			check("after")
		}
		if undos == 0 {
			t.Fatalf("seed %d: a session with no undo checks nothing", seed)
		}
	}
}
