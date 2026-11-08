package widget

import (
	"context"
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/highlight"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	tuimermaid "github.com/yongjohnlee80/golib/tui/mermaid"
	"github.com/yongjohnlee80/golib/tui/style"
	tuivegalite "github.com/yongjohnlee80/golib/tui/vegalite"
)

// A diagram drawn in cells: a Mermaid diagram (tui/mermaid) or a Vega-Lite chart (tui/vegalite),
// laid out within a number of columns and painted later. The HTML page and the cell Editor's
// Rendered view lay their pictures by one rule (LayCellPicture), in one theme (CellPictureThemes).

// CellPicture is a diagram drawn in cells: its size, and how it paints at (x0, y0) of a surface.
type CellPicture interface {
	Size() tui.Size
	Paint(s tui.Surface, x0, y0 int)
}

// ErrNoCellPicture is a diagram the terminal doesn't draw: a fence of another language, or a laid
// diagram with no rows.
var ErrNoCellPicture = errors.New("widget: no picture in cells")

// LayCellPicture lays src, a fence of lang ("mermaid" or "vega-lite"), within cols, measured by
// measure, in the theme syntax gives (CellPictureThemes): a Mermaid kind tui/mermaid draws, fitted
// to cols where its kind can be laid narrower, else at its own width; a Vega-Lite spec by
// tui/vegalite's first tier that takes it. Its error says why there is none: a kind or a spec the
// terminal doesn't draw, one that doesn't parse, one too large.
func LayCellPicture(ctx context.Context, lang, src string, measure func(string) int, cols int, syntax SyntaxStyles) (CellPicture, error) {
	m, cols := measureFunc(measure), max(cols, 1)
	diagram, chart := CellPictureThemes(syntax)
	switch lang {
	case "mermaid":
		d, err := pm.Parse(src)
		if err != nil {
			return nil, err
		}
		if err := tuimermaid.Drawn(d); err != nil {
			return nil, err
		}
		g, err := tuimermaid.LayWithin(ctx, d, m, cols)
		if err != nil {
			return nil, err
		}
		if g.Size.H == 0 {
			return nil, ErrNoCellPicture
		}
		return &mermaidPicture{g: g, th: diagram}, nil
	case "vega-lite":
		d, err := tuivegalite.Lay(ctx, src, m, cols)
		if err != nil {
			return nil, err
		}
		if d.H == 0 {
			return nil, ErrNoCellPicture
		}
		return &chartPicture{d: d, th: chart}, nil
	}
	return nil, fmt.Errorf("%q: %w", lang, ErrNoCellPicture)
}

// CellPictureThemes are a picture's styles: a chart's series in the code colours syntax gives (with
// none, the tokens), a diagram's diagramTheme with the charts' series, so a pie's slices and a
// chart's series take the same colours.
func CellPictureThemes(syntax SyntaxStyles) (tuimermaid.Theme, tuivegalite.Theme) {
	chart := tuivegalite.ThemeFromSyntax(func(r highlight.Style) style.Style { return syntax[r] })
	diagram := diagramTheme
	diagram.Series = chart.Series
	return diagram, chart
}

// diagramTheme is a diagram's styles in cells: mermaid.TokenTheme's, the surface's own tokens on
// its own background.
var diagramTheme = tuimermaid.TokenTheme()

// measureFunc is a width function as tui/mermaid's and tui/vegalite's Measurer.
type measureFunc func(string) int

func (f measureFunc) StringWidth(s string) int { return f(s) }

// mermaidPicture is a Mermaid grid in the theme it was laid in. A row holds it by pointer, as a
// chart: its theme's series are a slice, and a picture is told from the next by comparing them.
type mermaidPicture struct {
	g  *tuimermaid.Grid
	th tuimermaid.Theme
}

func (p *mermaidPicture) Size() tui.Size                  { return p.g.Size }
func (p *mermaidPicture) Paint(s tui.Surface, x0, y0 int) { p.g.Paint(s, x0, y0, -1, p.th) }
func (p *mermaidPicture) HitAt(x, y int) (string, tui.Rect, bool) {
	return p.g.HitAt(x, y)
}

// chartPicture is a Vega-Lite chart in the theme it was laid in. A row holds it by pointer: its
// theme's palette is a slice, and a picture is told from the next by comparing them.
type chartPicture struct {
	d  *tuivegalite.Drawing
	th tuivegalite.Theme
}

