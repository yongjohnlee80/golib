package view

import (
	"errors"
	"strings"
	"testing"
	tparse "text/template/parse"

	"github.com/yongjohnlee80/golib/parse/yaml"
)

const track = `---
version: 1
name: track_view
process: |
  SELECT 1
---
# {{.track}}
{{if .lyric}}{{.lyric}}{{end}}
`

func TestParse_SplitsFrontmatterAndBody(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte(track))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(f.Source[f.Open.Start:f.Open.End]); got != "---" {
		t.Errorf("open = %q", got)
	}
	if got := string(f.Source[f.Close.Start:f.Close.End]); got != "---" {
		t.Errorf("close = %q", got)
	}
	if got, want := string(f.Source[f.Front.Start:f.Front.End]), "version: 1\nname: track_view\nprocess: |\n  SELECT 1\n"; got != want {
		t.Errorf("front = %q, want %q", got, want)
	}
	if got, want := string(f.Source[f.Body.Start:f.Body.End]), "# {{.track}}\n{{if .lyric}}{{.lyric}}{{end}}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
	if f.Meta == nil || f.Meta.Root.Kind != yaml.KindMapping || len(f.Meta.Root.Pairs) != 3 {
		t.Fatalf("meta = %+v", f.Meta)
	}
	if got := string(f.Meta.Root.Pairs[2].Value.Value); got != "SELECT 1\n" {
		t.Errorf("process = %q", got)
	}
}

// The parser pads the body so the template's line numbers are the file's; the padding must not
// reach the tree, or every document would start with blank lines.
func TestParse_BodyTreeHoldsTheBodyExactly(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte(track))
	if err != nil {
		t.Fatal(err)
	}
	body := f.Templates[BodyTemplate]
	if body == nil {
		t.Fatal("no body template")
	}
	if got, want := body.Root.String(), "# {{.track}}\n{{if .lyric}}{{.lyric}}{{end}}\n"; got != want {
		t.Errorf("body tree = %q, want %q", got, want)
	}
	first, ok := body.Root.Nodes[0].(*tparse.TextNode)
	if !ok {
		t.Fatalf("first node = %T", body.Root.Nodes[0])
	}
	if pos := f.TemplatePosition(first.Pos); pos.Line != 7 || pos.Column != 1 {
		t.Errorf("first text at %s, want 7:1", pos)
	}
}

func TestParse_TemplatePositionIsTheFiles(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte(track), WithName("track.view"))
	if err != nil {
		t.Fatal(err)
	}
	var lyric *tparse.IfNode
	for _, n := range f.Templates[BodyTemplate].Root.Nodes {
		if n, ok := n.(*tparse.IfNode); ok {
			lyric = n
		}
	}
	if lyric == nil {
		t.Fatal("no if node")
	}
	// An action's Pos is its first token's, so {{.lyric}} after the 13 bytes of "{{if .lyric}}"
	// is at column 16, where ".lyric" begins.
	action := lyric.List.Nodes[0].(*tparse.ActionNode)
	if got := f.TemplatePosition(action.Pos).String(); got != "track.view:8:16" {
		t.Errorf("{{.lyric}} at %s, want track.view:8:16", got)
	}
}

