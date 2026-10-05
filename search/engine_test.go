package search

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// key is a document key with no order: the engine may only compare it for equality.
type key struct{ tenant, id string }

// fake is a store whose views answer what the test sets. inView counts the views open, so a read
// outside one is caught.
type fake struct {
	lexical  []Candidate[key]
	semantic []Candidate[key]
	state    State
	changed  int // Semantic answers ErrModelChanged this many times first
	signals  map[key]Signals
	listed   []Candidate[key]

	mu      sync.Mutex
	views   int
	inView  atomic.Int32
	outside atomic.Int32 // reads outside a view
	filters []Filter     // what each retriever was asked for
	terms   [][]Term
}

func (s *fake) open(ctx context.Context, fn func() error) error {
	s.mu.Lock()
	s.views++
	s.mu.Unlock()
	s.inView.Add(1)
	defer s.inView.Add(-1)
	return fn()
}

func (s *fake) read(f Filter, terms []Term) {
	if s.inView.Load() == 0 {
		s.outside.Add(1)
	}
	s.mu.Lock()
	s.filters = append(s.filters, f)
	s.terms = append(s.terms, terms)
	s.mu.Unlock()
}

// lexView answers words only.
type lexView struct{ s *fake }

func (v lexView) Lexical(ctx context.Context, terms []Term, f Filter, n int) ([]Candidate[key], error) {
	v.s.read(f, terms)
	return first(v.s.lexical, n), nil
}

// semView adds semantic search.
type semView struct{ lexView }

func (v semView) Semantic(ctx context.Context, model string, vec []float32, f Filter, n int) ([]Candidate[key], error) {
	v.s.read(f, nil)
	v.s.mu.Lock()
	defer v.s.mu.Unlock()
	if v.s.changed > 0 {
		v.s.changed--
		return nil, ErrModelChanged
	}
	return first(v.s.semantic, n), nil
}

func (v semView) SemanticState(ctx context.Context) (State, error) {
	v.s.read(Filter{}, nil)
	return v.s.state, nil
}

// fullView adds boosts and listing.
type fullView struct{ semView }

func (v fullView) Signals(ctx context.Context, docs []key) (map[key]Signals, error) {
	v.s.read(Filter{}, nil)
	return v.s.signals, nil
}

func (v fullView) List(ctx context.Context, f Filter, n int) ([]Candidate[key], error) {
	v.s.read(f, nil)
	return first(v.s.listed, n), nil
}

func first(c []Candidate[key], n int) []Candidate[key] { return c[:min(n, len(c))] }

type lexStore struct{ *fake }

func (s lexStore) View(ctx context.Context, fn func(lexView) error) error {
	return s.open(ctx, func() error { return fn(lexView{s.fake}) })
}

type semStore struct{ *fake }

func (s semStore) View(ctx context.Context, fn func(semView) error) error {
	return s.open(ctx, func() error { return fn(semView{lexView{s.fake}}) })
}

type fullStore struct{ *fake }

func (s fullStore) View(ctx context.Context, fn func(fullView) error) error {
	return s.open(ctx, func() error { return fn(fullView{semView{lexView{s.fake}}}) })
}

// embedder counts its calls; err, when set, is what it answers.
type embedder struct {
	calls atomic.Int32
	err   error
}

func (e *embedder) embed(ctx context.Context, q string) (string, []float32, error) {
	e.calls.Add(1)
	if e.err != nil {
		return "", nil, e.err
	}
	return "m", []float32{1, 0}, nil
}

func cand(doc string, ord int) Candidate[key] {
	return Candidate[key]{Doc: key{"t", doc}, Ord: ord, Path: doc + ".md", Breadcrumb: doc, Snippet: doc}
}

func hitsOf(r Result) []string {
	var out []string
	for _, h := range r.Hits {
		out = append(out, fmt.Sprintf("%s#%d %s", h.Path, h.ByteStart, strings.Join(h.Via, "+")))
	}
	return out
}

func corpus() *fake {
	return &fake{
		lexical:  []Candidate[key]{cand("a", 0), cand("b", 0)},
		semantic: []Candidate[key]{cand("c", 0), cand("a", 0)},
		state:    StateReady,
	}
}

