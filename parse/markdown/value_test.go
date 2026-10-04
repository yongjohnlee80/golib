package markdown_test

import (
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/markdown"
)

var (
	_ parse.Parser[*markdown.Document] = markdown.Markdown{}
	_ parse.Named                      = markdown.Markdown{}
)

func TestMarkdown_ParsesAsTheFunctionDoes(t *testing.T) {
	t.Parallel()
	src := []byte("# Title\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\n~~gone~~\n")
	p := markdown.New(markdown.GFM())
	got, err := p.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	if want := markdown.Parse(src, markdown.GFM()); !reflect.DeepEqual(got.Root, want.Root) {
		t.Error("the parser value and Parse built different trees")
	}
	// The options reach the parse: without GFM the table is a paragraph.
	plain, _ := markdown.New().Parse(src)
	if reflect.DeepEqual(plain.Root, got.Root) {
		t.Error("GFM made no difference, so the options did not reach the parse")
	}
	if p.FormatName() != "markdown" {
		t.Errorf("FormatName = %q", p.FormatName())
	}
}

// The options are copied, so a caller reusing its slice cannot change a parser it already built.
func TestNew_CopiesTheOptions(t *testing.T) {
	t.Parallel()
	opts := []markdown.Option{markdown.GFM()}
	p := markdown.New(opts...)
	opts[0] = nil
	src := []byte("| a |\n| - |\n| 1 |\n")
	got, _ := p.Parse(src)
	if want := markdown.Parse(src, markdown.GFM()); !reflect.DeepEqual(got.Root, want.Root) {
		t.Error("changing the caller's option slice changed the parser")
	}
}
