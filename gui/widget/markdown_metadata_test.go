package widget

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

func metadataLayout(t *testing.T, source string, line int, editing bool) BlockLayout {
	t.Helper()
	r := NewMarkdownRenderer(HideMetadata())
	lines := strings.Split(source, "\n")
	sh := gui.NewRecordingCanvas(gui.Size{W: 500, H: 500}, gui.Size{W: 8, H: 16}).Text()
	th := Theme{Prose: gui.Font{Size: 16}, Mono: gui.Font{Family: gui.MonospaceFamily(), Size: 14}}
	for _, b := range r.Blocks(lines, 0, len(lines)) {
		if b.From <= line && line < b.To {
			return r.LayOut(b, lines, 400, editing, sh, th, nil)
		}
	}
	t.Fatalf("no block for line %d", line)
	return BlockLayout{}
}

func TestLaterCommentCloseAndRemovalReflowEarlierEditorBlocks(t *testing.T) {
	source := "before <!-- note\nstill note\nend\n\nlast"
	h := startEditor(t, 60, 20, WithRenderer(NewMarkdownRenderer(HideMetadata())), WithCore(tuiwidget.CoreInitialText(source)))
	h.onLoop(func() { h.e.SetMode(Rendered); h.e.core.SetLine(4, 0) })
	firstText := func() string {
		var text strings.Builder
		h.onLoop(func() {
			h.e.layout.ensure()
			lb := h.e.layout.lay(h.e.layout.blockOf(0), nil)
			for _, ll := range lb.bl.Lines {
				for _, sp := range ll.Spans {
					if !sp.Hidden {
						text.WriteString(sp.Text)
					}
				}
			}
		})
		return text.String()
	}
	h.paint()
	if text := firstText(); text != "before <!-- note" {
		t.Fatalf("initial text %q", text)
	}
	h.onLoop(func() {
		if _, err := h.e.core.Insert(tuiwidget.TextPos{Line: 2, Col: 3}, " -->"); err != nil {
			t.Error(err)
		}
		h.e.core.SetLine(4, 0)
	})
	h.paint()
	if text := firstText(); text != "before " {
		t.Fatalf("later close kept earlier layout: %q", text)
	}
	h.onLoop(func() {
		if err := h.e.core.Delete(tuiwidget.TextRange{From: tuiwidget.TextPos{Line: 2, Col: 3}, To: tuiwidget.TextPos{Line: 2, Col: 7}}); err != nil {
			t.Error(err)
		}
		h.e.core.SetLine(4, 0)
	})
	h.paint()
	if text := firstText(); text != "before <!-- note" {
		t.Fatalf("removing close kept earlier mask: %q", text)
	}
}

func TestMetadataFastEditsMatchFreshParserMasks(t *testing.T) {
	for _, c := range []struct{ old, new []string }{
		{[]string{"head", "ordinary [[link]] and `code` words", "<!-- note -->"}, []string{"head", "ordinary [[link]] and `code` new words", "<!-- note -->"}},
		{[]string{"<!-- note", "body", "-->", "tail"}, []string{"<!-- note", "longer body", "-->", "tail"}},
		{[]string{"before <!-- note", "end", ""}, []string{"before <!-- note", "end -->", ""}},
		{[]string{"text", "```", "<!-- literal -->", "```"}, []string{"text", "ordinary", "<!-- literal -->", "```"}},
		{[]string{"before <!-- note", "===", "end -->"}, []string{"before <!-- note", "words", "end -->"}},
		{[]string{"head", "`a` <!-- note -->", "tail"}, []string{"head", "`` <!-- note -->", "tail"}},
	} {
		var incremental, fresh markdownMetadata
		incremental.prepare(c.old)
		incremental.prepare(c.new)
		fresh.prepare(c.new)
		if !reflect.DeepEqual(incremental.clusters, fresh.clusters) || !reflect.DeepEqual(incremental.hidden, fresh.hidden) || !reflect.DeepEqual(incremental.continued, fresh.continued) {
			t.Errorf("incremental masks differ from fresh parse: %v -> %v", c.old, c.new)
		}
	}
}

func BenchmarkMetadataBlocksAfterOrdinaryEdit(b *testing.B) {
	var sb strings.Builder
	for i := 0; sb.Len() < 1<<20; i++ {
		fmt.Fprintf(&sb, "## Section %d\n\nSome prose %d with a [[link]] and `code`.\n<!-- note %d -->\n\n", i, i, i)
	}
	base := strings.Split(sb.String(), "\n")
	for _, hide := range []bool{false, true} {
		b.Run(fmt.Sprint(hide), func(b *testing.B) {
			lines := append([]string(nil), base...)
			r := NewMarkdownRenderer()
			if hide {
				r = NewMarkdownRenderer(HideMetadata())
			}
			r.Blocks(lines, 0, len(lines))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				lines[2] = fmt.Sprintf("Some prose %d with a [[link]] and `code`.", i)
				r.Blocks(lines, 0, len(lines))
			}
		})
	}
}