// TestModeMatrix: every mode against every semantic state, with what ran and what was reported.
func TestModeMatrix(t *testing.T) {
	type want struct {
		used  Mode
		state State
		err   error
		lex   bool // the lexical retriever ran
		sem   bool // the semantic retriever ran
		calls int  // the embedder's calls
	}
	cases := []struct {
		name   string
		build  func(s *fake, e *embedder) Searcher
		mode   Mode
		answer want
	}{}
	noEmbedder := func(s *fake, e *embedder) Searcher { return NewEngine[key, fullView](fullStore{s}) }
	lexicalOnly := func(s *fake, e *embedder) Searcher {
		return NewEngine[key, lexView](lexStore{s}, WithQueryEmbedder(e.embed))
	}
	withState := func(st State, embedErr error) func(s *fake, e *embedder) Searcher {
		return func(s *fake, e *embedder) Searcher {
			s.state, e.err = st, embedErr
			return NewEngine[key, fullView](fullStore{s}, WithQueryEmbedder(e.embed))
		}
	}
	add := func(name string, b func(*fake, *embedder) Searcher, auto, sem, lex want) {
		cases = append(cases,
			struct {
				name   string
				build  func(s *fake, e *embedder) Searcher
				mode   Mode
				answer want
			}{name + "/auto", b, ModeAuto, auto},
			struct {
				name   string
				build  func(s *fake, e *embedder) Searcher
				mode   Mode
				answer want
			}{name + "/semantic", b, ModeSemantic, sem},
			struct {
				name   string
				build  func(s *fake, e *embedder) Searcher
				mode   Mode
				answer want
			}{name + "/lexical", b, ModeLexical, lex})
	}
	add("off: no embedder", noEmbedder,
		want{used: ModeLexical, state: StateOff, lex: true},
		want{err: ErrNoProvider},
		want{used: ModeLexical, state: StateOff, lex: true})
	add("off: a lexical-only store", lexicalOnly,
		want{used: ModeLexical, state: StateOff, lex: true},
		want{err: ErrNoProvider},
		want{used: ModeLexical, state: StateOff, lex: true})
	add("partial", withState(StatePartial, nil),
		want{used: ModeHybrid, state: StatePartial, lex: true, sem: true, calls: 1},
		want{used: ModeSemantic, state: StatePartial, sem: true, calls: 1},
		want{used: ModeLexical, state: StatePartial, lex: true})
	add("switching: from the embedder", withState(StateReady, fmt.Errorf("offline: %w", ErrSwitching)),
		want{used: ModeLexical, state: StateSwitching, lex: true, calls: 1},
		want{err: ErrSwitching, calls: 1},
		want{used: ModeLexical, state: StateReady, lex: true})
	// the query embedded, but the snapshot says the index is between models: by words all the same
	add("switching: from the view", withState(StateSwitching, nil),
		want{used: ModeLexical, state: StateSwitching, lex: true, calls: 1},
		want{err: ErrSwitching, calls: 1},
		want{used: ModeLexical, state: StateSwitching, lex: true})
	add("error", withState(StateReady, errors.New("provider down")),
		want{used: ModeLexical, state: StateError, lex: true, calls: 1},
		want{err: ErrEmbedFailed, calls: 1},
		want{used: ModeLexical, state: StateReady, lex: true})
	add("ready", withState(StateReady, nil),
		want{used: ModeHybrid, state: StateReady, lex: true, sem: true, calls: 1},
		want{used: ModeSemantic, state: StateReady, sem: true, calls: 1},
		want{used: ModeLexical, state: StateReady, lex: true})

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, e := corpus(), &embedder{}
			res, err := c.build(s, e).Search(context.Background(), Query{Text: "storage", Mode: c.mode})
			w := c.answer
			if int(e.calls.Load()) != w.calls {
				t.Errorf("the embedder was called %d times, want %d", e.calls.Load(), w.calls)
			}
			if w.err != nil {
				if !errors.Is(err, w.err) {
					t.Fatalf("err = %v, want %v", err, w.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.ModeUsed != w.used || res.Semantic != w.state {
				t.Errorf("ModeUsed, Semantic = %s, %s; want %s, %s", res.ModeUsed, res.Semantic, w.used, w.state)
			}
			if (res.Semantic == StateError) != (res.SemanticError == ErrEmbedFailed.Error()) {
				t.Errorf("SemanticError = %q with state %s", res.SemanticError, res.Semantic)
			}
			var lex, sem bool
			for _, h := range res.Hits {
				for _, v := range h.Via {
					lex = lex || v == "lexical"
					sem = sem || v == "semantic"
				}
			}
			if lex != w.lex || sem != w.sem {
				t.Errorf("retrievers ran: lexical %v, semantic %v; want %v, %v (%v)", lex, sem, w.lex, w.sem, hitsOf(res))
			}
		})
	}
}

