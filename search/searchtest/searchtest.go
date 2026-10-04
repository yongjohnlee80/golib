package searchtest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/search"
)

// Fixture is a consumer's store over the corpus, as Run's open returns it.
type Fixture[D comparable, V search.View[D]] struct {
	Store search.Store[D, V]
	// Model is the active model's fingerprint, for a store whose views are Semantic.
	Model string
	// Change replaces one document's chunks, through the store's own write path, and returns once
	// the change is committed. Run calls it while a View is open, to check that the View keeps its
	// snapshot.
	Change func(path string, chunks []string)
}

// Run checks a store against the search port's contract. open indexes docs (Corpus()) through the
// consumer's own write path, storing Embed(body) as each chunk's vector, and returns the store. It
// is called once. The checks:
//
//   - Lexical: matches, n honoured, presentation fields of the document matched;
//   - Filter semantics: tags ANDed; paths ORed, a directory admitting only what is under it, ""
//     or "." meaning none; facet fields ANDed and values ORed; all applied before the limit;
//   - equal ranks ordered by path, the same every time;
//   - one snapshot per View: a change committed while a View is open is not seen in it, by any
//     retriever or presentation field, and is seen by the next View;
//   - Semantic, when the view type has it: the nearest chunks by Embed, filters before the limit,
//     ErrModelChanged on another model, no hit from a document that is not ready or a chunk that
//     is gone, and StatePartial read while a document is not ready;
//   - Signaler and Lister, when the view type has them, consistent with the corpus.
//
// The suite tests the port from outside, so it cannot produce a store's internal races; a store
// with an in-memory index owes its own cells for those.
func Run[D comparable, V search.View[D]](t testing.TB, open func(t testing.TB, docs []Doc) Fixture[D, V]) {
	t.Helper()
	f := open(t, Corpus())
	c := &checker[D, V]{t: t, f: f}
	c.lexical()
	c.filters()
	c.ties()
	if implements[V, search.Semantic[D]]() {
		c.semantic()
	}
	if implements[V, search.Signaler[D]]() {
		c.signals()
	}
	if implements[V, search.Lister[D]]() {
		c.list()
	}
	c.snapshot() // last: it changes the store
}

type checker[D comparable, V search.View[D]] struct {
	t testing.TB
	f Fixture[D, V]
}

// view runs fn in one View; a store error fails the run.
func (c *checker[D, V]) view(what string, fn func(v V) error) {
	c.t.Helper()
	if err := c.f.Store.View(context.Background(), fn); err != nil {
		c.t.Fatalf("searchtest: %s: %v", what, err)
	}
}

func terms(words ...string) []search.Term {
	var out []search.Term
	for _, w := range words {
		out = append(out, search.Term{Text: w})
	}
	return out
}

func paths[D comparable](cs []search.Candidate[D]) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Path)
	}
	return out
}

func sortedPaths[D comparable](cs []search.Candidate[D]) []string {
	p := paths(cs)
	slices.Sort(p)
	return slices.Compact(p)
}

func (c *checker[D, V]) lexicalOf(what string, words []string, f search.Filter, n int) []search.Candidate[D] {
	c.t.Helper()
	var out []search.Candidate[D]
	c.view(what, func(v V) error {
		var err error
		out, err = v.Lexical(context.Background(), terms(words...), f, n)
		return err
	})
	return out
}

