package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// embedNote embeds a diagram, a chart, a table, a file too large to draw, and a file that isn't
// there, each on a line of its own.
const embedNote = "intro\n\n![flow](a.mmd)\n\n![sales](b.chart.json)\n\n![stock](c.csv)\n\n" +
	"![big](big.csv)\n\n![gone](missing.mmd)\n\nafter\n"

// embedFiles are the files embedNote's lines embed, as a host resolves them: the Markdown each
// stands for (missing.mmd resolves to nothing).
var embedFiles = map[string]string{
	"a.mmd":        "```mermaid\nflowchart LR\n  Gull --> Tern\n```",
	"b.chart.json": "```vega-lite\n" + `{"mark":"bar","data":{"values":[{"k":"Apples","v":3},{"k":"Pears","v":5}]},"encoding":{"x":{"field":"k","type":"nominal"},"y":{"field":"v","type":"quantitative"}}}` + "\n```",
	"c.csv":        "| Item | Qty |\n| --- | ---: |\n| apple | 3 |\n| pear | 5 |\n\n*1 more row*",
	"big.csv":      "big.csv is too large to draw here",
}

// An image-only line whose src the renderer's Embeds resolves is drawn as the file it embeds: a
// mermaid fence as its diagram, a vega-lite fence as its chart, a table boxed with a note under
// it, other Markdown (a notice) as its words, each in the line's place, the line's own text gone.
// A src Embeds doesn't resolve is the image's, as before: here, its alt text. Typing on an embed's
// line shows its source, and Esc draws it again.
func TestAnImageOnlyLineIsDrawnAsTheFileItEmbeds(t *testing.T) {
	r := mdRenderer()
	r.Embeds = func(src string) (string, bool) {
		md, ok := embedFiles[src]
		return md, ok
	}
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText(embedNote)))
	sh := newShell(ed)
	h := startApp(t, sh, 60, 60)
	defer h.stop()
	h.onLoop(func() {
		ed.SetRenderer(r)
		ed.SetView(widget.ViewRendered)
		h.app.FocusInto(ed)
	})
	h.barrier(sh)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("the view:\n%s", h.grid())
		}
	})
	h.waitFor("each embed drawn as its file", func() bool {
		g := h.grid()
		for _, want := range []string{"Gull", "Tern", "Apples", "Pears", "apple", "pear", "1 more row",
			"big.csv is too large to draw here", "gone", "after"} {
			if !strings.Contains(g, want) {
				return false
			}
		}
		return strings.Contains(g, "┌") && !strings.Contains(g, "](a.mmd)") && !strings.Contains(g, "](b.chart.json)") &&
			!strings.Contains(g, "](c.csv)") && !strings.Contains(g, "](big.csv)") && !strings.Contains(g, "flowchart")
	})
	// typed in, the diagram's line is its source; Esc, drawn again
	h.onLoop(func() { ed.Core().SetLine(2, 0) })
	h.inject(key('i'))
	h.waitFor("the embed's source while typing on its line", func() bool {
		g := h.grid()
		return strings.Contains(g, "![flow](a.mmd)") && !strings.Contains(g, "Gull")
	})
	h.inject(key(tui.KeyEscape))
	h.waitFor("the diagram drawn again in Normal", func() bool {
		g := h.grid()
		return strings.Contains(g, "Gull") && !strings.Contains(g, "](a.mmd)")
	})
}
