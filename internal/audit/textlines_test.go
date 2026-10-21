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
// lives in textbuffer.go's line primitives, and this guard keeps it there: no other non-test file
// in tui/widget assigns to bufferText.textLines, whole or by index.

const (
	textLinesField = "textLines"
	textLinesHome  = "textbuffer.go"
)

// textLinesWrites is every assignment to a .textLines selector (x.textLines = …, x.textLines[i] =
// …, x.textLines[i] += …) in tui/widget's non-test files, by file.
func textLinesWrites(t *testing.T, root string) map[string][]string {
	t.Helper()
	dir := filepath.Join(root, "tui", "widget")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading tui/widget: %v", err)
	}
	fset := token.NewFileSet()
	out := map[string][]string{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			continue // the build reports it first
		}
		ast.Inspect(f, func(n ast.Node) bool {
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
					out[name] = append(out[name], fset.Position(as.Pos()).String())
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
	for _, f := range files {
		for _, at := range writes[f] {
			t.Errorf("%s writes .%s directly: use the line primitives in %s (setLine, insertLines, "+
				"removeLines, replaceLines, setLines), which touch and log the change", at, textLinesField, textLinesHome)
		}
	}
}
