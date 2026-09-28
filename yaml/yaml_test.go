package yaml_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	pyaml "github.com/yongjohnlee80/golib/parse/yaml"
	"github.com/yongjohnlee80/golib/yaml"
)

func doc(t *testing.T, src string) *pyaml.Document {
	t.Helper()
	st, err := pyaml.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	if len(st.Docs) == 0 {
		return nil
	}
	return st.Docs[0]
}

// TestSchemaTables walks chapter 10's tables: how each schema resolves each plain scalar, and the
// value it constructs.
func TestSchemaTables(t *testing.T) {
	type row struct {
		in       string
		json     any // the JSON schema's value, or errJSON when it rejects the scalar
		core     any
		failsafe bool // Failsafe resolves it (only non-plain scalars; "?" stays unresolved)
	}
	errJSON := errors.New("rejected")
	nan := math.NaN()
	for _, r := range []row{
		{"null", nil, nil, false},
		{"Null", errJSON, nil, false},
		{"NULL", errJSON, nil, false},
		{"~", errJSON, nil, false},
		{"---\n", errJSON, nil, false}, // an empty plain scalar
		{"true", true, true, false},
		{"True", errJSON, true, false},
		{"FALSE", errJSON, false, false},
		{"0", int64(0), int64(0), false},
		{"-12", int64(-12), int64(-12), false},
		{"+12", errJSON, int64(12), false},
		{"012", errJSON, int64(12), false},
		{"0o14", errJSON, int64(12), false},
		{"0x1F", errJSON, int64(31), false},
		{"1.5", 1.5, 1.5, false},
		{"-1e3", -1000.0, -1000.0, false},
		{".5", errJSON, 0.5, false},
		{"1.", 1.0, 1.0, false},
		{".inf", errJSON, math.Inf(1), false},
		{"-.Inf", errJSON, math.Inf(-1), false},
		{".NaN", errJSON, nan, false},
		{"yes", errJSON, "yes", false},
		{"0o9", errJSON, "0o9", false},
		{"1_000", errJSON, "1_000", false},
		{"'true'", "true", "true", true},
		{"\"12\"", "12", "12", true},
		{"!!str 12", "12", "12", true},
		{"!!int \"12\"", int64(12), int64(12), true},
		{"!!float 3", 3.0, 3.0, true},
		{"! 12", "12", "12", true},
	} {
		d := doc(t, r.in)
		for _, c := range []struct {
			schema yaml.Schema
			want   any
		}{{yaml.JSON, r.json}, {yaml.Core, r.core}} {
			got, err := yaml.Evaluate(d, c.schema)
			if c.want == errJSON {
				if err == nil {
					t.Errorf("%q under JSON = %#v, want an error: no JSON format matches", r.in, got)
				}
				continue
			}
			if err != nil {
				t.Errorf("%q under schema %d: %v", r.in, c.schema, err)
				continue
			}
			if f, ok := c.want.(float64); ok && math.IsNaN(f) {
				if g, ok := got.(float64); !ok || !math.IsNaN(g) {
					t.Errorf("%q under schema %d = %#v, want NaN", r.in, c.schema, got)
				}
				continue
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("%q under schema %d = %#v, want %#v", r.in, c.schema, got, c.want)
			}
		}
		tags, err := yaml.Resolve(d, yaml.Failsafe)
		if err != nil {
			t.Fatal(err)
		}
		if _, resolved := tags[d.Root]; resolved != r.failsafe {
			t.Errorf("%q under Failsafe: resolved %v, want %v", r.in, resolved, r.failsafe)
		}
	}
}

func TestFailsafe(t *testing.T) {
	d := doc(t, "a: 1\nb: [x, 'y']\n")
	if _, err := yaml.Evaluate(d, yaml.Failsafe); !errors.Is(err, yaml.ErrFailsafe) {
		t.Errorf("Evaluate(Failsafe) = %v, want ErrFailsafe", err)
	}
	tags, err := yaml.Resolve(d, yaml.Failsafe)
	if err != nil {
		t.Fatal(err)
	}
	seq := d.Root.Pairs[1].Value
	if _, ok := tags[d.Root]; ok {
		t.Error("an untagged mapping has the ? tag: Failsafe leaves it unresolved")
	}
	if tags[seq.Items[1]] != yaml.TagStr {
		t.Errorf("a quoted scalar has the ! tag: Failsafe resolves it to str, got %q", tags[seq.Items[1]])
	}
	if _, ok := tags[seq.Items[0]]; ok {
		t.Error("a plain scalar stays unresolved under Failsafe")
	}
}

func TestExplicitTags(t *testing.T) {
	for _, in := range []string{"!!int abc", "!!bool yes", "!!binary R0lG", "!local x", "!!seq x", "!!map [a]"} {
		if v, err := yaml.Evaluate(doc(t, in), yaml.Core); err == nil {
			t.Errorf("%q = %#v, want an error: the tag or its content is outside the Core schema", in, v)
		}
	}
}

