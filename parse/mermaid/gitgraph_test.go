package mermaid_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

func gitParse(t *testing.T, src string) *mermaid.GitGraphDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	g, ok := d.(*mermaid.GitGraphDiagram)
	if !ok || d.Kind() != mermaid.GitGraph {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return g
}

// Mermaid's documented example: commits on main, a branch made and checked out, commits there, a
// merge back into main. Each commit is on its branch, after its branch's head; the branch starts at
// main's head when it was made, and the merge has both heads for parents.
func TestGitGraphTheDocumentedExample(t *testing.T) {
	g := gitParse(t, `gitGraph
    commit
    commit
    branch develop
    checkout develop
    commit
    commit
    checkout main
    merge develop
    commit
    commit`)
	if g.Dir != mermaid.LR {
		t.Errorf("dir %v, want LR", g.Dir)
	}
	names := []string{}
	for _, b := range g.Branches {
		names = append(names, b.Name)
	}
	if !reflect.DeepEqual(names, []string{"main", "develop"}) {
		t.Fatalf("branches %v", names)
	}
	type c struct {
		branch  int
		parents []int
		merge   bool
	}
	want := []c{
		{0, nil, false},
		{0, []int{0}, false},
		{1, []int{1}, false}, // develop starts at main's head
		{1, []int{2}, false},
		{0, []int{1, 3}, true}, // main's head, then develop's
		{0, []int{4}, false},
		{0, []int{5}, false},
	}
	if len(g.Commits) != len(want) {
		t.Fatalf("%d commits, want %d", len(g.Commits), len(want))
	}
	for i, w := range want {
		got := g.Commits[i]
		if got.Branch != w.branch || !reflect.DeepEqual(got.Parents, w.parents) || got.Merge != w.merge || got.Picked != -1 {
			t.Errorf("commit %d: %+v, want branch %d parents %v merge %v", i, got, w.branch, w.parents, w.merge)
		}
	}
}

// A commit's attributes: an id, a tag and a type, in any order, a value quoted or a word; a merge
// takes them too; a branch its order; switch is checkout.
func TestGitGraphAttributes(t *testing.T) {
	g := gitParse(t, `gitGraph TB:
    commit id: "Alpha" tag: "v1.0"
    commit type: HIGHLIGHT
    branch "feature x" order: 2
    switch "feature x"
    commit type: REVERSE id: "Beta"
    checkout main
    merge "feature x" tag: "v2.0" type: HIGHLIGHT`)
	if g.Dir != mermaid.TB {
		t.Errorf("dir %v, want TB", g.Dir)
	}
	if b := g.Branches[1]; b.Name != "feature x" || b.Order != 2 {
		t.Errorf("branch %+v", b)
	}
	if g.Branches[0].Order != -1 {
		t.Errorf("main's order %d, want -1 (not given)", g.Branches[0].Order)
	}
	c := g.Commits
	if c[0].ID != "Alpha" || c[0].Tag != "v1.0" || c[0].Type != mermaid.GitNormal {
		t.Errorf("commit 0 %+v", c[0])
	}
	if c[1].ID != "" || c[1].Type != mermaid.GitHighlight {
		t.Errorf("commit 1 %+v: want no id, HIGHLIGHT", c[1])
	}
	if c[2].ID != "Beta" || c[2].Type != mermaid.GitReverse || c[2].Branch != 1 {
		t.Errorf("commit 2 %+v", c[2])
	}
	if m := c[3]; !m.Merge || m.Tag != "v2.0" || m.Type != mermaid.GitHighlight || !reflect.DeepEqual(m.Parents, []int{1, 2}) {
		t.Errorf("merge %+v", m)
	}
	if g := gitParse(t, "gitGraph BT:\n  commit"); g.Dir != mermaid.BT {
		t.Errorf("BT: %v", g.Dir)
	}
}

// A cherry-pick copies a commit of another branch onto the current one: its source by id, a tag
// of its own, its own id generated.
func TestGitGraphCherryPick(t *testing.T) {
	g := gitParse(t, `gitGraph
    commit id: "A"
    branch develop
    commit id: "B"
    checkout main
    commit
    cherry-pick id: "B" tag: "picked"`)
	p := g.Commits[3]
	if p.Picked != 1 || p.Branch != 0 || p.Tag != "picked" || p.ID != "" || !reflect.DeepEqual(p.Parents, []int{2}) {
		t.Errorf("cherry-pick %+v", p)
	}
}