func (c *checker[D, V]) lexical() {
	c.t.Helper()
	got := c.lexicalOf("lexical", []string{"quindle"}, search.Filter{}, 10)
	if len(got) != 1 || got[0].Path != "keep/k1.md" || got[0].Ord != 1 {
		c.t.Errorf("searchtest: lexical: quindle found %+v, want keep/k1.md's chunk 1", got)
	} else if !strings.Contains(stripMarks(got[0].Snippet), "quindle") || got[0].ByteStart > got[0].ByteEnd {
		c.t.Errorf("searchtest: lexical: the hit's snippet %q or span %d:%d does not present the chunk", got[0].Snippet, got[0].ByteStart, got[0].ByteEnd)
	}
	if got := c.lexicalOf("lexical", []string{"zorblax"}, search.Filter{}, 100); len(got) != 34 {
		c.t.Errorf("searchtest: lexical: zorblax found %d chunks, want 34", len(got))
	}
	if got := c.lexicalOf("lexical", []string{"zorblax"}, search.Filter{}, 5); len(got) != 5 {
		c.t.Errorf("searchtest: lexical: n = 5 answered %d", len(got))
	}
	if got := c.lexicalOf("lexical", []string{"zorblax", "quindle"}, search.Filter{}, 10); len(got) != 0 {
		c.t.Errorf("searchtest: lexical: every term must match: %v", paths(got))
	}
	if got := c.lexicalOf("lexical", []string{"glimmer"}, search.Filter{}, 10); len(got) != 1 {
		c.t.Errorf("searchtest: lexical: a document not ready for vectors still answers by words: %v", paths(got))
	}
}

// filterCases are the filter semantics, each with the documents it admits for zorblax.
var filterCases = []struct {
	name string
	f    search.Filter
	want []string
}{
	{"a tag", search.Filter{Tags: []string{"keep"}}, []string{"keep/k1.md", "keep/k2.md", "keep/k3.md", "keeper/k4.md"}},
	{"tags are ANDed", search.Filter{Tags: []string{"keep", "shared"}}, []string{"keep/k1.md", "keep/k3.md"}},
	{"a directory admits only what is under it", search.Filter{Paths: []string{"keep"}}, []string{"keep/k1.md", "keep/k2.md", "keep/k3.md"}},
	{"paths are ORed, a file or a directory", search.Filter{Paths: []string{"keep/k1.md", "keeper"}}, []string{"keep/k1.md", "keeper/k4.md"}},
	{"a facet value", search.Filter{Facets: map[string][]string{"kind": {"keep"}}}, []string{"keep/k1.md", "keep/k2.md", "keep/k3.md", "keeper/k4.md"}},
	{"facet values are ORed", search.Filter{Facets: map[string][]string{"level": {"one", "two"}}}, []string{"keep/k1.md", "keep/k2.md"}},
	{"facet fields are ANDed", search.Filter{Facets: map[string][]string{"kind": {"keep"}, "level": {"one"}}}, []string{"keep/k1.md"}},
	{"all of them together", search.Filter{Tags: []string{"shared"}, Paths: []string{"keep"}, Facets: map[string][]string{"kind": {"keep"}}}, []string{"keep/k1.md", "keep/k3.md"}},
	{"nothing admitted", search.Filter{Tags: []string{"keep", "noise"}}, nil},
}

func (c *checker[D, V]) filters() {
	c.t.Helper()
	for _, fc := range filterCases {
		// the limit is the number admitted: a filter applied after the limit leaves none of them,
		// since thirty noise chunks rank above every one
		n := max(len(fc.want), 1)
		if got := sortedPaths(c.lexicalOf("filters", []string{"zorblax"}, fc.f, n)); !slices.Equal(got, fc.want) {
			c.t.Errorf("searchtest: filters: %s: lexical admitted %v, want %v (filters apply before the limit)", fc.name, got, fc.want)
		}
	}
	for _, none := range [][]string{{"", "keep"}, {"keep", "."}} {
		if got := c.lexicalOf("filters", []string{"zorblax"}, search.Filter{Paths: none}, 100); len(got) != 34 {
			c.t.Errorf("searchtest: filters: paths %q mean no path filter: %d chunks, want 34", none, len(got))
		}
	}
}

func (c *checker[D, V]) ties() {
	c.t.Helper()
	want := []string{"tie/a.md", "tie/b.md", "tie/c.md"}
	for range 3 {
		if got := paths(c.lexicalOf("ties", []string{"plinth"}, search.Filter{}, 10)); !slices.Equal(got, want) {
			c.t.Errorf("searchtest: ties: equal ranks came out %v, want path order %v", got, want)
			return
		}
	}
}

func (c *checker[D, V]) semanticOf(what, model string, words string, f search.Filter, n int) ([]search.Candidate[D], error) {
	c.t.Helper()
	var out []search.Candidate[D]
	var serr error
	c.view(what, func(v V) error {
		out, serr = any(v).(search.Semantic[D]).Semantic(context.Background(), model, Embed(words), f, n)
		return nil
	})
	return out, serr
}