func TestIntegerRange(t *testing.T) {
	for _, in := range []string{"9223372036854775808", "-9223372036854775809", "0x8000000000000000", "0o1000000000000000000000"} {
		var ye *yaml.Error
		if _, err := yaml.Evaluate(doc(t, in), yaml.Core); !errors.As(err, &ye) || ye.Pos.Line != 1 {
			t.Errorf("%s: %v, want a positioned error: beyond int64", in, err)
		}
	}
	if v, err := yaml.Evaluate(doc(t, "-9223372036854775808"), yaml.Core); err != nil || v != int64(math.MinInt64) {
		t.Errorf("MinInt64 = %v, %v", v, err)
	}
}

// TestDuplicateKeys uses node equality after resolution: the same tag and canonical form, with a
// mapping compared as a set.
func TestDuplicateKeys(t *testing.T) {
	for _, c := range []struct {
		in  string
		dup bool
	}{
		{"1: a\n0x1: b\n", true},
		{"1: a\n0o1: b\n", true},
		{"1: a\n1.0: b\n", false},
		{"1: a\n'1': b\n", false},
		{"null: a\n~: b\n", true},
		{"? {a: 1, b: 2}\n: x\n? {b: 2, a: 1}\n: y\n", true},
		{"? [a, b]\n: x\n? [b, a]\n: y\n", false},
		{"&k a: 1\n*k : 2\n", true},
		{".nan: a\n.NaN: b\n", true},
	} {
		_, err := yaml.Evaluate(doc(t, c.in), yaml.Core)
		var ye *yaml.Error
		isDup := errors.As(err, &ye) && ye.Other != nil
		if isDup != c.dup {
			t.Errorf("%q: %v; want duplicate %v", c.in, err, c.dup)
		}
		if isDup && (ye.Pos.Line == ye.Other.Line) {
			t.Errorf("%q: the duplicate names %d:%d and %d:%d, want both keys' lines", c.in, ye.Pos.Line, ye.Pos.Column, ye.Other.Line, ye.Other.Column)
		}
	}
}

func TestAliases(t *testing.T) {
	v, err := yaml.Evaluate(doc(t, "base: &b {x: 1}\nuse: *b\n"), yaml.Core)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(yaml.Map)
	if u, _ := m.Get("use"); !reflect.DeepEqual(u, yaml.Map{{Key: "x", Value: int64(1)}}) {
		t.Errorf("use = %#v", u)
	}
	// a cycle: the anchor precedes the alias, but the alias is inside the anchored node
	if _, err := yaml.Evaluate(doc(t, "&a [1, *a]\n"), yaml.Core); err == nil {
		t.Error("a sequence containing itself constructed, want an error")
	}
}

// TestBillionLaughs: each level doubles the nodes an alias expands to; MaxNodes stops it before it
// is built.
func TestBillionLaughs(t *testing.T) {
	var b strings.Builder
	b.WriteString("a0: &a0 [lol, lol]\n")
	for i := 1; i <= 40; i++ {
		b.WriteString("a" + strconv.Itoa(i) + ": &a" + strconv.Itoa(i) + " [*a" + strconv.Itoa(i-1) + ", *a" + strconv.Itoa(i-1) + "]\n")
	}
	d := doc(t, b.String())
	var ye *yaml.Error
	if _, err := yaml.Evaluate(d, yaml.Core); !errors.As(err, &ye) || !strings.Contains(ye.Msg, "more than") {
		t.Errorf("2^40 expansions: %v, want the MaxNodes error", err)
	}
	// the mapping, two keys, [x, x] (3), the outer sequence, and each of three aliases again (3 each)
	const small = "a: &a [x, x]\nb: [*a, *a, *a]\n"
	if _, err := yaml.Evaluate(doc(t, small), yaml.Core, yaml.MaxNodes(15)); err == nil {
		t.Error("16 nodes under MaxNodes(15) constructed")
	}
	if _, err := yaml.Evaluate(doc(t, small), yaml.Core, yaml.MaxNodes(16)); err != nil {
		t.Errorf("16 nodes under MaxNodes(16): %v", err)
	}
}

// TestFrontmatter evaluates a note's frontmatter as AutoDoc reads it: title, tags, aliases, a date.
func TestFrontmatter(t *testing.T) {
	fm := "title: The design of AutoDoc\ntags:\n  - autodoc\n  - design\naliases: [AutoDoc design, AD]\ncreated: 2026-09-28\ndraft: false\n"
	v, err := yaml.Evaluate(doc(t, fm), yaml.Core)
	if err != nil {
		t.Fatal(err)
	}
	m := v.(yaml.Map)
	title, _ := m.Get("title")
	tags, _ := m.Get("tags")
	aliases, _ := m.Get("aliases")
	created, _ := m.Get("created")
	draft, _ := m.Get("draft")
	if title != "The design of AutoDoc" || !reflect.DeepEqual(tags, []any{"autodoc", "design"}) ||
		!reflect.DeepEqual(aliases, []any{"AutoDoc design", "AD"}) || created != "2026-09-28" || draft != false {
		t.Errorf("frontmatter = %#v", m)
	}
	if _, ok := m.Get("missing"); ok {
		t.Error("Get found a key that is not there")
	}
}

