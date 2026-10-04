package view_test

import (
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
