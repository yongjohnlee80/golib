package mermaid

import (
	"errors"
	"strconv"
	"strings"
)

// GitGraph is the gitGraph kind's value, declared here and registered in init (block.go holds 15).
const GitGraph Kind = 16

func init() { register(GitGraph, "gitgraph", []string{"gitGraph"}, (*parser).gitGraph) }

// GitGraphDiagram is a parsed gitGraph: its direction, its branches in the order they were made
// (Branches[0] is main), and its commits in source order, which is the graph's time axis.
type GitGraphDiagram struct {
	Dir      Dir // LR (the default), TB or BT
	Branches []GitBranch
	Commits  []GitCommit
}

func (*GitGraphDiagram) Kind() Kind { return GitGraph }

// GitBranch is one branch: its name, and its order (a lane's place, lowest nearest main; -1 when
// not given, when it takes its place by creation).
type GitBranch struct {
	Name  string
	Order int
	Span  [2]int // its branch statement; main's is zero
}

// GitCommitType is a commit's look, as Mermaid's type: attribute names it.
type GitCommitType uint8

const (
	GitNormal    GitCommitType = iota // a filled dot
	GitReverse                        // a crossed dot: a commit that reverts
	GitHighlight                      // a filled square
)

// GitCommit is one commit: on a branch, after its parents, perhaps tagged. A merge has two parents
// (its branch's head, then the merged branch's); a cherry-pick copies another commit (Picked).
type GitCommit struct {
	ID      string // as written; "" when none was given (Mermaid generates one, and draws it only on request)
	Branch  int    // into Branches
	Parents []int  // into Commits: the branch's previous head first; none for the first commit
	Type    GitCommitType
	Tag     string
	Merge   bool // a merge commit: Parents[1] is the merged branch's head
	Picked  int  // a cherry-pick's source commit, into Commits; -1 otherwise
	Span    [2]int
}

// gitGraphParser is the parse of one gitGraph: the diagram, the branch checked out, each branch's
// head (-1 before its first commit), and the commits' and branches' indexes by name.
type gitGraphParser struct {
	p        *parser
	d        *GitGraphDiagram
	current  int
	heads    []int
	branches map[string]int
	ids      map[string]int
}

// gitGraph parses a gitGraph's body: rest is its header line after the keyword, at restOff. The
// header may give a direction, `LR:`, `TB:` or `BT:`. Each other line is one statement: commit,
// branch, checkout (or switch), merge or cherry-pick, each with its attributes (`key: value`, a
// value quoted or one word).
func (p *parser) gitGraph(rest string, restOff int) (Diagram, error) {
	g := &gitGraphParser{p: p, d: &GitGraphDiagram{Dir: LR}, branches: map[string]int{}, ids: map[string]int{}}
	g.d.Branches = []GitBranch{{Name: "main", Order: -1}}
	g.heads = []int{-1}
	g.branches["main"] = 0
	if s := strings.TrimSpace(rest); s != "" {
		off := restOff + strings.Index(rest, s)
		switch s {
		case "LR:":
			g.d.Dir = LR
		case "TB:":
			g.d.Dir = TB
		case "BT:":
			g.d.Dir = BT
		default:
			p.fail(off, "unexpected %q after gitGraph (want LR:, TB: or BT:)", s)
		}
	}
	for _, st := range timelineLines(p.src, restOff+len(rest)) {
		g.statement(st)
		if errors.Is(p.unsupported, ErrTooLarge) {
			return nil, p.unsupported
		}
	}
	return p.done(g.d)
}

// statement is one line.
func (g *gitGraphParser) statement(st stmt) {
	w, rest := firstWord(st.text)
	restOff := st.off + len(st.text) - len(rest)
	switch w {
	case "commit":
		g.commit(st, rest, restOff)
	case "branch":
		g.branch(st, rest, restOff)
	case "checkout", "switch":
		g.checkout(st, rest, restOff)
	case "merge":
		g.merge(st, rest, restOff)
	case "cherry-pick":
		g.cherryPick(st, rest, restOff)
	case "accTitle:", "accDescr:":
		// accessibility text: nothing drawn
	case "accTitle", "accDescr":
		if !strings.HasPrefix(rest, ":") {
			g.p.unsupport(st.off, w+" block")
		}
	default:
		g.p.fail(st.off, "unknown gitGraph statement %q", w)
	}
}