func (c *checker[D, V]) semantic() {
	c.t.Helper()
	got, err := c.semanticOf("semantic", c.f.Model, "second chunk quindle", search.Filter{}, 3)
	if err != nil || len(got) == 0 || got[0].Path != "keep/k1.md" || got[0].Ord != 1 {
		c.t.Errorf("searchtest: semantic: the nearest to its own words is %v, %v; want keep/k1.md's chunk 1", paths(got), err)
	}
	if len(got) > 3 {
		c.t.Errorf("searchtest: semantic: n = 3 answered %d", len(got))
	}
	if got, err := c.semanticOf("semantic", c.f.Model, "zorblax", search.Filter{Paths: []string{"keep"}}, 3); err != nil || !slices.Equal(sortedPaths(got), []string{"keep/k1.md", "keep/k2.md", "keep/k3.md"}) {
		c.t.Errorf("searchtest: semantic: filtered to keep/ with n = 3: %v, %v (filters apply before the limit)", paths(got), err)
	}
	if got, err := c.semanticOf("semantic", c.f.Model, "glimmer late text", search.Filter{}, 50); err != nil || slices.Contains(paths(got), "late/unready.md") {
		c.t.Errorf("searchtest: semantic: a document not ready answered: %v, %v", paths(got), err)
	}
	if _, err := c.semanticOf("semantic", "another|model||64", "zorblax", search.Filter{}, 3); !errors.Is(err, search.ErrModelChanged) {
		c.t.Errorf("searchtest: semantic: another model answered %v, want ErrModelChanged", err)
	}
	got, _ = c.semanticOf("semantic", c.f.Model, "plinth", search.Filter{}, 3)
	if !slices.Equal(paths(got), []string{"tie/a.md", "tie/b.md", "tie/c.md"}) {
		c.t.Errorf("searchtest: semantic: equal dot products came out %v, want path order", paths(got))
	}
	c.view("semantic", func(v V) error {
		st, err := any(v).(search.Semantic[D]).SemanticState(context.Background())
		if err != nil || st != search.StatePartial {
			c.t.Errorf("searchtest: semantic: the state with a document not ready is %q, %v; want partial", st, err)
		}
		return nil
	})
}

func (c *checker[D, V]) signals() {
	c.t.Helper()
	c.view("signals", func(v V) error {
		ids := map[string]D{}
		for _, p := range []string{"links/hub.md", "links/s2.md", "keep/k1.md"} {
			got, err := v.Lexical(context.Background(), terms(map[string]string{"links/hub.md": "hub", "links/s2.md": "two", "keep/k1.md": "quindle"}[p]), search.Filter{Paths: []string{p}}, 1)
			if err != nil || len(got) != 1 {
				c.t.Fatalf("searchtest: signals: finding %s: %v, %v", p, paths(got), err)
			}
			ids[p] = got[0].Doc
		}
		sig, err := any(v).(search.Signaler[D]).Signals(context.Background(), []D{ids["links/hub.md"], ids["links/s2.md"], ids["keep/k1.md"]})
		if err != nil {
			return err
		}
		if s := sig[ids["links/hub.md"]]; s.InLinks != 2 {
			c.t.Errorf("searchtest: signals: hub has %d in-links, want 2 (a repeat counts once)", s.InLinks)
		}
		if s := sig[ids["links/s2.md"]]; s.InLinks != 0 {
			c.t.Errorf("searchtest: signals: s2 has %d in-links, want 0 (its link to itself does not count)", s.InLinks)
		}
		if s := sig[ids["keep/k1.md"]]; !slices.Equal(sortedCopy(s.Tags), []string{"keep", "shared"}) {
			c.t.Errorf("searchtest: signals: k1's tags are %v", s.Tags)
		}
		return nil
	})
}

func sortedCopy(s []string) []string { s = slices.Clone(s); slices.Sort(s); return s }

