package qmlfiles_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/decl/themes"
	"github.com/yongjohnlee80/golib/tui/decl/themes/qmlfiles"
)

// The names are the files', and the themes package offers the same set.
func TestNamesAreTheFiles(t *testing.T) {
	names := qmlfiles.Names()
	if len(names) == 0 {
		t.Fatal("no themes")
	}
	for _, n := range names {
		if _, err := fs.Stat(qmlfiles.FS(), n+".qml"); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	if !slices.Equal(names, themes.Names()) {
		t.Errorf("qmlfiles names %v, themes %v", names, themes.Names())
	}
}

// The package imports nothing of golib, so a program reading theme names links no UI.
func TestTheFilesNeedNoToolkit(t *testing.T) {
	srcs, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, src := range srcs {
		if strings.HasSuffix(src, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), src, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range f.Imports {
			if p := strings.Trim(imp.Path.Value, `"`); strings.HasPrefix(p, "github.com/yongjohnlee80/") {
				t.Errorf("%s imports %s", src, p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no source files read")
	}
}