// TestFrontmatterFromMarkdown is AutoDoc's path: parse/markdown hands over a note's frontmatter
// raw, parse/yaml parses it, and Evaluate reads it under the Core schema.
func TestFrontmatterFromMarkdown(t *testing.T) {
	note := "---\ntitle: Weekly review\ntags: [review, weekly]\n---\n# Weekly review\n"
	md := markdown.Parse([]byte(note), markdown.Obsidian())
	fm := md.Root.FirstChild
	if fm == nil || fm.Kind != markdown.KindFrontmatter {
		t.Fatalf("no frontmatter node: %v", fm)
	}
	st, err := pyaml.Parse(fm.Literal)
	if err != nil {
		t.Fatal(err)
	}
	v, err := yaml.Evaluate(st.Docs[0], yaml.Core)
	if err != nil {
		t.Fatal(err)
	}
	title, _ := v.(yaml.Map).Get("title")
	tags, _ := v.(yaml.Map).Get("tags")
	if title != "Weekly review" || !reflect.DeepEqual(tags, []any{"review", "weekly"}) {
		t.Errorf("frontmatter = %#v", v)
	}
}

// TestYAMLTestSuiteJSON: every valid suite test with in.json evaluates, document by document under
// the Core schema, to that JSON. (A few error tests carry an in.json too; an ill-formed input has no
// value, so they are the parser's tests only.) The suite constructs a tag it does not define by the node's kind, as
// a caller that knows more tags does; the harness does the same by giving those nodes the
// non-specific "!" before Evaluate.
func TestYAMLTestSuiteJSON(t *testing.T) {
	suite := filepath.Join("..", "parse", "yaml", "testdata", "yaml-test-suite")
	paths, _ := filepath.Glob(filepath.Join(suite, "*", "in.json"))
	subs, _ := filepath.Glob(filepath.Join(suite, "*", "*", "in.json"))
	paths = append(paths, subs...)
	sort.Strings(paths)
	passed, ran := 0, 0
	for _, jp := range paths {
		dir := filepath.Dir(jp)
		id, _ := filepath.Rel(suite, dir)
		if _, err := os.Stat(filepath.Join(dir, "error")); err == nil {
			continue
		}
		in, _ := os.ReadFile(filepath.Join(dir, "in.yaml"))
		want, _ := os.ReadFile(jp)
		st, err := pyaml.Parse(in)
		if err != nil {
			t.Errorf("%s: %v", id, err)
			continue
		}
		var got []any
		for _, d := range st.Docs {
			byKindForUnknownTags(d.Root, map[*pyaml.Node]bool{})
			v, err := yaml.Evaluate(d, yaml.Core)
			if err != nil {
				t.Errorf("%s: %v", id, err)
				got = nil
				break
			}
			got = append(got, v)
		}
		ran++
		wantVals, err := decodeAll(want)
		if err != nil {
			t.Fatalf("%s: in.json: %v", id, err)
		}
		if len(got) != len(wantVals) {
			t.Errorf("%s: %d documents, in.json has %d", id, len(got), len(wantVals))
			continue
		}
		ok := true
		for i := range got {
			if !sameAsJSON(got[i], wantVals[i]) {
				t.Errorf("%s: document %d = %#v, in.json says %s", id, i, got[i], want)
				ok = false
			}
		}
		if ok {
			passed++
		}
	}
	t.Logf("yaml-test-suite in.json: %d of %d valid tests evaluate to their JSON", passed, ran)
	if passed != ran {
		t.Errorf("%d in.json tests fail", ran-passed)
	}
}

var coreTags = map[string]bool{yaml.TagStr: true, yaml.TagSeq: true, yaml.TagMap: true, yaml.TagNull: true,
	yaml.TagBool: true, yaml.TagInt: true, yaml.TagFloat: true, "!": true, "": true}

func byKindForUnknownTags(n *pyaml.Node, seen map[*pyaml.Node]bool) {
	if n == nil || seen[n] {
		return
	}
	seen[n] = true
	if !coreTags[n.Tag] {
		n.Tag = "!"
	}
	for _, c := range n.Items {
		byKindForUnknownTags(c, seen)
	}
	for _, p := range n.Pairs {
		byKindForUnknownTags(p.Key, seen)
		byKindForUnknownTags(p.Value, seen)
	}
}

func decodeAll(b []byte) ([]any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var out []any
	for {
		var v any
		if err := dec.Decode(&v); err == io.EOF {
			return out, nil
		} else if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
}

func sameAsJSON(got, want any) bool {
	switch w := want.(type) {
	case nil:
		return got == nil
	case bool, string:
		return got == w
	case json.Number:
		switch g := got.(type) {
		case int64:
			i, err := w.Int64()
			return err == nil && i == g
		case float64:
			f, err := w.Float64()
			return err == nil && f == g
		}
		return false
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !sameAsJSON(g[i], w[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		g, ok := got.(yaml.Map)
		if !ok || len(g) != len(w) {
			return false
		}
		for _, it := range g {
			k, ok := it.Key.(string)
			if !ok {
				return false
			}
			if wv, ok := w[k]; !ok || !sameAsJSON(it.Value, wv) {
				return false
			}
		}
		return true
	}
	return false
}
