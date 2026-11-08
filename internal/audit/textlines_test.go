package audit

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// THE EDITOR LINE-PRIMITIVES GUARD.
//
// A change to an editor's lines has to touch what it changed (the highlighter's cache, the layout)
// and log what it did to the line structure (marks, other editors over a shared buffer). About
// twenty sites once wrote the lines directly and repeated that bookkeeping by hand, and a site that
// forgot left marks and cursors on the wrong lines with nothing to say so. The bookkeeping now
// lives in the text model's line primitives (tui/widget/internal/text/buffer.go), and this guard
// keeps it there: no other non-test file in tui/widget or the packages under it assigns to
// Doc.Lines, whole or by index.
//
// Undo rests on it too: an editor's history keeps no copy of the text, only what each primitive
// hands it to take its change back (Doc.UndoRec), so a write past the primitives would be a change
// undo cannot take back. Inside buffer.go, the writes must sit in the primitives themselves
// (textLinesWriters).

const (
	textLinesField = "Lines"
	textLinesHome  = "internal/text/buffer.go" // relative to tui/widget
)

// textLinesWriters are the functions in textLinesHome that may write the lines: the primitives,
// each of which hands undo its change before it makes it.
var textLinesWriters = map[string]bool{
	"SetLine": true, "InsertLines": true, "RemoveLines": true, "ReplaceLines": true, "SetValue": true,
	"DeleteRegion": true, "InsertText": true, // a region's delete and a typed text's insert
}

// textLineWrite is one assignment to the lines: where, and the function it is in ("" outside one).
type textLineWrite struct{ at, fn string }

// textLinesWrites is every assignment to a .Lines selector (x.Lines = …, x.Lines[i] = …,
// x.Lines[i] += …) in the non-test files of tui/widget and the packages under it, by file (its
// path from tui/widget).
func textLinesWrites(t *testing.T, root string) map[string][]textLineWrite {
	t.Helper()
	dir := filepath.Join(root, "tui", "widget")
	var names []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			rel, _ := filepath.Rel(dir, path)
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading tui/widget: %v", err)
	}
	fset := token.NewFileSet()
	out := map[string][]textLineWrite{}
	for _, name := range names {
		f, err := parser.ParseFile(fset, filepath.Join(dir, filepath.FromSlash(name)), nil, 0)
		if err != nil {
			continue // the build reports it first
		}
		fn := "" // the function the walk is in: a FuncDecl's body comes after its name
		ast.Inspect(f, func(n ast.Node) bool {
			switch d := n.(type) {
			case *ast.FuncDecl:
				fn = d.Name.Name
			case *ast.GenDecl:
				fn = "" // a var's initializer is in no function
			}
			as, ok := n.(*ast.AssignStmt)
			if !ok || as.Tok == token.DEFINE {
				return true
			}
			for _, lhs := range as.Lhs {
				for {
					if ix, ok := lhs.(*ast.IndexExpr); ok {
						lhs = ix.X
						continue
					}
					break
				}
				if sel, ok := lhs.(*ast.SelectorExpr); ok && sel.Sel.Name == textLinesField {
					out[name] = append(out[name], textLineWrite{fset.Position(as.Pos()).String(), fn})
				}
			}
			return true
		})
	}
	return out
}

func TestOnlyTheLinePrimitivesWriteTheLines(t *testing.T) {
	writes := textLinesWrites(t, repoRoot(t))
	// the positive control: the primitives themselves write the lines, and the scan must see them
	if len(writes[textLinesHome]) == 0 {
		t.Fatalf("no write to .%s seen in %s; the guard is not looking", textLinesField, textLinesHome)
	}
	var files []string
	for f := range writes {
		if f != textLinesHome {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	for _, w := range writes[textLinesHome] {
		if !textLinesWriters[w.fn] {
			t.Errorf("%s writes .%s in %s, which is not a line primitive: undo would not hear of the "+
				"change (textLinesWriters)", w.at, textLinesField, w.fn)
		}
	}
	for _, f := range files {
		for _, w := range writes[f] {
			t.Errorf("%s writes .%s directly: use the line primitives in %s (SetLine, InsertLines, "+
				"RemoveLines, ReplaceLines, SetValue, DeleteRegion, InsertText), which touch and log the "+
				"change and hand undo what takes it back", w.at, textLinesField, textLinesHome)
		}
	}
}
