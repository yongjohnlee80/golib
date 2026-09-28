package dao

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/dao/internal/txexec"
	"github.com/yongjohnlee80/golib/errs"
)

// txexec must import nothing of golib: dao imports it to install the hook, so
// an import of dao (or of anything that imports dao) would be a cycle, and a
// golib import of any kind makes one possible.
func TestTxexecImportsOnlyTheStandardLibrary(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("internal", "txexec", "*.go"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no txexec sources found (%v): this test would check nothing", err)
	}
	for _, f := range files {
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if first := strings.SplitN(p, "/", 2)[0]; strings.Contains(first, ".") {
				t.Errorf("%s imports %s; txexec may import only the standard library", f, p)
			}
		}
	}
}

func TestTxexecJoinIsInstalledAndRefusesOtherValues(t *testing.T) {
	if txexec.Join == nil {
		t.Fatal("dao did not install txexec.Join")
	}
	tx := Begin(context.Background())
	for name, args := range map[string][2]any{
		"a string and an int":       {"nope", 1},
		"a transaction and nothing": {tx, nil},
		"nothing and a connection":  {nil, newConn()},
		"a nil transaction":         {(*Transaction)(nil), newConn()},
	} {
		if _, err := txexec.Join(args[0], args[1]); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%s: %v, want ErrInvalidArgument", name, err)
		}
	}
	// a real pair reaches the transaction's join: the fake connection cannot
	// begin, and that is the error that comes back
	if _, err := txexec.Join(tx, newConn()); err == nil || err.Error() != "no tx" {
		t.Errorf("a transaction and a connection: %v, want the connection's own Begin error", err)
	}
}
