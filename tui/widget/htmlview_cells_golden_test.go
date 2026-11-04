package widget

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	mdhtml "github.com/yongjohnlee80/golib/parse/markdown/html"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

var update = flag.Bool("update", false, "rewrite the cell layout's goldens (testdata/htmlcells)")

// recordSurface records what PaintCells draws: each cell's cluster and style.
type recordSurface struct {
	w, h  int
	text  [][]string
	style [][]style.Style
}

func newRecordSurface(w, h int) *recordSurface {
	s := &recordSurface{w: w, h: h, text: make([][]string, h), style: make([][]style.Style, h)}
	for y := range h {
		s.text[y] = make([]string, w)
		s.style[y] = make([]style.Style, w)
		for x := range w {
			s.text[y][x] = " "
		}
	}
	return s
}

// SetCell keeps content's first cluster only, as a Surface does; a wide one covers the cell after
// it too, which then holds nothing of its own ("").
func (s *recordSurface) SetCell(x, y int, content string, st style.Style) {
	for g := range tui.Graphemes(content) {
		content = g
		break
	}
	if x >= 0 && x < s.w && y >= 0 && y < s.h {
		s.text[y][x], s.style[y][x] = content, st
		if tui.StringWidth(content) == 2 && x+1 < s.w {
			s.text[y][x+1], s.style[y][x+1] = "", st
		}
	}
}

func (s *recordSurface) Fill(r tui.Rect, content string, st style.Style) {
	for y := r.Y; y < r.Y+r.H; y++ {
		for x := r.X; x < r.X+r.W; x++ {
			s.SetCell(x, y, content, st)
		}
	}
}

func (s *recordSurface) Sub(tui.Rect) tui.Surface { return s }
func (s *recordSurface) Size() tui.Size           { return tui.Size{W: s.w, H: s.h} }
func (s *recordSurface) StringWidth(t string) int { return len([]rune(t)) }
func (s *recordSurface) Theme() *style.Theme      { th := style.DefaultTheme(); return &th }
func (s *recordSurface) Caps() tui.Capabilities   { return tui.Capabilities{} }
func (s *recordSurface) row(y int) string         { return strings.TrimRight(strings.Join(s.text[y], ""), " ") }

var _ tui.Surface = (*recordSurface)(nil)

var tokenNames = map[style.Token]string{
	style.TokenPrimary: "primary", style.TokenSecondary: "secondary", style.TokenForeground: "fg",
	style.TokenBackground: "background", style.TokenSurface: "surface", style.TokenPanel: "panel",
	style.TokenWarning: "warning", style.TokenError: "error", style.TokenSuccess: "success",
	style.TokenAccent: "accent", style.TokenBoost: "boost", style.TokenTextMuted: "muted",
	style.TokenTextOnWarning: "on-warning", style.TokenBorder: "border",
}

// styleWords is st as a golden says it: its attributes, then fg= and bg= by token.
func styleWords(st style.Style) string {
	var out []string
	for _, a := range []struct {
		name string
		get  func() (bool, bool)
	}{{"bold", st.GetBold}, {"italic", st.GetItalic}, {"underline", st.GetUnderline}, {"strike", st.GetStrikethrough}, {"reverse", st.GetReverse}} {
		if v, ok := a.get(); ok && v {
			out = append(out, a.name)
		}
	}
	color := func(prefix string, c style.Color, ok bool) {
		if !ok {
			return
		}
		name := "?"
		if t, isToken := c.Token(); isToken {
			if n, known := tokenNames[t]; known {
				name = n
			} else {
				name = fmt.Sprint("token", int(t))
			}
		}
		out = append(out, prefix+name)
	}
	fg, fok := st.GetForeground()
	color("fg=", fg, fok)
	bg, bok := st.GetBackground()
	color("bg=", bg, bok)
	return strings.Join(out, " ")
}

// cellGolden is how markdown src (or, raw, HTML) reads in cells at width: each row's text, then its
// runs of one style (columns from-to), blanks and default text left out. It measures a cluster by
// its runes (layCells).
func cellGolden(t *testing.T, src string, width int, raw bool) string {
	t.Helper()
	return cellGoldenMeasured(t, src, width, raw, nil)
}

// cellGoldenMeasured is cellGolden measured by measure, nil: by runes.
func cellGoldenMeasured(t *testing.T, src string, width int, raw bool, measure func(string) int) string {
	t.Helper()
	html := bytes.NewBufferString(src)
	if !raw {
		html.Reset()
		if err := mdhtml.Render(html, markdown.Parse([]byte(src), markdown.WithGFM(), markdown.WithObsidian())); err != nil {
			t.Fatal(err)
		}
	}
	var c *cellHTML
	if measure == nil {
		c = layCells(t, html.String(), width)
	} else {
		c = newCellHTML(measure)
		c.SetSource(html.Bytes())
		c.SetWidth(float32(width))
	}
	s := newRecordSurface(width, len(c.rows))
	c.PaintCells(s, 0, width, DocPos{}, DocPos{})
	var b strings.Builder
	fmt.Fprintf(&b, "width %d\n", width)
	for _, line := range strings.Split(strings.TrimRight(src, "\n"), "\n") {
		fmt.Fprintf(&b, "> %s\n", line)
	}
	b.WriteString("\n")
	for y := range s.h {
		fmt.Fprintf(&b, "%q\n", s.row(y))
		for x := 0; x < s.w; {
			d, end := styleWords(s.style[y][x]), x+1
			for end < s.w && styleWords(s.style[y][end]) == d {
				end++
			}
			if blank := strings.TrimSpace(strings.Join(s.text[y][x:end], "")) == ""; d != "" && d != "fg=fg" && !(blank && !strings.Contains(d, "bg=")) {
				fmt.Fprintf(&b, "    %d-%d %s\n", x, end-1, d)
			}
			x = end
		}
	}
	return b.String()
}

