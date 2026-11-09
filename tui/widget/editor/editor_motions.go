package editor

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
	txt "github.com/yongjohnlee80/golib/tui/widget/internal/text"
)

// bufferWords is a buffer as the word motions read it.
type bufferWords struct{ b *txt.Buffer }

func (w bufferWords) LineCount() int              { return len(w.b.Lines) }
func (w bufferWords) LineClusters(i int) []string { return w.b.LineClusters(i) }

func (c *EditorCore) walker(big bool) *kit.WordWalker {
	return kit.NewWordWalker(bufferWords{&c.buf}, c.buf.Ln, c.buf.Col, big)
}

// wordForward is w (W with big), as Vim's fwd_word: past the cursor's run, then past blanks to
// the next run's start, stopping on an empty line. At the text's end it rests there.
func (c *EditorCore) wordForward(big bool) (int, int) { return kit.WordForwardFrom(c.walker(big)) }

// wordBack is b (B with big), as Vim's bck_word: back past blanks, stopping on an empty line, then
// to the start of the run there.
func (c *EditorCore) wordBack(big bool) (int, int) { return kit.WordBackFrom(c.walker(big)) }

// wordEnd is e (E with big), as Vim's end_word: one step, then past blanks (empty lines too), then
// to the end of the run there, which is the cursor's own when it goes on.
func (c *EditorCore) wordEnd(big bool) (int, int) { return kit.WordEndFrom(c.walker(big)) }

// wordEndBack is ge (gE with big), as Vim's bckend_word: back past the cursor's run, then past
// blanks to the end of the previous run, stopping on an empty line.
func (c *EditorCore) wordEndBack(big bool) (int, int) { return kit.WordEndBackFrom(c.walker(big)) }

// paraForward/paraBack: next/previous blank-line boundary.
func (c *EditorCore) paraForward(count int) int {
	ln := c.buf.Ln
	for ; count > 0; count-- {
		i := ln + 1
		for i < len(c.buf.Lines) && strings.TrimSpace(c.buf.Lines[i]) != "" {
			i++
		}
		ln = min(i, len(c.buf.Lines)-1)
	}
	return ln
}

func (c *EditorCore) paraBack(count int) int {
	ln := c.buf.Ln
	for ; count > 0; count-- {
		i := ln - 1
		for i > 0 && strings.TrimSpace(c.buf.Lines[i]) != "" {
			i--
		}
		ln = max(i, 0)
	}
	return ln
}

// move applies a motion action count times, extending the selection in
// visual modes.
func (c *EditorCore) move(act Action, count int) {
	c.hist.close()
	// Visual highlights derive from vAnchor + cursor; the buffer's own
	// selection anchor stays nil so insertText never sees a stray region.
	const extend = false
	apply := func(ln, col int) {
		c.buf.MoveCursor(ln, col, extend)
		if c.keys.mode != ModeInsert {
			c.clampNormal()
		}
		c.reveal()
		c.markDirty()
	}
	switch act {
	case ActLeft:
		apply(c.buf.Ln, c.buf.Col-count)
		c.buf.Desired = -1
	case ActRight:
		apply(c.buf.Ln, min(c.buf.Col+count, c.lastCol(c.buf.Ln)))
		c.buf.Desired = -1
	case ActDown, ActUp:
		delta := count
		if act == ActUp {
			delta = -count
		}
		ln, col := c.lineTarget(delta)
		d := c.buf.Desired
		apply(ln, col)
		c.buf.Desired = d
	case ActPageDown, ActPageUp:
		delta := c.pageLines() * count
		if act == ActPageUp {
			delta = -delta
		}
		ln, col := c.lineTarget(delta)
		d := c.buf.Desired
		apply(ln, col)
		c.buf.Desired = d
	case ActLineStart:
		c.buf.Desired = -1
		apply(c.buf.Ln, 0)
	case ActLineEnd:
		c.buf.Desired = -1
		c.oneShot.end = c.oneShot.on // Ctrl-o $: Insert resumes past the end
		apply(c.buf.Ln, c.lastCol(c.buf.Ln))
		if c.keys.mode == ModeVisualBlock {
			c.buf.Desired = blockToEnd // the block runs to every line's end, through j and k
		}
	case ActWordForward, ActWORDForward, ActWordBack, ActWORDBack, ActWordEnd, ActWORDEnd,
		ActWordEndBack, ActWORDEndBack:
		c.buf.Desired = -1
		step, big := c.wordForward, act == ActWORDForward
		switch act {
		case ActWordBack, ActWORDBack:
			step, big = c.wordBack, act == ActWORDBack
		case ActWordEnd, ActWORDEnd:
			step, big = c.wordEnd, act == ActWORDEnd
		case ActWordEndBack, ActWORDEndBack:
			step, big = c.wordEndBack, act == ActWORDEndBack
		}
		for range count {
			ln, col := step(big)
			c.buf.MoveCursor(ln, col, false)
		}
		c.clampNormal()
		c.reveal()
		c.markDirty()
	case ActParaForward:
		c.buf.Desired = -1
		apply(c.paraForward(count), 0)
	case ActParaBack:
		c.buf.Desired = -1
		apply(c.paraBack(count), 0)
	case ActSentenceForward, ActSentenceBack:
		c.buf.Desired = -1
		p := c.sentenceTarget(act == ActSentenceForward, count)
		apply(p.Ln, p.Col)
	case ActLineDownFirst:
		c.toLine(c.buf.Ln + count)
	case ActLineUpFirst:
		c.toLine(c.buf.Ln - count)
	case ActFirstNonBlank:
		c.toLine(c.buf.Ln)
	case ActLeftWrap:
		c.leftWrap(count)
	case ActFindCharRepeat, ActFindCharReverse:
		if f := c.keys.lastFind; f != nil {
			g := *f
			if act == ActFindCharReverse {
				g.forward = !g.forward
			}
			c.findChar(g, count, true)
		}
	case ActViewTop, ActViewMiddle, ActViewBottom:
		c.viewLine(act, count)
	}
}

// goToLine is the shared gg/G target motion: an EXPLICIT count means
// "line count" (1-based, clamped); without one, gg goes to the top and G
// to the bottom.
func (c *EditorCore) goToLine(hadCount bool, count int, bottom bool) {
	c.setJumpMark()
	c.buf.Desired = -1
	ln := 0
	switch {
	case hadCount:
		ln = min(count-1, len(c.buf.Lines)-1)
	case bottom:
		ln = len(c.buf.Lines) - 1
	}
	c.buf.MoveCursor(ln, 0, false)
	c.clampNormal()
	c.reveal()
	c.markDirty()
}
