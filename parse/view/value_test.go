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

// Every way a .view file fails answers as golib/parse's shared syntax error, positioned in the
// file. A file that ended mid-construct is ErrUnterminated and one that is wrong where it stands is
// ErrSyntax: never both, including through the YAML error a frontmatter failure wraps.
func TestError_IsAParseSyntaxError(t *testing.T) {
	t.Parallel()
	const head = "---\nname: v\n---\n"
	for name, tc := range map[string]struct {
		src        string
		line       int
		incomplete bool
	}{
		"empty file":                 {"", 1, true},
		"opening half typed":         {"--", 1, true},
		"no closing line":            {"---\nname: v\n", 3, true},
		"action open":                {head + "{{.x", 4, true},
		"block open":                 {head + "{{if .x}}", 4, true},
		"nested blocks open":         {head + "{{range .x}}{{if .y}}{{else}}", 4, true},
		"string open":                {head + "{{printf \"a", 4, true},
		"comment open":               {head + "{{/* note", 4, true},
		"first line wrong":           {"name: v\n---\n", 1, false},
		"dash then text":             {"-x", 1, false},
		"two yaml documents":         {"---\na: 1\n...\n--- \nb: 2\n---\n", 4, false},
		"yaml open in closed header": {"---\nname: v\nargs: [a\n---\n", 4, false},
		"end with nothing to end":    {head + "{{end}}", 4, false},
		"if with no condition":       {head + "{{if}}x{{end}}", 4, false},
		"else outside a block":       {head + "{{.x}} {{else}}", 4, false},
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
		if se.Incomplete != tc.incomplete {
			t.Errorf("%s: incomplete = %v, want %v (%v)", name, se.Incomplete, tc.incomplete, err)
		}
		if errors.Is(err, parse.ErrUnterminated) != tc.incomplete || errors.Is(err, parse.ErrSyntax) == tc.incomplete {
			t.Errorf("%s: identities wrong: unterminated %v, syntax %v", name, errors.Is(err, parse.ErrUnterminated), errors.Is(err, parse.ErrSyntax))
		}
		var ve *view.Error
		if !errors.As(err, &ve) || ve.Incomplete != tc.incomplete {
			t.Errorf("%s: err = %v, no longer a *view.Error with Incomplete %v", name, err, tc.incomplete)
		}
	}
}
