package yaml_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/yaml"
)

var (
	_ parse.Parser[*yaml.Stream] = yaml.YAML{}
	_ parse.Named                = yaml.YAML{}
)

func TestYAML_ParsesAsTheFunctionDoes(t *testing.T) {
	t.Parallel()
	src := []byte("a: [1, [2, [3]]]\nb: x\n")
	got, err := yaml.New().Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := yaml.Parse(src)
	if !reflect.DeepEqual(got.Docs, want.Docs) {
		t.Error("the parser value and Parse built different trees")
	}
	// The options reach the parse: a depth bound below the nesting refuses it.
	if _, err := yaml.New(yaml.MaxDepth(2)).Parse(src); err == nil {
		t.Error("MaxDepth(2) accepted four levels of nesting")
	}
	var ye *yaml.Error
	if _, err := yaml.New().Parse([]byte("a: [1\n")); !errors.As(err, &ye) {
		t.Errorf("err = %v, want a *yaml.Error", err)
	}
	if yaml.New().FormatName() != "yaml" {
		t.Error("FormatName")
	}
}

// A YAML syntax error also answers as golib/parse's shared syntax error, with the same position,
// and keeps its own type and message. A stream that ended mid-construct is ErrUnterminated and one
// that is wrong where it stands is ErrSyntax: never both.
func TestError_IsAParseSyntaxError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src        string
		incomplete bool
	}{
		{"a: 1\nb: [2\n", true}, // a flow sequence still open
		{"a: {x: 1", true},      // a flow mapping still open
		{"a: 'quoted", true},    // a quoted scalar still open
		{"a: \"esc\\u12", true}, // an escape cut off
		{"- [a, b\n- c", true},  // still inside the '[': "b - c" is one plain scalar
		{"- [a, b]]\n", false},  // a ']' that closes nothing
		{"a: ]", false},         // a ']' that opens nothing
		{"a: b: c", false},      // a mapping value where none is allowed
		{"a: 'x' y", false},     // content after a closed quoted scalar
		{"a: \xff b", false},    // a byte not valid in UTF-8, with text after it
		{"a: \xe2\x82", true},   // a character cut off part way through its encoding
	} {
		_, err := yaml.Parse([]byte(tc.src))
		var se parse.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: err = %v, not a parse.SyntaxError", tc.src, err)
			continue
		}
		var ye *yaml.Error
		if !errors.As(err, &ye) || se.Format != "yaml" || se.Pos != ye.Pos {
			t.Errorf("%q: SyntaxError %+v does not match the *yaml.Error", tc.src, se)
		}
		if se.Incomplete != tc.incomplete || ye.Incomplete != tc.incomplete {
			t.Errorf("%q: incomplete = %v, want %v (%v)", tc.src, se.Incomplete, tc.incomplete, err)
		}
		if errors.Is(err, parse.ErrUnterminated) != tc.incomplete || errors.Is(err, parse.ErrSyntax) == tc.incomplete {
			t.Errorf("%q: identities wrong: unterminated %v, syntax %v", tc.src, errors.Is(err, parse.ErrUnterminated), errors.Is(err, parse.ErrSyntax))
		}
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%q: not errs.ErrInvalidArgument", tc.src)
		}
	}
	var notSyntax *parse.SyntaxError // a pointer target never matches, as for parse.SyntaxError itself
	_, err := yaml.Parse([]byte("a: ]"))
	if errors.As(err, &notSyntax) {
		t.Error("a *parse.SyntaxError target matched")
	}
}