// TestCellHTMLGoldens: each element reads in cells the way it does in the window (golden per
// element, testdata/htmlcells; -update rewrites them): a table boxed, its header ruled off and its
// cells aligned, one too wide wrapped as rows; headings told apart by level; links underlined;
// inline code and code blocks set off; quotes barred, a callout's bar and title in its type's
// color; list markers by depth, a task's checkbox; rules, emphasis, strike and highlight.
func TestCellHTMLGoldens(t *testing.T) {
	for _, tc := range []struct {
		name  string
		width int
		src   string
		raw   bool // src is HTML (an element markdown has no syntax for)
	}{
		{"table", 40, "| Name | Qty | Note |\n| :--- | ---: | :---: |\n| apple | 3 | ripe |\n| kiwi | 12 | — |\n", false},
		{"table-too-wide", 20, "| Name | Description |\n| --- | --- |\n| apple | a long description of it |\n", false},
		{"headings", 30, "# One\n\n## Two\n\n### Three\n\n#### Four\n\n##### Five\n\n###### Six\n", false},
		{"link", 30, "see [the docs](docs.md) and <https://example.com> too\n", false},
		{"code", 30, "run `make all` now\n\n```go\nfunc main() {\n\tprintln(\"hi\")\n}\n```\n", false},
		{"blockquote", 30, "> quoted text\n> > nested quote\n", false},
		{"callouts", 34, "> [!note] A note\n> its body\n\n> [!warning] Careful\n> mind it\n\n> [!tip]\n> a tip\n\n> [!danger] Stop\n> no\n", false},
		{"lists", 30, "- one\n  - two\n    - three\n- four\n\n1. first\n2. second\n   1. inner\n", false},
		{"tasks", 30, "- [ ] to do\n- [x] done\n- plain\n", false},
		{"rule", 20, "above\n\n---\n\nbelow\n", false},
		{"emphasis", 40, "**bold** *italic* ~~struck~~ `code`\n", false},
		{"mark", 30, "<p>a <mark>marked</mark> word</p>", true},
		// wide clusters, measured as a terminal draws them (tui.StringWidth): the columns and their
		// box line up by display cells (TestCellHTMLWideTableLinesUp)
		{"table-wide", 40, "| 이름 | Qty | 메모 |\n| :--- | ---: | :---: |\n| 사과 | 3 | 익음 |\n| apple | 12 | — |\n| 東京 | 1 | 駅 |\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var measure func(string) int
			if strings.HasSuffix(tc.name, "-wide") {
				measure = tui.StringWidth
			}
			got := cellGoldenMeasured(t, tc.src, tc.width, tc.raw, measure)
			path := filepath.Join("testdata", "htmlcells", tc.name+".golden")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (go test -run TestCellHTMLGoldens -update writes it)", err)
			}
			if got != string(want) {
				t.Errorf("%s reads otherwise in cells:\n--- got\n%s\n--- want (%s)\n%s", tc.name, got, path, want)
			}
		})
	}
}

// TestCellHTMLWideTableLinesUp: a table of wide clusters (Korean, Chinese) beside ASCII, measured
// by display cells, puts every column rule in the same display column on every row, the box's
// joints with them.
func TestCellHTMLWideTableLinesUp(t *testing.T) {
	var html bytes.Buffer
	src := "| 이름 | Qty | 메모 |\n| :--- | ---: | :---: |\n| 사과 | 3 | 익음 |\n| apple | 12 | — |\n| 東京 | 1 | 駅 |\n"
	if err := mdhtml.Render(&html, markdown.Parse([]byte(src), markdown.WithGFM())); err != nil {
		t.Fatal(err)
	}
	c := newCellHTML(tui.StringWidth)
	c.SetSource(html.Bytes())
	c.SetWidth(40)
	var want []int
	for _, r := range c.rows {
		var at []int
		for _, k := range r.cls {
			if strings.ContainsAny(k.text, "│┌┬┐├┼┤└┴┘") {
				at = append(at, k.x)
			}
		}
		if len(at) == 0 {
			continue
		}
		if want == nil {
			want = at
		}
		if fmt.Sprint(at) != fmt.Sprint(want) {
			t.Errorf("a row's rules at columns %v, the first row's at %v", at, want)
		}
	}
	if len(want) != 4 {
		t.Fatalf("the box has rules at %v, want four: its edges and two joints", want)
	}
}
