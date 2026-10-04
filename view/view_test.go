package view

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pview "github.com/yongjohnlee80/golib/parse/view"
)

const trackView = `---
version: 1
name: track_view
source: postgres
args:
  - entity_ids
process: |
  SELECT jsonb_build_object('entity_id', t.id, 'track', t.title)
  FROM track t
  WHERE t.id = ANY($1)
export: tracks/track_{{.entity_id}}.md
destination: file:///var/export
---
# {{.track}}
`

func mustNew(t *testing.T, src string) *View {
	t.Helper()
	v, err := New([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestNew_ReadsTheFrontmatter(t *testing.T) {
	t.Parallel()
	v := mustNew(t, trackView)
	if v.Name() != "track_view" || v.Source() != "postgres" {
		t.Errorf("name, source = %q, %q", v.Name(), v.Source())
	}
	if got := v.Args(); len(got) != 1 || got[0] != "entity_ids" {
		t.Errorf("args = %v", got)
	}
	if want := "SELECT jsonb_build_object('entity_id', t.id, 'track', t.title)\nFROM track t\nWHERE t.id = ANY($1)\n"; v.Process() != want {
		t.Errorf("process = %q, want %q", v.Process(), want)
	}
	if v.Export() != "tracks/track_{{.entity_id}}.md" || v.Destination() != "file:///var/export" {
		t.Errorf("export, destination = %q, %q", v.Export(), v.Destination())
	}
	if v.Format() != FormatMarkdown {
		t.Errorf("format = %q, want markdown from the export's extension", v.Format())
	}
}

func TestView_ArgsIsACopy(t *testing.T) {
	t.Parallel()
	v := mustNew(t, trackView)
	v.Args()[0] = "changed"
	if v.Args()[0] != "entity_ids" {
		t.Error("changing the returned args changed the view")
	}
}

func TestNew_Format(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ fields, want string }{
		{"", FormatText},
		{"export: a.json", FormatJSON},
		{"export: a.JSON", FormatJSON},
		{"export: a.xml", FormatXML},
		{"export: a.html", FormatHTML},
		{"export: a.htm", FormatHTML},
		{"export: a.md", FormatMarkdown},
		{"export: a.markdown", FormatMarkdown},
		{"export: a.txt", FormatText},
		{"export: a.json\nformat: text", FormatText},
		{"format: xml", FormatXML},
	} {
		v := mustNew(t, "---\nversion: 1\nname: v\nsource: sqlite\nprocess: SELECT 1\n"+tc.fields+"\n---\n")
		if v.Format() != tc.want {
			t.Errorf("%q: format = %q, want %q", tc.fields, v.Format(), tc.want)
		}
	}
}

func TestNew_Rejects(t *testing.T) {
	t.Parallel()
	const ok = "version: 1\nname: v\nsource: sqlite\nprocess: SELECT 1\n"
	for name, tc := range map[string]struct {
		src  string
		want string // the error names this, and where
	}{
		"no frontmatter":      {"name: v\n", "1:1: the first line"},
		"empty frontmatter":   {"---\n---\n", "2:1: the frontmatter is empty"},
		"not a mapping":       {"---\n- a\n---\n", "must be a mapping"},
		"bad yaml value":      {"---\n" + ok + "x: !!int abc\n---\n", "frontmatter:"},
		"non-string key":      {"---\n" + ok + "1: x\n---\n", "6:1: frontmatter keys must be plain names"},
		"unknown field":       {"---\n" + ok + "proccess: x\n---\n", "6:1: proccess: unknown field"},
		"version missing":     {"---\nname: v\nsource: sqlite\nprocess: SELECT 1\n---\n", "2:1: version: want 1, have 0"},
		"version 2":           {"---\nversion: 2\nname: v\nsource: sqlite\nprocess: SELECT 1\n---\n", "2:1: version: want 1, have 2"},
		"version a string":    {"---\nversion: one\n---\n", "2:1: version: want an integer"},
		"name missing":        {"---\nversion: 1\nsource: sqlite\nprocess: SELECT 1\n---\n", "name is required"},
		"name blank":          {"---\nversion: 1\nname: \" \"\nsource: sqlite\nprocess: SELECT 1\n---\n", "3:1: name is required"},
		"name a number":       {"---\nversion: 1\nname: 7\n---\n", "3:1: name: want a string"},
		"source missing":      {"---\nversion: 1\nname: v\nprocess: SELECT 1\n---\n", "source is required"},
		"process missing":     {"---\nversion: 1\nname: v\nsource: sqlite\n---\n", "process is required"},
		"process templated":   {"---\nversion: 1\nname: v\nsource: sqlite\nprocess: SELECT {{.id}}\n---\n", "5:1: process is not a template"},
		"args not a list":     {"---\n" + ok + "args: a\n---\n", "args: want a list of names"},
		"args holds a number": {"---\n" + ok + "args: [a, 1]\n---\n", "args: want a list of names"},
		"args blank name":     {"---\n" + ok + "args: [a, \"\"]\n---\n", "args: want a list of names"},
		"args twice":          {"---\n" + ok + "args: [a, a]\n---\n", `args: "a" is declared twice`},
		"destination no URI":  {"---\n" + ok + "destination: /var/export\n---\n", "6:1: destination: want a URI"},
		"destination bad URI": {"---\n" + ok + "destination: \"gs://b%zz\"\n---\n", "destination: want a URI"},
		"unknown format":      {"---\n" + ok + "format: pdf\n---\n", `6:1: format: unknown format "pdf"`},
		"bad export template": {"---\n" + ok + "export: \"{{.id\"\n---\n", "6:1: export:"},
		"bad body template":   {"---\n" + ok + "---\n{{if .x}}\n", "template: body:"},
		"unknown function":    {"---\n" + ok + "---\nline 7\n  {{.x | nosuch}}\n", `8:10: function "nosuch" is not defined`},
		"unknown in a branch": {"---\n" + ok + "---\n{{if nosuch}}x{{end}}", `function "nosuch" is not defined`},
		"unknown in a define": {"---\n" + ok + "---\n{{define \"r\"}}{{nosuch}}{{end}}", `function "nosuch" is not defined`},
		"ambiguous html":      {"---\n" + ok + "format: html\n---\n<a {{if .x}}href=\"{{else}}title=\"{{end}}{{.y}}\">", `branches end in different contexts`},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := New([]byte(tc.src))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestNew_ParserErrorsUnwrap(t *testing.T) {
	t.Parallel()
	_, err := New([]byte("---\nname: v\n"))
	var pe *pview.Error
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want it to unwrap to *parse/view.Error", err)
	}
}

// Every function a body may call is accepted at load: this package's and text/template's own.
func TestNew_AcceptsEveryProvidedFunction(t *testing.T) {
	t.Parallel()
	body := `{{toJson .}}{{quote .x}}{{raw .x}}{{trim .x}}{{upper .x}}{{lower .x}}{{default "d" .x}}` +
		`{{len .}}{{index . "x"}}{{printf "%v" .x}}{{if and .x (not .y) (eq .x "a")}}{{end}}`
	mustNew(t, "---\nversion: 1\nname: v\nsource: sqlite\nprocess: SELECT 1\n---\n"+body)
}

func TestLoad(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	good := filepath.Join(dir, "track.view")
	if err := os.WriteFile(good, []byte(trackView), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := Load(good)
	if err != nil {
		t.Fatal(err)
	}
	if v.Name() != "track_view" {
		t.Errorf("name = %q", v.Name())
	}

	bad := filepath.Join(dir, "bad.view")
	if err := os.WriteFile(bad, []byte("---\nversion: 1\nname: v\nsource: x\nprocess: y\nextra: 1\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(bad); err == nil || !strings.Contains(err.Error(), bad+":6:1: extra: unknown field") {
		t.Errorf("err = %v, want the file name and line", err)
	}

	if _, err := Load(filepath.Join(dir, "missing.view")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing file: err = %v", err)
	}
}

func TestLoad_Testdata(t *testing.T) {
	t.Parallel()
	v, err := Load("testdata/track.view")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := v.Parse([]byte(`{"entity_id":"trk_01","track":"Alone","label_title":"Monstercat","bpm":142,"music_key":"F minor","isrc":"US1234567890","release_date":"2024-05-01","lyric":"Say \"hi\"\nagain"}`))
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/track.golden.md")
	if err != nil {
		t.Fatal(err)
	}
	if doc != string(want) {
		t.Errorf("rendered:\n%s\nwant:\n%s", doc, want)
	}
}