// gitAttrs are a statement's attributes, by key, each value unquoted, with where it starts.
type gitAttrs map[string]gitAttr

type gitAttr struct {
	value string
	off   int
}

// attrs reads `key: value` pairs from s (at off): a value quoted, or one word. keys are the ones the
// statement takes; any other key is outside the subset (msg, Mermaid's old commit message, too).
func (g *gitGraphParser) attrs(s string, off int, keys ...string) (gitAttrs, bool) {
	out := gitAttrs{}
	for {
		lead := len(s) - len(strings.TrimLeft(s, " \t"))
		s, off = s[lead:], off+lead
		if s == "" {
			return out, true
		}
		colon := strings.IndexByte(s, ':')
		if colon <= 0 {
			g.p.fail(off, "a gitGraph attribute wants key: value, not %q", s)
			return nil, false
		}
		key := strings.TrimSpace(s[:colon])
		if !containsString(keys, key) {
			g.p.unsupport(off, "a gitGraph "+key+" attribute")
			return nil, false
		}
		if _, dup := out[key]; dup {
			g.p.fail(off, "the attribute %s is given twice", key)
			return nil, false
		}
		v := s[colon+1:]
		vlead := len(v) - len(strings.TrimLeft(v, " \t"))
		v, voff := v[vlead:], off+colon+1+vlead
		var raw string
		if strings.HasPrefix(v, `"`) {
			end := strings.IndexByte(v[1:], '"')
			if end < 0 {
				g.p.fail(voff, "the attribute %s's value is not closed", key)
				return nil, false
			}
			raw = v[:end+2]
			text, err := g.p.label(raw, voff)
			if err != nil {
				return nil, false
			}
			out[key] = gitAttr{value: text, off: voff}
		} else {
			raw, _ = firstWord(v)
			out[key] = gitAttr{value: raw, off: voff}
		}
		s, off = v[len(raw):], voff+len(raw)
	}
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// name reads a branch name: one word, or a quoted string.
func (g *gitGraphParser) name(s string, off int) (string, string, int, bool) {
	if strings.HasPrefix(s, `"`) {
		end := strings.IndexByte(s[1:], '"')
		if end < 0 {
			g.p.fail(off, "a branch name is not closed")
			return "", "", 0, false
		}
		text, err := g.p.label(s[:end+2], off)
		if err != nil {
			return "", "", 0, false
		}
		return text, s[end+2:], off + end + 2, true
	}
	w, rest := firstWord(s)
	if w == "" {
		g.p.fail(off, "a branch name is missing")
		return "", "", 0, false
	}
	return w, rest, off + len(s) - len(rest), true
}

// commitType reads a type: attribute.
func (g *gitGraphParser) commitType(a gitAttrs) (GitCommitType, bool) {
	t, ok := a["type"]
	if !ok {
		return GitNormal, true
	}
	switch t.value {
	case "NORMAL":
		return GitNormal, true
	case "REVERSE":
		return GitReverse, true
	case "HIGHLIGHT":
		return GitHighlight, true
	}
	g.p.fail(t.off, "unknown commit type %q (want NORMAL, REVERSE or HIGHLIGHT)", t.value)
	return GitNormal, false
}

// add appends c on the current branch, with id and tag from a, and moves the branch's head to it.
func (g *gitGraphParser) add(c GitCommit, a gitAttrs) bool {
	if id, ok := a["id"]; ok {
		if _, dup := g.ids[id.value]; dup {
			g.p.fail(id.off, "the commit id %q is given twice", id.value)
			return false
		}
		c.ID = id.value
	}
	if tag, ok := a["tag"]; ok {
		c.Tag = tag.value
	}
	c.Branch = g.current
	if h := g.heads[g.current]; h >= 0 {
		c.Parents = append([]int{h}, c.Parents...)
	}
	i := len(g.d.Commits)
	g.d.Commits = append(g.d.Commits, c)
	if c.ID != "" {
		g.ids[c.ID] = i
	}
	g.heads[g.current] = i
	return true
}

func (g *gitGraphParser) commit(st stmt, rest string, restOff int) {
	a, ok := g.attrs(rest, restOff, "id", "tag", "type")
	if !ok {
		return
	}
	t, ok := g.commitType(a)
	if !ok {
		return
	}
	g.add(GitCommit{Type: t, Picked: -1, Span: [2]int{st.off, st.off + len(st.text)}}, a)
}

func (g *gitGraphParser) branch(st stmt, rest string, restOff int) {
	name, tail, tailOff, ok := g.name(rest, restOff)
	if !ok {
		return
	}
	if _, dup := g.branches[name]; dup {
		g.p.fail(restOff, "the branch %q already exists", name)
		return
	}
	a, ok := g.attrs(tail, tailOff, "order")
	if !ok {
		return
	}
	order := -1
	if o, ok := a["order"]; ok {
		n, err := strconv.Atoi(o.value)
		if err != nil || n < 0 {
			g.p.fail(o.off, "a branch's order %q is not a whole number", o.value)
			return
		}
		order = n
	}
	// a branch starts at the current branch's head: its first commit's parent
	g.branches[name] = len(g.d.Branches)
	g.d.Branches = append(g.d.Branches, GitBranch{Name: name, Order: order, Span: [2]int{st.off, st.off + len(st.text)}})
	g.heads = append(g.heads, g.heads[g.current])
	g.current = len(g.d.Branches) - 1
}

func (g *gitGraphParser) checkout(_ stmt, rest string, restOff int) {
	name, tail, tailOff, ok := g.name(rest, restOff)
	if !ok {
		return
	}
	if strings.TrimSpace(tail) != "" {
		g.p.fail(tailOff, "unexpected %q after the branch name", strings.TrimSpace(tail))
		return
	}
	b, found := g.branches[name]
	if !found {
		g.p.fail(restOff, "the branch %q does not exist", name)
		return
	}
	g.current = b
}

func (g *gitGraphParser) merge(st stmt, rest string, restOff int) {
	name, tail, tailOff, ok := g.name(rest, restOff)
	if !ok {
		return
	}
	b, found := g.branches[name]
	switch {
	case !found:
		g.p.fail(restOff, "the branch %q does not exist", name)
		return
	case b == g.current:
		g.p.fail(restOff, "a branch can't be merged into itself (%q)", name)
		return
	case g.heads[b] < 0:
		g.p.fail(restOff, "the branch %q has no commits to merge", name)
		return
	case g.heads[b] == g.heads[g.current]:
		g.p.fail(restOff, "the branch %q has nothing the current branch hasn't", name)
		return
	}
	a, ok := g.attrs(tail, tailOff, "id", "tag", "type")
	if !ok {
		return
	}
	t, ok := g.commitType(a)
	if !ok {
		return
	}
	g.add(GitCommit{Type: t, Merge: true, Parents: []int{g.heads[b]}, Picked: -1, Span: [2]int{st.off, st.off + len(st.text)}}, a)
}

func (g *gitGraphParser) cherryPick(st stmt, rest string, restOff int) {
	a, ok := g.attrs(rest, restOff, "id", "tag", "parent")
	if !ok {
		return
	}
	id, ok := a["id"]
	if !ok {
		g.p.fail(restOff, "a cherry-pick wants the id of the commit it copies")
		return
	}
	src, found := g.ids[id.value]
	switch {
	case !found:
		g.p.fail(id.off, "no commit has the id %q", id.value)
		return
	case g.d.Commits[src].Branch == g.current:
		g.p.fail(id.off, "the commit %q is on the current branch already", id.value)
		return
	case g.d.Commits[src].Merge:
		g.p.unsupport(id.off, "a cherry-picked merge commit")
		return
	}
	if _, ok := a["parent"]; ok {
		g.p.unsupport(a["parent"].off, "a cherry-pick's parent")
		return
	}
	// the copy's own id is generated, as Mermaid's: the id attribute names its source
	delete(a, "id")
	g.add(GitCommit{Picked: src, Span: [2]int{st.off, st.off + len(st.text)}}, a)
}
