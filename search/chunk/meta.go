package chunk

import (
	"encoding/json"
	"fmt"
	"math"
	"path"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/parse/markdown"
	pyaml "github.com/yongjohnlee80/golib/parse/yaml"
	"github.com/yongjohnlee80/golib/yaml"
)

// Meta is what a document says about itself: its title, tags and aliases, and its frontmatter.
type Meta struct {
	Title           string
	Tags, Aliases   []string // sorted, de-duplicated; tags lowercased (Obsidian compares them so)
	FrontmatterJSON string   // the evaluated frontmatter, in source order; "" when there is none
	FrontmatterErr  string   // why the frontmatter did not parse or evaluate; the body indexes all the same
}

// ReadMeta reads a parsed note's metadata: the frontmatter's title (else the first top-level h1,
// else the file name of p), its tags and aliases, and the inline #tags of the body. Frontmatter is
// YAML 1.2 under the Core schema; one that does not parse or is not a mapping is recorded in
// FrontmatterErr, and the rest is still read.
func ReadMeta(doc *markdown.Document, p string) Meta {
	var m Meta
	tags := map[string]bool{}
	aliases := map[string]bool{}
	if fm := doc.Root.FirstChild; fm != nil && fm.Kind == markdown.KindFrontmatter {
		v, err := evalFrontmatter(fm.Literal)
		if err != nil {
			m.FrontmatterErr = err.Error()
		} else if mp, ok := v.(yaml.Map); ok {
			if t, ok := mp.Get("title"); ok {
				if s, ok := t.(string); ok {
					m.Title = strings.TrimSpace(s)
				}
			}
			for _, t := range stringList(get(mp, "tags")) {
				tags[strings.ToLower(strings.TrimPrefix(t, "#"))] = true
			}
			for _, a := range stringList(get(mp, "aliases")) {
				aliases[a] = true
			}
			b, _ := json.Marshal(toJSON(mp))
			m.FrontmatterJSON = string(b)
		} else if v != nil {
			m.FrontmatterErr = "the frontmatter is not a mapping"
		}
	}
	var walk func(*markdown.Node)
	walk = func(n *markdown.Node) {
		for c := n.FirstChild; c != nil; c = c.Next {
			if c.Kind == markdown.KindTag {
				tags[strings.ToLower(string(c.Label))] = true
			}
			if m.Title == "" && c.Kind == markdown.KindHeading && c.Level == 1 && c.Parent == doc.Root {
				m.Title = plainText(c, doc.Source)
			}
			walk(c)
		}
	}
	walk(doc.Root)
	if m.Title == "" {
		m.Title = strings.TrimSuffix(path.Base(p), path.Ext(p))
	}
	delete(tags, "")
	delete(aliases, "")
	m.Tags, m.Aliases = sortedKeys(tags), sortedKeys(aliases)
	return m
}

// evalFrontmatter reads frontmatter as YAML 1.2 under the Core schema. A stream of no documents is
// no metadata; more than one is an error.
func evalFrontmatter(raw []byte) (any, error) {
	st, err := pyaml.Parse(raw)
	if err != nil {
		return nil, err
	}
	switch len(st.Docs) {
	case 0:
		return nil, nil
	case 1:
		return yaml.Evaluate(st.Docs[0], yaml.Core)
	}
	return nil, fmt.Errorf("the frontmatter holds %d YAML documents, not one", len(st.Docs))
}

func get(m yaml.Map, k string) any { v, _ := m.Get(k); return v }

// stringList reads a list of strings, or one string (Obsidian writes both forms). Other scalars
// are written out; anything else is ignored.
func stringList(v any) []string {
	switch x := v.(type) {
	case nil:
		return nil
	case string:
		var out []string
		for _, f := range strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == ' ' }) {
			out = append(out, f)
		}
		return out
	case []any:
		var out []string
		for _, e := range x {
			switch s := e.(type) {
			case string:
				out = append(out, strings.TrimSpace(s))
			case int64, float64, bool:
				out = append(out, fmt.Sprint(s))
			}
		}
		return out
	}
	return nil
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// toJSON converts an evaluated YAML value to one encoding/json writes, keeping a mapping's entries
// in source order.
func toJSON(v any) any {
	switch x := v.(type) {
	case yaml.Map:
		return orderedObject(x)
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = toJSON(e)
		}
		return out
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return fmt.Sprint(x) // JSON has no NaN or infinity: keep it as text
		}
	}
	return v
}

type orderedObject yaml.Map

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	b.WriteByte('{')
	for i, it := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, ok := it.Key.(string)
		if !ok {
			k = fmt.Sprint(it.Key)
		}
		kb, _ := json.Marshal(k)
		vb, err := json.Marshal(toJSON(it.Value))
		if err != nil {
			return nil, err
		}
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return []byte(b.String()), nil
}
