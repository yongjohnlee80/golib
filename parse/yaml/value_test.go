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
// and keeps its own type and message.
func TestError_IsAParseSyntaxError(t *testing.T) {
	t.Parallel()
	_, err := yaml.Parse([]byte("a: 1\nb: [2\n"))
	var se parse.SyntaxError
	if !errors.As(err, &se) {
		t.Fatalf("err = %v, not a parse.SyntaxError", err)
	}
	var ye *yaml.Error
	if !errors.As(err, &ye) {
		t.Fatalf("err = %v, no longer a *yaml.Error", err)
	}
	if se.Format != "yaml" || se.Pos != ye.Pos || se.Pos.Line == 0 {
		t.Errorf("SyntaxError %+v, want format yaml at %v", se, ye.Pos)
	}
	if !errors.Is(err, parse.ErrSyntax) || !errors.Is(err, errs.ErrInvalidArgument) || errors.Is(err, parse.ErrUnterminated) {
		t.Errorf("err = %v: identities wrong", err)
	}
	var notSyntax *parse.SyntaxError // a pointer target never matches, as for parse.SyntaxError itself
	if errors.As(err, &notSyntax) {
		t.Error("a *parse.SyntaxError target matched")
	}
}