// TestALexicalOnlyStoreNeverCallsTheEmbedder: an embedder that blocks or fails changes nothing for
// a store without vectors.
func TestALexicalOnlyStoreNeverCallsTheEmbedder(t *testing.T) {
	block := func(ctx context.Context, q string) (string, []float32, error) {
		<-ctx.Done()
		return "", nil, ctx.Err()
	}
	s := corpus()
	got, err := NewEngine[key, lexView](lexStore{s}, WithQueryEmbedder(block)).Search(context.Background(), Query{Text: "storage"})
	want, _ := NewEngine[key, lexView](lexStore{corpus()}).Search(context.Background(), Query{Text: "storage"})
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("with a blocking embedder: %+v, %v; without one: %+v", got, err, want)
	}
}

// TestCapabilityFollowsTheViewType: an engine over View[D] treats every store as lexical-only, even
// one whose values could search by meaning; an interface that names the capability keeps it.
func TestCapabilityFollowsTheViewType(t *testing.T) {
	ctx := context.Background()
	e := &embedder{}
	broad := NewEngine[key, View[key]](viewStore{corpus()}, WithQueryEmbedder(e.embed))
	if _, err := broad.Search(ctx, Query{Text: "storage", Mode: ModeSemantic}); !errors.Is(err, ErrNoProvider) || e.calls.Load() != 0 {
		t.Errorf("over View[D]: %v, %d calls; want ErrNoProvider and none", err, e.calls.Load())
	}
	narrow := NewEngine[key, semanticView](semIfaceStore{corpus()}, WithQueryEmbedder(e.embed))
	res, err := narrow.Search(ctx, Query{Text: "storage", Mode: ModeSemantic})
	if err != nil || res.ModeUsed != ModeSemantic || e.calls.Load() != 1 {
		t.Errorf("over an interface with Semantic: %+v, %v, %d calls", res, err, e.calls.Load())
	}
}

// semanticView is an interface view type that includes the capability.
type semanticView interface {
	View[key]
	Semantic[key]
}

type viewStore struct{ *fake }

func (s viewStore) View(ctx context.Context, fn func(View[key]) error) error {
	return s.open(ctx, func() error { return fn(semView{lexView{s.fake}}) })
}

type semIfaceStore struct{ *fake }

func (s semIfaceStore) View(ctx context.Context, fn func(semanticView) error) error {
	return s.open(ctx, func() error { return fn(semView{lexView{s.fake}}) })
}

// TestModelChangedRetries: a view whose model moved is retried, embedding again, up to three times;
// then the query counts as not embedded.
func TestModelChangedRetries(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		changed int
		mode    Mode
		state   State
		err     error
		calls   int32
	}{
		{1, ModeAuto, StateReady, nil, 2},
		{2, ModeAuto, StateReady, nil, 3},
		{3, ModeAuto, StateError, nil, 3},
		{2, ModeSemantic, StateReady, nil, 3},
		{3, ModeSemantic, "", ErrEmbedFailed, 3},
	} {
		s, e := corpus(), &embedder{}
		s.changed = c.changed
		res, err := NewEngine[key, fullView](fullStore{s}, WithQueryEmbedder(e.embed)).Search(ctx, Query{Text: "storage", Mode: c.mode})
		if e.calls.Load() != c.calls {
			t.Errorf("%d changes, %s: %d embeddings, want %d", c.changed, c.mode, e.calls.Load(), c.calls)
		}
		if c.err != nil {
			if !errors.Is(err, c.err) {
				t.Errorf("%d changes, %s: %v, want %v", c.changed, c.mode, err, c.err)
			}
			continue
		}
		if err != nil || res.Semantic != c.state {
			t.Errorf("%d changes, %s: %s, %v; want %s", c.changed, c.mode, res.Semantic, err, c.state)
		}
		if c.state == StateError && (res.ModeUsed != ModeLexical || len(res.Hits) != 2) {
			t.Errorf("after the last change the answer is lexical: %+v", res)
		}
	}
}

