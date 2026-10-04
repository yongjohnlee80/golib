package view_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/view"
)

var (
	_ parse.Parser[*view.File] = view.View{}
	_ parse.Named              = view.View{}
)

func TestView_ParsesAsTheFunctionDoes(t *testing.T) {
	t.Parallel()
	src := []byte("---\nname: v\n---\n# {{.title}}\n")
	got, err := view.New(view.WithName("v.view")).Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := view.Parse(src, view.WithName("v.view"))
	if got.Body != want.Body || got.Front != want.Front || !reflect.DeepEqual(got.Source, want.Source) || got.Name != want.Name {
		t.Error("the parser value and Parse read different files")
	}
	// The options reach the parse: the name is in the error's position.
	if _, err := view.New(view.WithName("bad.view")).Parse([]byte("name: v\n")); err == nil || !strings.Contains(err.Error(), "bad.view:1:1") {
		t.Errorf("err = %v, want a position naming bad.view", err)
	}
	if view.New().FormatName() != "view" {
		t.Error("FormatName")
	}
}

// Every way a .view file fails answers as golib/parse's shared syntax error, positioned in the file.
func TestError_IsAParseSyntaxError(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		src  string
		line int
	}{
		"no opening line": {"name: v\n", 1},
		"bad frontmatter": {"---\nname: v\nargs: [a\n---\n", 4},
		"bad template":    {"---\nname: v\n---\n{{if .x}}\n", 4},
	} {
		_, err := view.Parse([]byte(tc.src), view.WithName("v.view"))
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%s: err = %v, not a parse.SyntaxError", name, err)
			continue
		}
		if se.Format != "view" || se.Pos.Line != tc.line || se.Pos.File != "v.view" {
			t.Errorf("%s: SyntaxError %+v, want format view at v.view line %d", name, se, tc.line)
		}
		if !errors.Is(err, parse.ErrSyntax) {
			t.Errorf("%s: err = %v, not parse.ErrSyntax", name, err)
		}
		var ve *view.Error
		if !errors.As(err, &ve) {
			t.Errorf("%s: err = %v, no longer a *view.Error", name, err)
		}
	}
}
