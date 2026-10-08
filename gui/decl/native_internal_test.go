package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/widget"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// The built-in markdown factory: MarkdownSpec.Mermaid gives the renderer the style's one Diagrams
// chain, shared by every renderer the style builds (one cache per window); without it, none. With
// no DiagramHost (no window yet) a mermaid fence still lays out as code, every line kept.
func TestMermaidDrawsThroughTheStylesOneChain(t *testing.T) {
	n := &native{renderers: map[string]func(tuidecl.RendererSpec) widget.Renderer{}}
	on := n.markdownRenderer(tuidecl.MarkdownSpec{Mermaid: true}).(*widget.MarkdownRenderer)
	again := n.markdownRenderer(tuidecl.MarkdownSpec{Mermaid: true}).(*widget.MarkdownRenderer)
	off := n.markdownRenderer(tuidecl.MarkdownSpec{}).(*widget.MarkdownRenderer)
	if on.Diagrams() == nil || on.Diagrams() != again.Diagrams() {
		t.Errorf("mermaid renderers' chains %v and %v: want one, shared", on.Diagrams(), again.Diagrams())
	}
	if off.Diagrams() != nil {
		t.Error("a renderer without mermaid has a chain")
	}

	lines := []string{"top", "```mermaid", "graph TD", "A-->B", "```", "end"}
	var fence widget.Block
	for _, b := range on.Blocks(lines, 0, len(lines)) {
		if b.From == 1 {
			fence = b
		}
	}
	sh := gui.NewRecordingCanvas(gui.Size{W: 400, H: 200}, gui.Size{W: 8, H: 16}).Text()
	th := widget.Theme{Prose: gui.Font{Size: 14}, Mono: gui.Font{Size: 13}}
	bl := on.LayOut(fence, lines, 300, false, sh, th, nil)
	if bl.Picture != nil || len(bl.Lines) != 4 {
		t.Errorf("with no host the fence drew a picture (%v) or laid %d lines, want 4 lines of code", bl.Picture != nil, len(bl.Lines))
	}
}
