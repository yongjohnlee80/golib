package yaml_test

import (
	"errors"
	"reflect"
	"testing"

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
