package code

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/search"
	"github.com/yongjohnlee80/golib/search/chunk"
)

const goSample = `//go:build linux

// Package x does things.
package x

import (
	"errors"
	"fmt"
)

// a detached comment, not a doc

// Thing is a thing.
type Thing struct {
	Name string
}

// Get returns the value.
func (t *Thing) Get() (string, error) {
	if t == nil {
		return "", errors.New("nil")
	}
	return t.Name, nil
} // Get's trailing comment

// Set is a value-receiver method.
func (t Thing) Set(v string) { fmt.Println(v) }

// Map is generic.
func Map[T, U any](xs []T, f func(T) U) []U {
	out := make([]U, 0, len(xs))
	for _, x := range xs {
		out = append(out, f(x))
	}
	return out
}

//go:generate stringer -type=Kind

// Limits.
const (
	A = 1
	B = 2
)

var debug = false
`

const goGrouped = `package x

// Group doc.
type (
	// One is first.
	One int
	// Two is second.
	Two interface{ M() }
)
`

func goChunks(t *testing.T, src string, limit int) []search.Chunk {
	t.Helper()
	cs, err := Go{}.Chunk(search.Doc{Path: "pkg/x/x.go", Text: []byte(src), Tokens: limit})
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func TestGoUnitsAndBreadcrumbs(t *testing.T) {
	cs := goChunks(t, goSample, 0)
	header := cs[0]
	if header.Breadcrumb != "pkg/x" || !strings.HasPrefix(header.Body, "//go:build linux") || !strings.HasSuffix(header.Body, ")") {
		t.Errorf("header: %q %q", header.Breadcrumb, header.Body)
	}
	for _, crumb := range []string{"pkg/x > Thing (struct)", "pkg/x > Thing > Get", "pkg/x > Thing > Set", "pkg/x > Map", "pkg/x > const A, B", "pkg/x > var debug"} {
		chunkOf(t, cs, crumb)
	}
	get := chunkOf(t, cs, "Thing > Get")
	if !strings.HasPrefix(get.Body, "// Get returns the value.") || !strings.HasSuffix(get.Body, "// Get's trailing comment") {
		t.Errorf("Get's span does not run from its doc to its trailing comment: %q", get.Body)
	}
	// the detached comment and the go:generate directive are glue, each doc in exactly one unit
	var glue []string
	for _, c := range cs {
		if strings.HasSuffix(c.Breadcrumb, "(glue)") {
			glue = append(glue, c.Body)
		}
	}
	if len(glue) != 2 || glue[0] != "// a detached comment, not a doc" || glue[1] != "//go:generate stringer -type=Kind" {
		t.Errorf("glue = %q", glue)
	}
	docs := 0
	for _, c := range cs {
		docs += strings.Count(c.Body, "// Thing is a thing.")
	}
	if docs != 1 {
		t.Errorf("Thing's doc is in %d chunks", docs)
	}
}

func TestGoEmbedIsDocAndSignatureNotBody(t *testing.T) {
	get := chunkOf(t, goChunks(t, goSample, 0), "Thing > Get")
	if !strings.Contains(get.Embed, "Get returns the value.") {
		t.Errorf("doc missing from Embed: %q", get.Embed)
	}
	if !strings.Contains(get.Embed, "func (t *Thing) Get() (string, error)") {
		t.Errorf("signature missing from Embed: %q", get.Embed)
	}
	if strings.Contains(get.Embed, "if t == nil") || strings.Contains(get.Embed, "errors.New") {
		t.Errorf("body in Embed: %q", get.Embed)
	}
	thing := chunkOf(t, goChunks(t, goSample, 0), "Thing (struct)")
	if !strings.Contains(thing.Embed, "Name string") {
		t.Errorf("a type's Embed lacks its fields: %q", thing.Embed)
	}
}

func TestGoGroupedTypesPartitionTheGroup(t *testing.T) {
	cs, _ := Go{}.Chunk(search.Doc{Path: "pkg/x/types.go", Text: []byte(goGrouped)})
	one, two := chunkOf(t, cs, "> One"), chunkOf(t, cs, "Two (interface)")
	if !strings.HasPrefix(one.Body, "// Group doc.\ntype (") || !strings.HasSuffix(two.Body, ")") {
		t.Errorf("the group's tokens are not in its units: %q / %q", one.Body, two.Body)
	}
	if !strings.Contains(one.Embed, "Group doc.") || !strings.Contains(one.Embed, "One is first.") {
		t.Errorf("One's Embed: %q", one.Embed)
	}
}

func TestGoOversizedFunctionSplitsAtStatements(t *testing.T) {
	var b strings.Builder
	b.WriteString("package x\n\n// Big does a lot.\nfunc Big(n int) error {\n")
	for i := 0; i < 40; i++ {
		b.WriteString("\tv := step(n)\n\tif err := check(v); err != nil {\n\t\treturn err\n\t}\n")
	}
	b.WriteString("\treturn nil\n}\n")
	src := b.String()
	cs := goChunks(t, src, 80)
	partition(t, "big", []byte(src), cs)
	var frags []search.Chunk
	for _, c := range cs {
		if strings.Contains(c.Breadcrumb, "Big") {
			frags = append(frags, c)
		}
	}
	if len(frags) < 3 {
		t.Fatalf("Big did not split: %d fragments", len(frags))
	}
	for i, f := range frags {
		if !strings.Contains(f.Breadcrumb, "func Big(n int) error") {
			t.Errorf("fragment %d's breadcrumb lacks the signature: %q", i, f.Breadcrumb)
		}
		if i > 0 {
			if first := strings.TrimSpace(f.Body); !strings.HasPrefix(first, "v :=") && !strings.HasPrefix(first, "if ") && !strings.HasPrefix(first, "return") {
				t.Errorf("fragment %d does not begin at a statement: %q", i, first[:min(30, len(first))])
			}
			if strings.Contains(f.Embed, "!= nil") {
				t.Errorf("fragment %d embeds control flow: %q", i, f.Embed)
			}
			if !strings.Contains(f.Embed, "step") || !strings.Contains(f.Embed, "func Big") {
				t.Errorf("fragment %d's Embed lacks its calls or the signature: %q", i, f.Embed)
			}
		}
	}
	if !strings.Contains(frags[0].Embed, "Big does a lot.") {
		t.Errorf("first fragment's Embed lacks the doc: %q", frags[0].Embed)
	}
}

func TestGoThatDoesNotParseFallsBackToText(t *testing.T) {
	src := "package x\n\nfunc broken( {\n\nsome words here\n"
	cs := goChunks(t, src, 0)
	want := chunk.Text([]byte(src), "pkg/x/x.go", 0)
	if len(cs) != len(want) || cs[0].Body != want[0].Body {
		t.Errorf("fallback = %+v, want chunk.Text's %+v", cs, want)
	}
	if !strings.Contains(Go{}.Version(), chunk.Version) {
		t.Errorf("Go's version %q does not fold in the fallback's %q", Go{}.Version(), chunk.Version)
	}
}
