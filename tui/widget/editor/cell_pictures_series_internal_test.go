package editor

import (
	"context"
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TestAPieLaidFromSyntaxWearsTheChartSeries: a pie laid from a page's syntax styles takes the chart
// series those styles give (CellPictureThemes), as a chart on the same page does, so its slices are
// the page's code colours.
func TestAPieLaidFromSyntaxWearsTheChartSeries(t *testing.T) {
	var styles SyntaxStyles
	styles[highlight.Keyword] = style.New().Foreground(style.RGB(200, 40, 40))
	styles[highlight.String] = style.New().Foreground(style.RGB(40, 160, 40))
	styles[highlight.DecVal] = style.New().Foreground(style.RGB(40, 40, 200))
	pic, err := LayCellPicture(context.Background(), "mermaid", "pie\n  \"Dogs\" : 386\n  \"Cats\" : 85\n  \"Rats\" : 15",
		func(s string) int { return len([]rune(s)) }, 60, styles)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := pic.(*mermaidPicture)
	if !ok {
		t.Fatalf("a pie laid as %T, want a mermaid picture", pic)
	}
	_, chart := CellPictureThemes(styles)
	if len(chart.Series) < 3 || !slices.Equal(p.th.Series, chart.Series) {
		t.Fatalf("the pie's series are %v, want the charts' %v", p.th.Series, chart.Series)
	}
}
