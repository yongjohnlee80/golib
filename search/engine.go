package search

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"

	"github.com/yongjohnlee80/golib/search/query"
)

// QueryEmbedder embeds a query with the model active now: the model's fingerprint and the query's
// normalized vector. It answers ErrSwitching while a new model fills.
type QueryEmbedder func(ctx context.Context, q string) (model string, vec []float32, err error)

// Engine searches a Store: lexical retrieval, semantic retrieval when the store and an embedder
// allow it, reciprocal rank fusion, boosts, and a cap per document. It is safe for concurrent use.
type Engine[D comparable, V View[D]] struct {
	st        Store[D, V]
	embed     QueryEmbedder
	semantic  bool // V is a Semantic
	signaler  bool // V is a Signaler
	lister    bool // V is a Lister
	k         int
	top       int
	perDoc    int
	limit     int
	maxLimit  int
	linkBoost float64
	tagBoost  float64
}

var _ Searcher = (*Engine[int64, View[int64]])(nil)

// Option configures an Engine. A value that cannot be meant (a non-positive count) leaves the
// default.
type Option func(*options)

type options struct {
	embed               QueryEmbedder
	k, top, perDoc      int
	limit, maxLimit     int
	linkBoost, tagBoost float64
}

// WithQueryEmbedder lets the engine search by meaning, with a store whose views are Semantic.
func WithQueryEmbedder(fn QueryEmbedder) Option { return func(o *options) { o.embed = fn } }

// WithFusionK sets reciprocal rank fusion's k: a chunk ranked r by a retriever scores 1/(k+r)
// there (r from 1). The default is 60.
func WithFusionK(k int) Option { return func(o *options) { o.k = k } }

// WithRetrieverTop sets how many candidates each retriever contributes to fusion. The default is 50.
func WithRetrieverTop(n int) Option { return func(o *options) { o.top = n } }

// WithPerDocument sets how many hits one document may have in a Result. The default is 3.
func WithPerDocument(n int) Option { return func(o *options) { o.perDoc = n } }

// WithLimits sets the limit of a query that names none, and the most hits any query gets. The
// defaults are 20 and 200.
func WithLimits(def, max int) Option {
	return func(o *options) { o.limit, o.maxLimit = def, max }
}

// WithBoosts sets the boosts: a hit's score is multiplied by 1 + link·ln(1 + in-links), and by tag
// when one of its document's tags is a query word. The defaults are 0.1 and 1.2. A negative link
// or a tag below 1 leaves the default.
func WithBoosts(link, tag float64) Option {
	return func(o *options) { o.linkBoost, o.tagBoost = link, tag }
}

// NewEngine is an engine over st. What it asks of the store is decided here, once, from V's static
// method set: semantic search when V is a Semantic (and an embedder is set), boosts when V is a
// Signaler, filters alone when V is a Lister. Build it over the store's concrete view type, or an
// interface that includes the capabilities meant: over View[D] alone, a store is lexical-only,
// whatever its values could do.
func NewEngine[D comparable, V View[D]](st Store[D, V], opts ...Option) *Engine[D, V] {
	o := options{k: 60, top: 50, perDoc: 3, limit: 20, maxLimit: 200, linkBoost: 0.1, tagBoost: 1.2}
	def := o
	for _, opt := range opts {
		opt(&o)
	}
	for _, f := range []struct{ v, def *int }{{&o.k, &def.k}, {&o.top, &def.top}, {&o.perDoc, &def.perDoc}, {&o.limit, &def.limit}, {&o.maxLimit, &def.maxLimit}} {
		if *f.v <= 0 {
			*f.v = *f.def
		}
	}
	if o.linkBoost < 0 {
		o.linkBoost = def.linkBoost
	}
	if o.tagBoost < 1 {
		o.tagBoost = def.tagBoost
	}
	v := reflect.TypeFor[V]()
	return &Engine[D, V]{
		st: st, embed: o.embed,
		semantic: v.Implements(reflect.TypeFor[Semantic[D]]()),
		signaler: v.Implements(reflect.TypeFor[Signaler[D]]()),
		lister:   v.Implements(reflect.TypeFor[Lister[D]]()),
		k:        o.k, top: o.top, perDoc: o.perDoc, limit: o.limit, maxLimit: o.maxLimit,
		linkBoost: o.linkBoost, tagBoost: o.tagBoost,
	}
}

// attempts is how many times a query is embedded when the active model changes under it.
const attempts = 3