// Each misuse Mermaid itself refuses is a SyntaxError on its line: a branch made twice, a checkout or
// a merge of a branch that doesn't exist, a merge into itself, a merge of a branch with no commits or
// with nothing new, a commit id given twice, a cherry-pick of an id no commit has or of the current
// branch's own, an unknown statement or commit type, a bad order, and an unknown header.
func TestGitGraphSyntaxErrors(t *testing.T) {
	for _, c := range []struct{ src, msg string }{
		{"gitGraph\n  branch a\n  branch a", "already exists"},
		{"gitGraph\n  checkout nope", "does not exist"},
		{"gitGraph\n  commit\n  merge nope", "does not exist"},
		{"gitGraph\n  commit\n  merge main", "into itself"},
		{"gitGraph\n  commit\n  branch a\n  checkout main\n  merge a", "nothing the current branch hasn't"},
		{"gitGraph\n  branch a\n  checkout main\n  merge a", "no commits"},
		{"gitGraph\n  commit id: \"x\"\n  commit id: \"x\"", "given twice"},
		{"gitGraph\n  commit\n  cherry-pick id: \"zz\"", "no commit has"},
		{"gitGraph\n  commit id: \"x\"\n  cherry-pick id: \"x\"", "current branch already"},
		{"gitGraph\n  commit\n  cherry-pick", "wants the id"},
		{"gitGraph\n  rebase main", "unknown gitGraph statement"},
		{"gitGraph\n  commit type: BOLD", "unknown commit type"},
		{"gitGraph\n  branch a order: x", "not a whole number"},
		{"gitGraph RL:\n  commit", "after gitGraph"},
		{"gitGraph\n  commit id \"x\"", "key: value"},
		{"gitGraph\n  commit id: \"x", "not closed"},
		{"gitGraph\n  branch dev\n  commit id: \"A\"\n  checkout main\n  merge dev", "no commits to merge \"dev\" into"},
		{"gitGraph\n  branch dev\n  commit id: \"A\"\n  checkout main\n  cherry-pick id: \"A\"", "no commits to cherry-pick onto"},
	} {
		_, err := mermaid.Parse(c.src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || !strings.Contains(se.Msg, c.msg) {
			t.Errorf("%q: %v; want a SyntaxError saying %q", c.src, err, c.msg)
		}
	}
}

// What Mermaid draws but this subset doesn't is ErrUnsupported on its line, never drawn half: the
// old msg attribute, a cherry-pick's parent, a cherry-picked merge, an accessibility block.
func TestGitGraphUnsupported(t *testing.T) {
	for _, src := range []string{
		"gitGraph\n  commit msg: \"hi\"",
		"gitGraph\n  commit id: \"A\"\n  branch b\n  commit id: \"B\"\n  checkout main\n  commit\n  cherry-pick id: \"B\" parent: \"A\"",
		"gitGraph\n  commit\n  branch b\n  commit\n  checkout main\n  commit\n  merge b id: \"M\"\n  checkout b\n  cherry-pick id: \"M\"",
		"gitGraph\n  accDescr {\n  text\n  }",
	} {
		if _, err := mermaid.Parse(src); !errors.Is(err, mermaid.ErrUnsupported) {
			t.Errorf("%q: %v; want ErrUnsupported", src, err)
		}
	}
}

// Each commit's span is its statement's, and a branch's its own.
func TestGitGraphSpans(t *testing.T) {
	src := "gitGraph\n  commit id: \"A\"\n  branch b\n"
	g := gitParse(t, src)
	if s := g.Commits[0].Span; src[s[0]:s[1]] != `commit id: "A"` {
		t.Errorf("commit span %q", src[s[0]:s[1]])
	}
	if s := g.Branches[1].Span; src[s[0]:s[1]] != "branch b" {
		t.Errorf("branch span %q", src[s[0]:s[1]])
	}
}

// A bare name, id or tag is held to the label limit as a quoted one is: past it, ErrTooLarge.
func TestGitGraphBareValuesAreLimited(t *testing.T) {
	long := strings.Repeat("x", mermaid.MaxLabel+1)
	for _, src := range []string{
		"gitGraph\n  branch " + long,
		"gitGraph\n  commit id: " + long,
		"gitGraph\n  commit tag: " + long,
	} {
		if _, err := mermaid.Parse(src); !errors.Is(err, mermaid.ErrTooLarge) {
			t.Errorf("a bare value of %d bytes: %v, want ErrTooLarge", len(long), err)
		}
	}
}
