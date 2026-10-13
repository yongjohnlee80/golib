package widget

import "strings"

// VIM * AND # — the word under the cursor, found again: * on, # back, g* and g# without word
// boundaries (Vim's nv_ident, src/normal.c). The word is the keyword under or after the cursor on
// its line (find_ident_at_pos), else the first run of non-blanks there. The cursor goes to the
// word's start, a jump, then to the count'th match on or back, wrapping; the match is the one find
// n and N repeat (SetFind), with WholeWord for * and #.
//
// A host that keeps its own find (SetFindHost) takes the request instead, so there is one find
// state: the host's. Case is matched, as Vim's * with 'ignorecase' off; a host's own find may fold
// it, as its 'ignorecase'.

// FindRequest is a find the editor's keys ask a host for (* # g* g#): Term found again from the
// cursor, which is on the term's start, Count times, back or on, wrapping.
type FindRequest struct {
	Term     string
	Options  FindOptions // WholeWord for * and #
	Backward bool
	Count    int
}

// SetFindHost hands * and # to a host that keeps its own find. host reports whether it took the
// request; on false, or with no host, the core's find takes it (SetFind, then n's step).
func (c *EditorCore) SetFindHost(host func(FindRequest) bool) { c.findHost = host }

// identUnderCursor is Vim's find_ident_at_pos with FIND_IDENT|FIND_STRING: the keyword under the
// cursor or the first after it on the line, else the first non-blank run there. Backing up to its
// start stops at any class change; the keyword's end is where its class changes, a run's at a
// blank. ok is false on a line with neither.
func (c *EditorCore) identUnderCursor() (word string, at int, ok bool) {
	cs := c.buf.lineClusters(c.buf.ln)
	class := func(i int) int { return wordClass(cs[i], false) }
	for pass := 0; pass < 2; pass++ { // 0: a keyword; 1: any non-blank text
		col := min(c.buf.col, len(cs))
		for col < len(cs) {
			if k := class(col); k != 0 && (pass == 1 || k != 1) {
				break
			}
			col++
		}
		if col == len(cs) {
			continue
		}
		k := class(col)
		for col > 0 && class(col-1) == k {
			col--
		}
		end := col
		for end < len(cs) && (pass == 0 && class(end) == k || pass == 1 && class(end) != 0) {
			end++
		}
		return strings.Join(cs[col:end], ""), col, true
	}
	return "", 0, false
}

// starSearch is * (back false) and # (back true); g* and g# with whole false.
func (c *EditorCore) starSearch(back, whole bool, count int) {
	word, at, ok := c.identUnderCursor()
	if !ok {
		return // Vim's "no string under cursor"
	}
	c.hist.close()
	c.setJumpMark()
	c.buf.col, c.buf.desired = at, -1
	req := FindRequest{Term: word, Options: FindOptions{WholeWord: whole}, Backward: back, Count: max(count, 1)}
	if c.findHost != nil && c.findHost(req) {
		return
	}
	c.SetFind(req.Term, req.Options)
	for range req.Count {
		c.FindNext(back)
	}
	c.reveal()
	c.markDirty()
}