// errRetry is the active model changing between a query's embedding and its snapshot, with tries
// left.
var errRetry = errors.New("search: retry")

// Search answers q. With semantic search, the query is embedded outside the snapshot, with the
// model active then; a View whose active model has changed since answers ErrModelChanged, and the
// query is embedded again, up to three times.
func (e *Engine[D, V]) Search(ctx context.Context, q Query) (Result, error) {
	mode := q.Mode
	switch mode {
	case "":
		mode = ModeAuto
	case ModeAuto, ModeLexical, ModeSemantic:
	default:
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownMode, q.Mode)
	}
	canSemantic := e.semantic && e.embed != nil
	limit := q.Limit
	if limit <= 0 {
		limit = e.limit
	}
	limit = min(limit, e.maxLimit)
	res := Result{Hits: []Hit{}, ModeUsed: ModeLexical, Semantic: StateOff}
	if mode == ModeSemantic {
		res.ModeUsed = ModeSemantic
	}
	text, facets, err := query.Facets(q.Text, q.Filter.Facets, q.Fields)
	if err != nil {
		return Result{}, err
	}
	f := q.Filter
	f.Facets = facets
	terms, words := query.Terms(text)
	// a query with no words embeds nothing, so it needs no embedder: filters list their documents
	// (ModeFacet) and no filters answer nothing, in every mode
	if len(terms) == 0 {
		if filtered(f) {
			return e.list(ctx, res, f, limit)
		}
		return res, nil
	}
	if mode == ModeSemantic && !canSemantic {
		return Result{}, ErrNoProvider
	}
	useSemantic := canSemantic && mode != ModeLexical
	for attempt := 0; ; attempt++ {
		var model string
		var vec []float32
		var embedErr error
		if useSemantic {
			model, vec, embedErr = e.embed(ctx, text)
		}
		out, err := e.searchIn(ctx, res, mode, useSemantic, terms, words, f, model, vec, embedErr, attempt == attempts-1, limit)
		if errors.Is(err, errRetry) {
			continue
		}
		return out, err
	}
}