// A body may open with its own frontmatter: only the first "---" pair is the view's.
func TestParse_BodyMayOpenWithItsOwnFrontmatter(t *testing.T) {
	t.Parallel()
	src := "---\nname: v\n---\n---\nid: \"{{.id}}\"\n---\n# {{.title}}\n"
	f, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := f.Templates[BodyTemplate].Root.String(), "---\nid: \"{{.id}}\"\n---\n# {{.title}}\n"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestParse_CRLFDelimiters(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte("---\r\nname: v\r\n---\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(f.Source[f.Body.Start:f.Body.End]); got != "body\r\n" {
		t.Errorf("body = %q", got)
	}
}

// "{{-" trims the whitespace before it, padding included, so nothing of the padding is left to strip.
func TestParse_TrimMarkerAtBodyStart(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte("---\nname: v\n---\n  {{- .x}} y"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := f.Templates[BodyTemplate].Root.String(), "{{.x}} y"; got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

func TestParse_EmptyPartsAndDefines(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		src         string
		meta        bool
		templates   []string
		bodyPrinted string
	}{
		"empty frontmatter":       {src: "---\n---\nbody", meta: false, templates: []string{BodyTemplate}, bodyPrinted: "body"},
		"empty body":              {src: "---\nname: v\n---\n", meta: true, templates: []string{BodyTemplate}},
		"closing line at the end": {src: "---\nname: v\n---", meta: true, templates: []string{BodyTemplate}},
		"defines":                 {src: "---\nname: v\n---\n{{define \"row\"}}{{.}}{{end}}{{template \"row\" .}}", meta: true, templates: []string{BodyTemplate, "row"}, bodyPrinted: `{{template "row" .}}`},
		"only defines":            {src: "---\nname: v\n---\n{{define \"row\"}}{{.}}{{end}}", meta: true, templates: []string{BodyTemplate, "row"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, err := Parse([]byte(tc.src))
			if err != nil {
				t.Fatal(err)
			}
			if (f.Meta != nil) != tc.meta {
				t.Errorf("meta = %v, want present %v", f.Meta, tc.meta)
			}
			if len(f.Templates) != len(tc.templates) {
				t.Errorf("templates = %v, want %v", f.Templates, tc.templates)
			}
			for _, n := range tc.templates {
				if f.Templates[n] == nil {
					t.Errorf("template %q missing", n)
				}
			}
			if got := f.Templates[BodyTemplate].Root.String(); got != tc.bodyPrinted {
				t.Errorf("body = %q, want %q", got, tc.bodyPrinted)
			}
		})
	}
}

// The body names functions no one has defined yet: which exist is the evaluator's decision.
func TestParse_FunctionNamesAreNotChecked(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte("---\nname: v\n---\n{{nosuch .x}}"))
	if err != nil {
		t.Fatal(err)
	}
	if f.Templates[BodyTemplate].Mode&tparse.SkipFuncCheck == 0 {
		t.Error("the body tree does not record that its functions are unchecked")
	}
}

func TestParse_Errors(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		src     string
		pos     string
		msg     string
		wrapped bool
	}{
		"no opening line":    {src: "name: v\n---\n", pos: "v.view:1:1", msg: "the first line must be"},
		"opening line only":  {src: "---", pos: "v.view:1:4", msg: "no closing"},
		"no closing line":    {src: "---\nname: v\nbody\n", pos: "v.view:4:1", msg: "no closing"},
		"indented closing":   {src: "---\nname: v\n ---\n", pos: "v.view:4:1", msg: "no closing"},
		"two yaml documents": {src: "---\na: 1\n...\n--- \nb: 2\n---\n", pos: "v.view:4:1", msg: "more than one YAML document"},
		"bad yaml":           {src: "---\nname: v\nargs: [a\n---\n", pos: "v.view:4:1", wrapped: true},
		"bad template":       {src: "---\nname: v\n---\nline 4\n{{if .x}}\n", pos: "v.view:4:1", msg: "body:6: unexpected EOF", wrapped: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse([]byte(tc.src), WithName("v.view"))
			var pe *Error
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want *Error", err)
			}
			if got := pe.Pos.String(); got != tc.pos {
				t.Errorf("pos = %s, want %s (%v)", got, tc.pos, err)
			}
			if !strings.Contains(pe.Msg, tc.msg) {
				t.Errorf("msg = %q, want it to contain %q", pe.Msg, tc.msg)
			}
			if (errors.Unwrap(err) != nil) != tc.wrapped {
				t.Errorf("unwrap = %v, want wrapped %v", errors.Unwrap(err), tc.wrapped)
			}
			if !strings.HasPrefix(err.Error(), "view: "+tc.pos+": ") {
				t.Errorf("error = %q", err.Error())
			}
		})
	}
}

func TestParse_YAMLErrorIsReportedAtTheFilesLine(t *testing.T) {
	t.Parallel()
	_, err := Parse([]byte("---\nname: v\nname2: : x\n---\n"))
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v", err)
	}
	var ye *yaml.Error
	if !errors.As(err, &ye) {
		t.Fatalf("err does not unwrap to the YAML error: %v", err)
	}
	if pe.Pos.Line != ye.Pos.Line+1 {
		t.Errorf("file line %d, frontmatter line %d: the file's should be one more", pe.Pos.Line, ye.Pos.Line)
	}
}

func TestParse_MaxDepth(t *testing.T) {
	t.Parallel()
	src := []byte("---\na: [[[1]]]\n---\n")
	if _, err := Parse(src); err != nil {
		t.Fatalf("default depth: %v", err)
	}
	if _, err := Parse(src, MaxDepth(2)); err == nil {
		t.Error("MaxDepth(2) accepted three levels of nesting")
	}
}

func TestPosition_CountsCharacters(t *testing.T) {
	t.Parallel()
	f, err := Parse([]byte("---\nname: ëë\n---\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.Position(len("---\nname: ëë")); got.Line != 2 || got.Column != 9 {
		t.Errorf("end of line 2 = %s, want 2:9", got)
	}
	if got := f.Position(-5); got.Line != 1 || got.Column != 1 {
		t.Errorf("a negative offset = %s, want 1:1", got)
	}
	if got := f.Position(1 << 20); got.Offset != len(f.Source) {
		t.Errorf("an offset past the end = %+v, want clamped to the end", got)
	}
	if got := f.MetaPosition(0); got.Line != 2 {
		t.Errorf("frontmatter offset 0 at line %d, want 2", got.Line)
	}
}