func (c *checker[D, V]) list() {
	c.t.Helper()
	c.view("list", func(v V) error {
		got, err := any(v).(search.Lister[D]).List(context.Background(), search.Filter{Tags: []string{"keep"}}, 10)
		if err != nil {
			return err
		}
		if want := []string{"keep/k1.md", "keep/k2.md", "keep/k3.md", "keeper/k4.md"}; !slices.Equal(paths(got), want) {
			c.t.Errorf("searchtest: list: %v, want %v in path order", paths(got), want)
		}
		for _, cand := range got {
			if cand.Ord != 0 || !strings.HasPrefix(cand.Snippet, "zorblax") {
				c.t.Errorf("searchtest: list: %s answers as chunk %d with snippet %q, want its first chunk", cand.Path, cand.Ord, cand.Snippet)
			}
		}
		if got, err := any(v).(search.Lister[D]).List(context.Background(), search.Filter{Tags: []string{"keep"}}, 2); err != nil || len(got) != 2 {
			c.t.Errorf("searchtest: list: n = 2 answered %v, %v", paths(got), err)
		}
		return nil
	})
}

// snapshot changes snap/x.md while a View is open: the View answers from before the change, in
// every retriever and presentation field, and the next View from after.
func (c *checker[D, V]) snapshot() {
	c.t.Helper()
	if c.f.Change == nil {
		c.t.Fatalf("searchtest: snapshot: the fixture has no Change")
	}
	semantic := implements[V, search.Semantic[D]]()
	c.view("snapshot", func(v V) error {
		ctx := context.Background()
		before, err := v.Lexical(ctx, terms("delta"), search.Filter{}, 5)
		if err != nil {
			return err
		}
		var wg sync.WaitGroup
		wg.Go(func() { c.f.Change("snap/x.md", []string{"epsilon replaced text"}) })
		wg.Wait()
		after, err := v.Lexical(ctx, terms("delta"), search.Filter{}, 5)
		if err != nil {
			return err
		}
		if len(before) != 1 || fmt.Sprint(before) != fmt.Sprint(after) {
			c.t.Errorf("searchtest: snapshot: a change committed during the View changed its answer: %+v, then %+v", before, after)
		} else if !strings.Contains(stripMarks(after[0].Snippet), "delta") {
			c.t.Errorf("searchtest: snapshot: the hit's snippet %q is not the snapshot's text", after[0].Snippet)
		}
		if got, err := v.Lexical(ctx, terms("epsilon"), search.Filter{}, 5); err != nil || len(got) != 0 {
			c.t.Errorf("searchtest: snapshot: the View saw the change: %v, %v", paths(got), err)
		}
		if semantic {
			got, err := any(v).(search.Semantic[D]).Semantic(ctx, c.f.Model, Embed("delta snapshot text"), search.Filter{Paths: []string{"snap"}}, 5)
			if err != nil || len(got) != 1 || !strings.Contains(got[0].Snippet, "delta") {
				c.t.Errorf("searchtest: snapshot: semantic search in the View did not answer from its snapshot: %+v, %v", got, err)
			}
		}
		return nil
	})
	if got := c.lexicalOf("snapshot", []string{"epsilon"}, search.Filter{}, 5); len(got) != 1 {
		c.t.Errorf("searchtest: snapshot: the next View does not see the change: %v", paths(got))
	}
	if got := c.lexicalOf("snapshot", []string{"delta"}, search.Filter{}, 5); len(got) != 0 {
		c.t.Errorf("searchtest: snapshot: the replaced chunk still answers: %v", paths(got))
	}
	if semantic {
		got, err := c.semanticOf("snapshot", c.f.Model, "delta snapshot text", search.Filter{Paths: []string{"snap"}}, 5)
		if err != nil || len(got) != 1 || strings.Contains(got[0].Snippet, "delta") {
			c.t.Errorf("searchtest: snapshot: semantic search after the change: %+v, %v (a chunk that is gone answered)", got, err)
		}
	}
}

// implements is the Engine's rule: what a store can do follows its view type's static method set.
func implements[V, I any]() bool { return reflect.TypeFor[V]().Implements(reflect.TypeFor[I]()) }

func stripMarks(s string) string {
	return strings.NewReplacer(search.HighlightStart, "", search.HighlightEnd, "").Replace(s)
}