// TestFusion: a chunk both retrievers found is one hit with both, scored by reciprocal rank; the cap
// counts hits per document.
func TestFusion(t *testing.T) {
	s, e := corpus(), &embedder{}
	s.lexical = []Candidate[key]{cand("a", 0), cand("a", 1), cand("a", 2), cand("a", 3), cand("b", 0)}
	s.semantic = []Candidate[key]{cand("b", 0), cand("a", 0)}
	res, err := NewEngine[key, fullView](fullStore{s}, WithQueryEmbedder(e.embed)).Search(context.Background(), Query{Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	// a#0: 1/61 + 1/62; b#0: 1/65 + 1/61; a#1: 1/62; a#2: 1/63; a#3 is capped
	if got, want := hitsOf(res), []string{"a.md#0 lexical+semantic", "b.md#0 lexical+semantic", "a.md#0 lexical", "a.md#0 lexical"}; !reflect.DeepEqual(got, want) {
		t.Errorf("hits = %q, want %q", got, want)
	}
	// computed at run time, as the engine adds: a constant expression rounds once, not per step
	one, sixty := 1.0, 60.0
	if got, want := res.Hits[0].Score, one/(sixty+1)+one/(sixty+2); got != want {
		t.Errorf("score = %v, want %v", got, want)
	}
	if got, want := res.Hits[0].Relevance, min(1, (one/(sixty+1)+one/(sixty+2))*(sixty+1)/2); got != want {
		t.Errorf("relevance = %v, want %v", got, want)
	}
}

// TestTiesAreDeterministic: equal scores order by path, then position, whatever order the
// retrievers gave, with a document key that has no order.
func TestTiesAreDeterministic(t *testing.T) {
	ctx := context.Background()
	mk := func(order ...string) []Candidate[key] {
		var out []Candidate[key]
		for _, o := range order {
			out = append(out, cand(o, 0))
		}
		return out
	}
	var answers [][]string
	for _, order := range [][]string{{"c", "a", "b"}, {"b", "c", "a"}} {
		// each in a store of its own, ranked first by the one retriever: the scores differ, so give
		// every chunk the same rank by putting each alone in its own query
		var got []string
		for _, o := range order {
			s := &fake{lexical: mk(o), state: StateReady}
			res, _ := NewEngine[key, lexView](lexStore{s}).Search(ctx, Query{Text: "x"})
			got = append(got, hitsOf(res)...)
		}
		answers = append(answers, got)
	}
	// equal scores within one result: two documents, each found once at the same rank by a different
	// retriever
	s, e := &fake{lexical: mk("b"), semantic: mk("a"), state: StateReady}, &embedder{}
	res, _ := NewEngine[key, fullView](fullStore{s}, WithQueryEmbedder(e.embed)).Search(ctx, Query{Text: "x"})
	if got, want := hitsOf(res), []string{"a.md#0 semantic", "b.md#0 lexical"}; !reflect.DeepEqual(got, want) {
		t.Errorf("equal scores: %q, want %q (path order)", got, want)
	}
	s = &fake{lexical: []Candidate[key]{cand("a", 2)}, semantic: []Candidate[key]{cand("a", 1)}, state: StateReady}
	s.lexical[0].ByteStart, s.semantic[0].ByteStart = 2, 1
	res, _ = NewEngine[key, fullView](fullStore{s}, WithQueryEmbedder(e.embed)).Search(ctx, Query{Text: "x"})
	if got, want := hitsOf(res), []string{"a.md#1 semantic", "a.md#2 lexical"}; !reflect.DeepEqual(got, want) {
		t.Errorf("equal scores in one document: %q, want %q (position order)", got, want)
	}
}

// TestBoosts: no boost without a Signaler; with one, × (1 + 0.1·ln(1 + in-links)) and × 1.2 for a
// tag that is a query word.
func TestBoosts(t *testing.T) {
	ctx := context.Background()
	s := corpus()
	s.lexical = []Candidate[key]{cand("a", 0), cand("b", 0)}
	plain, _ := NewEngine[key, semView](semStore{s}).Search(ctx, Query{Text: "design"})
	if plain.Hits[0].Score != 1/float64(61) || plain.Hits[1].Score != 1/float64(62) {
		t.Errorf("without a Signaler: %v", plain.Hits)
	}
	s.signals = map[key]Signals{{"t", "a"}: {InLinks: 3}, {"t", "b"}: {Tags: []string{"design"}}}
	res, _ := NewEngine[key, fullView](fullStore{s}).Search(ctx, Query{Text: "Design"})
	one, sixty, link, tag := 1.0, 60.0, 0.1, 1.2
	wantA := one / (sixty + 1) * (1 + link*math.Log(4))
	wantB := one / (sixty + 2) * tag
	got := map[string]float64{}
	for _, h := range res.Hits {
		got[h.Path] = h.Score
	}
	if got["a.md"] != wantA || got["b.md"] != wantB {
		t.Errorf("boosted: %v, want a %v, b %v", got, wantA, wantB)
	}
	res, _ = NewEngine[key, fullView](fullStore{s}, WithBoosts(0.2, 2)).Search(ctx, Query{Text: "design"})
	for _, h := range res.Hits {
		got[h.Path] = h.Score
	}
	link, tag = 0.2, 2
	if got["a.md"] != one/(sixty+1)*(1+link*math.Log(4)) || got["b.md"] != one/(sixty+2)*tag {
		t.Errorf("with WithBoosts(0.2, 2): %v", got)
	}
}

// fieldsOf declares the named fields, any text a value.
type fieldsOf []string

func (f fieldsOf) Declared(name string) bool {
	for _, n := range f {
		if n == name {
			return true
		}
	}
	return false
}
func (f fieldsOf) FacetValue(name, text string) (string, error) {
	if text == "bad" {
		return "", errors.New("not a value")
	}
	return text, nil
}

// TestFacetsFromTheQuery: Query.Fields decides which name:value words are filters; filters alone
// list through a Lister.
func TestFacetsFromTheQuery(t *testing.T) {
	ctx := context.Background()
	s := corpus()
	eng := NewEngine[key, fullView](fullStore{s})
	if _, err := eng.Search(ctx, Query{Text: "type:adr storage", Fields: fieldsOf{"type"}}); err != nil {
		t.Fatal(err)
	}
	if f, terms := s.filters[0], s.terms[0]; !reflect.DeepEqual(f.Facets, map[string][]string{"type": {"adr"}}) || len(terms) != 1 || terms[0].Text != "storage" {
		t.Errorf("a declared field: %+v, %+v", f, terms)
	}
	s.filters, s.terms = nil, nil
	if _, err := eng.Search(ctx, Query{Text: "type:adr storage"}); err != nil {
		t.Fatal(err)
	}
	if f, terms := s.filters[0], s.terms[0]; f.Facets != nil || len(terms) != 2 || terms[0].Text != "type:adr" {
		t.Errorf("the field no longer declared: %+v, %+v", f, terms)
	}
	if _, err := eng.Search(ctx, Query{Text: "storage", Filter: Filter{Facets: map[string][]string{"nope": {"x"}}}, Fields: fieldsOf{"type"}}); err == nil {
		t.Error("an undeclared explicit facet was accepted")
	}
	if _, err := eng.Search(ctx, Query{Text: "type:bad storage", Fields: fieldsOf{"type"}}); err == nil {
		t.Error("a bad facet value was accepted")
	}

	s.listed = []Candidate[key]{cand("a", 0), cand("b", 0)}
	res, err := eng.Search(ctx, Query{Text: "type:adr", Fields: fieldsOf{"type"}})
	if err != nil || res.ModeUsed != ModeFacet || res.Semantic != StateOff || len(res.Hits) != 2 {
		t.Fatalf("filters alone: %+v, %v", res, err)
	}
	if h := res.Hits[0]; h.Score != 1 || h.Relevance != 1 || !reflect.DeepEqual(h.Via, []string{"facet"}) {
		t.Errorf("a listed hit: %+v", h)
	}
	if _, err := NewEngine[key, semView](semStore{s}).Search(ctx, Query{Text: "type:adr", Fields: fieldsOf{"type"}}); !errors.Is(err, ErrNoLister) {
		t.Errorf("filters alone without a Lister: %v", err)
	}
	// every kind of filter alone lists; a path list holding "" or "." is no filter
	for name, f := range map[string]Filter{
		"a tag":  {Tags: []string{"keep"}},
		"a path": {Paths: []string{"guides"}},
	} {
		s.filters = nil
		res, err := eng.Search(ctx, Query{Text: "  ", Filter: f})
		if err != nil || res.ModeUsed != ModeFacet || len(res.Hits) != 2 || !reflect.DeepEqual(s.filters[0], f) {
			t.Errorf("%s alone: %+v, %v (listed with %+v)", name, res, err, s.filters)
		}
	}
	for name, q := range map[string]Query{
		"no words and no filters":    {Text: "  !!  "},
		"a root path is no filter":   {Text: "", Filter: Filter{Paths: []string{"guides", "."}}},
		"an empty path is no filter": {Text: "", Filter: Filter{Paths: []string{""}}},
	} {
		res, err := eng.Search(ctx, q)
		if err != nil || len(res.Hits) != 0 || res.ModeUsed != ModeLexical {
			t.Errorf("%s: %+v, %v", name, res, err)
		}
	}
}

// TestAWordlessQueryNeedsNoEmbedder: a query with no words embeds nothing, so semantic mode without
// an embedder still lists by its filters, or answers nothing without them; a query with words is
// refused, as before.
func TestAWordlessQueryNeedsNoEmbedder(t *testing.T) {
	ctx := context.Background()
	s := corpus()
	s.listed = []Candidate[key]{cand("a", 0), cand("b", 0)}
	eng := NewEngine[key, fullView](fullStore{s})
	for name, q := range map[string]Query{
		"a facet in the query": {Text: "type:adr", Fields: fieldsOf{"type"}, Mode: ModeSemantic},
		"a tag alone":          {Text: "  ", Filter: Filter{Tags: []string{"keep"}}, Mode: ModeSemantic},
	} {
		res, err := eng.Search(ctx, q)
		if err != nil || res.ModeUsed != ModeFacet || res.Semantic != StateOff || len(res.Hits) != 2 {
			t.Errorf("%s, semantic mode, no embedder: %+v, %v; want the two listed", name, res, err)
		}
	}
	if res, err := eng.Search(ctx, Query{Text: "", Mode: ModeSemantic}); err != nil || len(res.Hits) != 0 || res.ModeUsed != ModeSemantic {
		t.Errorf("no words and no filters, semantic mode, no embedder: %+v, %v; want no hits", res, err)
	}
	if _, err := eng.Search(ctx, Query{Text: "type:adr storage", Fields: fieldsOf{"type"}, Mode: ModeSemantic}); !errors.Is(err, ErrNoProvider) {
		t.Errorf("a word beside the facet, semantic mode, no embedder: %v, want ErrNoProvider", err)
	}
}

// TestModesAndLimits: an unknown mode is refused; the limit defaults, and is capped.
func TestModesAndLimits(t *testing.T) {
	ctx := context.Background()
	s := corpus()
	for i := range 300 {
		s.lexical = append(s.lexical, cand(fmt.Sprintf("d%03d", i), 0))
	}
	eng := NewEngine[key, lexView](lexStore{s}, WithRetrieverTop(400), WithPerDocument(0))
	if _, err := eng.Search(ctx, Query{Text: "x", Mode: "fuzzy"}); !errors.Is(err, ErrUnknownMode) {
		t.Errorf("an unknown mode: %v", err)
	}
	for _, c := range []struct{ limit, want int }{{0, 20}, {5, 5}, {500, 200}} {
		res, err := eng.Search(ctx, Query{Text: "x", Limit: c.limit})
		if err != nil || len(res.Hits) != c.want {
			t.Errorf("limit %d: %d hits, %v; want %d", c.limit, len(res.Hits), err, c.want)
		}
	}
	res, _ := NewEngine[key, lexView](lexStore{s}, WithLimits(7, 9)).Search(ctx, Query{Text: "x", Limit: 50})
	if len(res.Hits) != 9 {
		t.Errorf("WithLimits(7, 9) capped at %d", len(res.Hits))
	}
}

// TestNoReadOutsideTheView: every retriever and capability call happens inside the one view of each
// attempt.
func TestNoReadOutsideTheView(t *testing.T) {
	s, e := corpus(), &embedder{}
	s.changed = 1
	s.signals = map[key]Signals{}
	if _, err := NewEngine[key, fullView](fullStore{s}, WithQueryEmbedder(e.embed)).Search(context.Background(), Query{Text: "storage"}); err != nil {
		t.Fatal(err)
	}
	if s.outside.Load() != 0 || s.views != 2 {
		t.Errorf("%d reads outside a view, %d views; want 0 and 2 (one per attempt)", s.outside.Load(), s.views)
	}
}

// TestConcurrentSearches: the engine is safe for concurrent use.
func TestConcurrentSearches(t *testing.T) {
	s, e := corpus(), &embedder{}
	s.signals = map[key]Signals{{"t", "a"}: {InLinks: 1}}
	eng := NewEngine[key, fullView](fullStore{s}, WithQueryEmbedder(e.embed))
	want, _ := eng.Search(context.Background(), Query{Text: "storage"})
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 50 {
				got, err := eng.Search(context.Background(), Query{Text: "storage"})
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("a concurrent search: %+v, %v", got, err)
					return
				}
			}
		})
	}
	wg.Wait()
}
