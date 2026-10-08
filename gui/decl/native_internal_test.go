package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/widget"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// The built-in markdown factory's documented behaviour: MarkdownSpec.Mermaid draws no diagram
// while gui has no Diagrammer, so a mermaid fence lays out as code, every line kept, no picture.
func TestMermaidRendersAsCodeUntilADiagrammerExists(t *testing.T) {
	r := markdownRenderer(tuidecl.MarkdownSpec{Mermaid: true})
	lines := []string{"top", "```mermaid", "graph TD", "A-->B", "```", "end"}
	var fence widget.Block
	for _, b := range r.Blocks(lines, 0, len(lines)) {
		if b.From == 1 {
			fence = b
		}
	}
	if fence.To-fence.From != 4 {
		t.Fatalf("the mermaid fence is block %+v, want lines 1 to 5", fence)
	}
	sh := gui.NewRecordingCanvas(gui.Size{W: 400, H: 200}, gui.Size{W: 8, H: 16}).Text()
	th := widget.Theme{Prose: gui.Font{Size: 14}, Mono: gui.Font{Size: 13}}
	bl := r.LayOut(fence, lines, 300, false, sh, th, nil)
	if bl.Picture != nil {
		t.Error("a mermaid fence drew a picture with no Diagrammer")
	}
	if len(bl.Lines) != 4 {
		t.Errorf("the fence laid out %d lines of code, want 4", len(bl.Lines))
	}
}