func visibleMetadataText(bl BlockLayout) string {
	var out strings.Builder
	for _, ll := range bl.Lines {
		for _, sp := range ll.Spans {
			if !sp.Hidden {
				out.WriteString(sp.Text)
			}
		}
	}
	return out.String()
}

func TestHiddenMetadataHasNoReadingHeightButKeepsEditingSource(t *testing.T) {
	for _, c := range []struct {
		source string
		line   int
		want   string
	}{
		{"---\ntitle: private\ntags: [work]\n---\n# Body", 0, "title: private"},
		{"---\ntitle: private\n...\n# Body", 1, "title: private"},
		{"<!-- private\n# not a heading\n```\n-->\n# Body", 1, "not a heading"},
		{"<!-- private without a close", 0, "private"},
	} {
		bl := metadataLayout(t, c.source, c.line, false)
		if bl.Height != 0 || visibleMetadataText(bl) != "" || bl.Picture != nil {
			t.Errorf("metadata visible: %+v", bl)
		}
		edit := metadataLayout(t, c.source, c.line, true)
		if edit.Height <= 0 || !strings.Contains(visibleMetadataText(edit), c.want) {
			t.Errorf("editing lost source %q: %+v", c.want, edit)
		}
	}
	if bl := metadataLayout(t, "---\ntitle: unfinished\n# body", 0, false); bl.Height == 0 {
		t.Fatal("incomplete frontmatter swallowed the document")
	}
}

func TestCommentsPreserveAdjacentTextAndUnicodeSourcePositions(t *testing.T) {
	for _, c := range []struct {
		source string
		line   int
		want   string
	}{
		{"前<!-- hidden -->後", 0, "前後"},
		{"before <!-- multi\nhidden continuation\n--> after", 0, "before "},
		{"before <!-- multi\nhidden continuation\n--> after", 2, " after"},
		{"<!-- hidden -->after", 0, "after"},
		{"**left** <!-- hidden --> *right*", 0, "left  right"},
		{"| left | right |\n|---|---|\n| before<!-- hidden -->after | x |", 2, "beforeafter"},
	} {
		bl := metadataLayout(t, c.source, c.line, false)
		text := visibleMetadataText(bl)
		if !strings.Contains(text, c.want) || strings.Contains(text, "hidden") || strings.Contains(text, "<!--") || strings.Contains(text, "-->") {
			t.Errorf("%q line%d: visible %q, want %q", c.source, c.line, text, c.want)
		}
		lines := strings.Split(c.source, "\n")
		for _, ll := range bl.Lines {
			for _, sp := range ll.Spans {
				if sp.Line < 0 {
					continue
				}
				var clusters []string
				for v := range tui.Graphemes(lines[sp.Line]) {
					clusters = append(clusters, v)
				}
				if sp.Col < 0 || sp.Col > len(clusters) {
					t.Fatalf("invalid source column %+v", sp)
				}
				if !sp.Hidden && strings.Contains(sp.Text, "後") && sp.Col != len(clusters)-1 {
					t.Errorf("Unicode suffix moved in source: %+v", sp)
				}
			}
		}
	}
}

func TestCommentLookingCodeAndEscapesStayVisible(t *testing.T) {
	for _, c := range []struct {
		source string
		line   int
	}{
		{"```html\n<!-- literal -->\n```", 1},
		{"    <!-- indented literal -->", 0},
		{"`<!-- inline literal -->`", 0},
		{`\<!-- escaped literal -->`, 0},
		{`<div title="<!-- attribute literal -->">text</div>`, 0},
		// A block opener interrupts a paragraph before its inline comment can close, so the
		// Markdown parser treats this as literal text rather than one cross-block comment.
		{"before <!-- literal\n# heading\n--> after", 0},
	} {
		if text := visibleMetadataText(metadataLayout(t, c.source, c.line, false)); !strings.Contains(text, "literal") {
			t.Errorf("code/escape hidden: %q -> %q", c.source, text)
		}
	}
}

func TestMetadataMaskChangesWithTheDocumentAndPartialBlockRanges(t *testing.T) {
	r := NewMarkdownRenderer(HideMetadata())
	lines := []string{"before", "<!-- hidden", "still hidden", "-->", "after"}
	blocks := r.Blocks(lines, 2, 5)
	if len(blocks) != 2 || blocks[0] != (Block{From: 2, To: 4}) || blocks[1] != (Block{From: 4, To: 5}) {
		t.Fatalf("partial coverage: %v", blocks)
	}
	lines[1] = "ordinary text"
	r.Blocks(lines, 0, len(lines))
	if r.metadata.hidden[2] {
		t.Fatal("stale comment mask after removing the opener")
	}
}
