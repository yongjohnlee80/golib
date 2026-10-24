package html_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	"github.com/yongjohnlee80/golib/parse/markdown/html"
)

// The deprecated options are their replacements under the old names: GFM, Obsidian, Unsafe and
// SourceSpans give the page WithGFM, WithObsidian, WithUnsafe and WithSourceSpans give it, on a
// source each of them changes.
func TestTheDeprecatedOptionsAreTheirReplacements(t *testing.T) {
	src := []byte("---\ntitle: x\n---\n| a | b |\n|---|---|\n| [[note]] | ~~gone~~ |\n\n<div>raw</div>\n\n- [x] done #tag\n")
	render := func(opts []markdown.Option, hopts ...html.Option) string {
		var b strings.Builder
		if err := html.Render(&b, markdown.Parse(src, opts...), hopts...); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	old := render([]markdown.Option{markdown.GFM(), markdown.Obsidian()}, html.Unsafe(), html.SourceSpans())
	now := render([]markdown.Option{markdown.WithGFM(), markdown.WithObsidian()}, html.WithUnsafe(), html.WithSourceSpans())
	if old != now {
		t.Fatalf("the deprecated options render\n%s\nthe replacements\n%s", old, now)
	}
	// the source is one every option changes: without them the page differs
	if plain := render(nil); plain == now {
		t.Fatalf("the options change nothing on this source:\n%s", plain)
	}
	for _, want := range []string{"<table", "data-src=", "<div>raw</div>", "<del>"} {
		if !strings.Contains(now, want) {
			t.Errorf("the page has no %q:\n%s", want, now)
		}
	}
}