// searchIn is one attempt of Search, in one View.
func (e *Engine[D, V]) searchIn(ctx context.Context, res Result, mode Mode, useSemantic bool, terms []Term, words []string, f Filter,
	model string, vec []float32, embedErr error, last bool, limit int) (Result, error) {
	err := e.st.View(ctx, func(v V) error {
		var sem Semantic[D]
		if e.semantic && e.embed != nil {
			sem = any(v).(Semantic[D])
		}
		switch {
		case useSemantic && errors.Is(embedErr, ErrSwitching):
			if mode == ModeSemantic {
				return ErrSwitching
			}
			useSemantic = false
			res.Semantic = StateSwitching
		case useSemantic && embedErr != nil:
			if mode == ModeSemantic {
				return fmt.Errorf("%w: %v", ErrEmbedFailed, embedErr)
			}
			useSemantic = false
			res.Semantic, res.SemanticError = StateError, ErrEmbedFailed.Error()
		case sem != nil:
			st, err := sem.SemanticState(ctx)
			if err != nil {
				return err
			}
			res.Semantic = st
			if st == StateSwitching && useSemantic {
				// the index is between models: by words, as when the embedder says so
				if mode == ModeSemantic {
					return ErrSwitching
				}
				useSemantic = false
			}
		}
		var lexical, semantic []Candidate[D]
		var err error
		if mode != ModeSemantic {
			if lexical, err = v.Lexical(ctx, terms, f, e.top); err != nil {
				return err
			}
		}
		if useSemantic {
			semantic, err = sem.Semantic(ctx, model, vec, f, e.top)
			switch {
			case errors.Is(err, ErrModelChanged) && !last:
				return errRetry
			case errors.Is(err, ErrModelChanged):
				// the model keeps changing: the query counts as not embedded
				if mode == ModeSemantic {
					return fmt.Errorf("%w: the active model keeps changing", ErrEmbedFailed)
				}
				useSemantic, semantic = false, nil
				res.Semantic, res.SemanticError = StateError, ErrEmbedFailed.Error()
			case err != nil:
				return err
			case mode == ModeAuto:
				res.ModeUsed = ModeHybrid
			}
		}
		fused := e.fuse(lexical, semantic)
		if err := e.boost(ctx, v, fused, words); err != nil {
			return err
		}
		ran := 0
		if mode != ModeSemantic {
			ran++
		}
		if useSemantic {
			ran++
		}
		for i := range fused {
			fused[i].hit.Relevance = min(1, fused[i].hit.Score*float64(e.k+1)/float64(max(ran, 1)))
		}
		slices.SortFunc(fused, func(a, b fusedHit[D]) int { return a.compare(b) })
		perDoc := map[D]int{}
		for _, c := range fused {
			if perDoc[c.doc] == e.perDoc {
				continue
			}
			perDoc[c.doc]++
			res.Hits = append(res.Hits, c.hit)
			if len(res.Hits) == limit {
				break
			}
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

// filtered reports whether f narrows the documents: a tag, a facet, or a path filter ("" or "."
// among the paths means none).
func filtered(f Filter) bool {
	if len(f.Tags) > 0 || len(f.Facets) > 0 {
		return true
	}
	return len(f.Paths) > 0 && !slices.ContainsFunc(f.Paths, func(p string) bool { return p == "" || p == "." })
}

// list answers a query of filters alone.
func (e *Engine[D, V]) list(ctx context.Context, res Result, f Filter, limit int) (Result, error) {
	if !e.lister {
		return Result{}, ErrNoLister
	}
	err := e.st.View(ctx, func(v V) error {
		cands, err := any(v).(Lister[D]).List(ctx, f, limit)
		if err != nil {
			return err
		}
		for _, c := range cands {
			h := hitOf(c, "facet")
			h.Score, h.Relevance = 1, 1
			res.Hits = append(res.Hits, h)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	res.ModeUsed = ModeFacet
	return res, nil
}

// fusedHit is one chunk on its way to a hit.
type fusedHit[D comparable] struct {
	hit Hit
	doc D
	ord int
}

// compare orders hits by score, then path, then position, so equal scores come out the same way
// every time.
func (a fusedHit[D]) compare(b fusedHit[D]) int {
	if a.hit.Score != b.hit.Score {
		return cmp.Compare(b.hit.Score, a.hit.Score)
	}
	if a.hit.Path != b.hit.Path {
		return cmp.Compare(a.hit.Path, b.hit.Path)
	}
	return cmp.Compare(a.ord, b.ord)
}

func hitOf[D comparable](c Candidate[D], via string) Hit {
	return Hit{Path: c.Path, Breadcrumb: c.Breadcrumb, Snippet: c.Snippet, Generation: c.Generation,
		ByteStart: c.ByteStart, ByteEnd: c.ByteEnd, Via: []string{via}}
}

// fuse scores the candidates by reciprocal rank fusion: the sum, over the retrievers that found a
// chunk, of 1/(k + its rank there). With one retriever the order is its own.
func (e *Engine[D, V]) fuse(lexical, semantic []Candidate[D]) []fusedHit[D] {
	type key struct {
		doc D
		ord int
	}
	at := map[key]int{}
	var out []fusedHit[D]
	for _, l := range []struct {
		via   string
		cands []Candidate[D]
	}{{"lexical", lexical}, {"semantic", semantic}} {
		for rank, c := range l.cands {
			k := key{c.Doc, c.Ord}
			i, ok := at[k]
			if !ok {
				i = len(out)
				at[k] = i
				out = append(out, fusedHit[D]{hit: hitOf(c, l.via), doc: c.Doc, ord: c.Ord})
			} else {
				out[i].hit.Via = append(out[i].hit.Via, l.via)
			}
			out[i].hit.Score += 1 / float64(e.k+rank+1)
		}
	}
	return out
}

// boost scales the fused scores by the documents' signals: × (1 + link·ln(1 + in-links)), and × tag
// when a query word is one of the document's tags.
func (e *Engine[D, V]) boost(ctx context.Context, v V, hits []fusedHit[D], words []string) error {
	if !e.signaler || len(hits) == 0 {
		return nil
	}
	var docs []D
	seen := map[D]bool{}
	for _, h := range hits {
		if !seen[h.doc] {
			seen[h.doc] = true
			docs = append(docs, h.doc)
		}
	}
	signals, err := any(v).(Signaler[D]).Signals(ctx, docs)
	if err != nil {
		return err
	}
	inQuery := map[string]bool{}
	for _, w := range words {
		inQuery[w] = true
	}
	factor := map[D]float64{}
	for _, d := range docs {
		s := signals[d]
		f := 1 + e.linkBoost*math.Log(1+float64(s.InLinks))
		for _, t := range s.Tags {
			if inQuery[t] {
				f *= e.tagBoost
				break
			}
		}
		factor[d] = f
	}
	for i := range hits {
		hits[i].hit.Score *= factor[hits[i].doc]
	}
	return nil
}
