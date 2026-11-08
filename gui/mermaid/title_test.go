package mermaid

import (
	"context"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// fmTitle is a front matter block titled Weekly plan, with a config block passed over.
const fmTitle = "---\ntitle: Weekly plan\nconfig:\n  theme: forest\n---\n"

// titleLabels are l's labels that read "Weekly plan".
func titleLabels(l *Laid) []label {
	var out []label
	for _, lb := range l.texts {
		if len(lb.lines) == 1 && lb.lines[0] == "Weekly plan" {
			out = append(out, lb)
		}
	}
	return out
}

// A kind that draws no title of its own (a flowchart, a sankey) draws its front matter's title in a
// window above everything else, in the theme's text colour, the diagram taller by the title's band;
// a kind that draws its own (a pie) draws it once. In sepia and in dark.
func TestAFrontMatterTitleIsDrawnAboveInAWindow(t *testing.T) {
	m, release := gui.AcquireMeasurer(1)
	defer release()
	dark := testTheme
	dark.Background, dark.Text = color.NRGBA{R: 0x1c, G: 0x1c, B: 0x1c, A: 0xff}, color.NRGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff}
	for _, th := range []struct {
		name string
		th   Theme
	}{{"sepia", testTheme}, {"dark", dark}} {
		for name, body := range map[string]string{
			"flowchart": "flowchart LR\n  A[Start] --> B[Finish]",
			"sankey":    "sankey-beta\n\nWater,Tea,10\nWater,Coffee,4\n",
		} {
			l, err := Lay(context.Background(), fmTitle+body, 4000, th.th, m)
			if err != nil {
				t.Fatalf("%s %s: %v", th.name, name, err)
			}
			l0, err := Lay(context.Background(), body, 4000, th.th, m)
			if err != nil {
				t.Fatal(err)
			}
			titles := titleLabels(l)
			if len(titles) != 1 {
				t.Fatalf("%s %s: %d title labels; want one", th.name, name, len(titles))
			}
			ti := titles[0]
			if ti.color != th.th.Text {
				t.Errorf("%s %s: the title's colour %v; want the theme's text %v", th.name, name, ti.color, th.th.Text)
			}
			bottom := ti.box.Y + ti.box.H
			for _, n := range l.nodes {
				if n.box.Y < bottom {
					t.Errorf("%s %s: a node at y %v over the title (to %v)", th.name, name, n.box.Y, bottom)
				}
			}
			for _, b := range l.bands {
				if min(b.y0a, b.y1a) < bottom {
					t.Errorf("%s %s: a flow at y %v over the title (to %v)", th.name, name, min(b.y0a, b.y1a), bottom)
				}
			}
			if l.size.H <= l0.size.H {
				t.Errorf("%s %s: %v tall; want taller than the untitled %v", th.name, name, l.size.H, l0.size.H)
			}
		}
		l, err := Lay(context.Background(), fmTitle+"pie\n  \"Dogs\" : 3\n  \"Cats\" : 2", 4000, th.th, m)
		if err != nil {
			t.Fatal(err)
		}
		if n := len(titleLabels(l)); n != 1 {
			t.Errorf("%s: a pie with a front matter title draws it %d times; want once, its own", th.name, n)
		}
	}
}
