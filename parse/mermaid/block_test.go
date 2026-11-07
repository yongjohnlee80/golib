package mermaid_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

// Mermaid's documented block diagrams parse as their goldens (testdata/block, -update writes them):
// a grid's columns and its items in order, each block's id, label, shape and width, a space, a
// group's own grid, and the links between blocks.
func TestBlockCorpus(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "block", "*.mmd"))
	if err != nil || len(files) < 4 {
		t.Fatalf("the corpus: %d files, %v", len(files), err)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		d, err := mermaid.Parse(string(src))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if d.Kind() != mermaid.Block {
			t.Errorf("%s: kind %v", f, d.Kind())
		}
		got, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		golden := strings.TrimSuffix(f, ".mmd") + ".json"
		if *update {
			if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%v (run with -update to write it)", err)
		}
		if string(want) != string(got)+"\n" {
			t.Errorf("%s differs from its golden:\n%s", f, got)
		}
	}
}

func blockParse(t *testing.T, src string) *mermaid.BlockDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	b, ok := d.(*mermaid.BlockDiagram)
	if !ok {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return b
}

// A row's items: ids, shapes with their labels, widths, spaces; a group nested in a group; links by
// id, with their label and arrowheads.
func TestBlockItemsGroupsAndLinks(t *testing.T) {
	b := blockParse(t, "block-beta\n  columns 2\n  a[\"A, wide\"]:2 space\n  block:g:2\n    x(\"round\")\n  end\n  a --> x\n  x -- \"back\" --- a\n")
	if b.Root.Columns != 2 || len(b.Root.Items) != 3 {
		t.Fatalf("root %+v", b.Root)
	}
	a, sp, g := b.Root.Items[0], b.Root.Items[1], b.Root.Items[2]
	if a.ID != "a" || a.Label != "A, wide" || a.Width != 2 || a.Shape != mermaid.Rect {
		t.Errorf("a %+v", a)
	}
	if !sp.Space || sp.Width != 1 {
		t.Errorf("space %+v", sp)
	}
	if g.ID != "g" || g.Width != 2 || g.Group == nil || len(g.Group.Items) != 1 || g.Group.Items[0].Shape != mermaid.Round {
		t.Errorf("group %+v", g)
	}
	if len(b.Edges) != 2 || b.Edges[0].From != "a" || b.Edges[0].To != "x" || b.Edges[0].Head != mermaid.Head ||
		b.Edges[1].Label != "back" || b.Edges[1].Head != mermaid.None {
		t.Errorf("edges %+v", b.Edges)
	}
}

// What a block diagram refuses: a link to no block, a block defined twice, a width not above 0, an
// end with no group open, a group left open, and columns that aren't a number; a block arrow is a
// construct it doesn't read (ErrUnsupported, the host's fallback).
func TestBlockRefusals(t *testing.T) {
	for src, want := range map[string]string{
		"block-beta\n  a\n  a --> b\n":        `a link to "b", which is no block`,
		"block-beta\n  a a\n":                 `block "a" is defined twice`,
		"block-beta\n  a:0\n":                 "a block's width wants a number above 0",
		"block-beta\n  end\n":                 "end with no block group open",
		"block-beta\n  block:g\n    a\n":      "a block group is not closed",
		"block-beta\n  columns two\n":         `columns wants a number above 0, or auto: "two"`,
		"block-beta\n  a\n  b[\"x\"] --> a\n": `a link's end names a block`,
		"block-beta extra\n  a\n":             `unexpected "extra" after block`,
	} {
		_, err := mermaid.Parse(src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || !strings.Contains(se.Msg, want) {
			t.Errorf("%q: %v; want a syntax error saying %q", src, err, want)
		}
	}
	if _, err := mermaid.Parse("block-beta\n  arrow<[\"next\"]>(right)\n"); !errors.Is(err, mermaid.ErrUnsupported) {
		t.Errorf("a block arrow: %v, want ErrUnsupported", err)
	}
}