func (p *chartPicture) Size() tui.Size                  { return tui.Size{W: p.d.W, H: p.d.H} }
func (p *chartPicture) Paint(s tui.Surface, x0, y0 int) { p.d.Paint(s, x0, y0, p.th) }

// pictureRows are picture p's rows of cells, as it paints them once, each cell a decoration (no
// source of its own); a cell it leaves is a blank. Each row is the picture's width.
func pictureRows(p CellPicture, measure func(string) int) []CellRow {
	size := p.Size()
	rec := &cellRecorder{w: size.W, h: size.H, measure: measure, cells: make([][]RowCell, size.H)}
	for y := range rec.cells {
		rec.cells[y] = make([]RowCell, size.W)
	}
	p.Paint(rec, 0, 0)
	rows := make([]CellRow, size.H)
	for y, line := range rec.cells {
		row := CellRow{Anchor: -1}
		for x := 0; x < len(line); {
			c := line[x]
			if c.W == 0 { // left blank, or under a wide cluster's second cell (skipped below)
				c = RowCell{Text: " ", W: 1, Line: -1}
			}
			row.Cells = append(row.Cells, c)
			x += c.W
		}
		rows[y] = row
	}
	return rows
}

// cellRecorder is a surface that keeps what is painted on it, a cell per column: a picture painted
// once becomes rows of cells.
type cellRecorder struct {
	w, h    int
	measure func(string) int
	cells   [][]RowCell
}

var _ tui.Surface = (*cellRecorder)(nil)

func (r *cellRecorder) SetCell(x, y int, content string, st style.Style) {
	if y < 0 || y >= r.h || x < 0 || x >= r.w || content == "" {
		return
	}
	w := max(r.measure(content), 1)
	if x+w > r.w { // a wide cluster past the right edge: a blank in its place
		content, w = " ", 1
	}
	r.cells[y][x] = RowCell{Text: content, W: w, Style: st, Line: -1}
	for i := 1; i < w; i++ {
		r.cells[y][x+i] = RowCell{} // under the wide cluster
	}
}

func (r *cellRecorder) Fill(rect tui.Rect, content string, st style.Style) {
	w := max(r.measure(content), 1)
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		for x := rect.X; x < rect.X+rect.W; x += w {
			r.SetCell(x, y, content, st)
		}
	}
}

func (r *cellRecorder) Sub(rect tui.Rect) tui.Surface {
	return &subRecorder{r: r, at: rect}
}

func (r *cellRecorder) Size() tui.Size           { return tui.Size{W: r.w, H: r.h} }
func (r *cellRecorder) StringWidth(s string) int { return r.measure(s) }
func (r *cellRecorder) Theme() *style.Theme      { return nil }
func (r *cellRecorder) Caps() tui.Capabilities   { return tui.Capabilities{} }

// subRecorder is a recorder's view of rect, its origin rect's.
type subRecorder struct {
	r  *cellRecorder
	at tui.Rect
}

func (s *subRecorder) SetCell(x, y int, content string, st style.Style) {
	if x < 0 || y < 0 || x >= s.at.W || y >= s.at.H {
		return
	}
	s.r.SetCell(s.at.X+x, s.at.Y+y, content, st)
}

func (s *subRecorder) Fill(rect tui.Rect, content string, st style.Style) {
	w := max(s.r.measure(content), 1)
	for y := rect.Y; y < rect.Y+rect.H; y++ {
		for x := rect.X; x < rect.X+rect.W; x += w {
			s.SetCell(x, y, content, st)
		}
	}
}

func (s *subRecorder) Sub(rect tui.Rect) tui.Surface {
	return &subRecorder{r: s.r, at: tui.Rect{X: s.at.X + rect.X, Y: s.at.Y + rect.Y, W: min(rect.W, s.at.W-rect.X), H: min(rect.H, s.at.H-rect.Y)}}
}

func (s *subRecorder) Size() tui.Size           { return tui.Size{W: s.at.W, H: s.at.H} }
func (s *subRecorder) StringWidth(t string) int { return s.r.measure(t) }
func (s *subRecorder) Theme() *style.Theme      { return nil }
func (s *subRecorder) Caps() tui.Capabilities   { return tui.Capabilities{} }
