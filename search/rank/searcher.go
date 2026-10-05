package rank

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/search"
)

// TextSource turns hits into the texts the ranker reads, in any format the caller chooses: exactly
// len(hits) texts in hit order, or an error. A source that reads chunks answers ErrStale for a hit
// whose chunk is no longer there.
type TextSource func(ctx context.Context, hits []search.Hit) ([]string, error)

var (
	// ErrStale is a hit a TextSource can no longer resolve: its document changed between the
	// search and the lookup.
	ErrStale = errors.New("rank: a hit is no longer in the index")
	// ErrUnavailable is a Required Searcher that could not rank: no ranker in use, or the ranker
	// or the TextSource failed. It wraps the cause.
	ErrUnavailable = errors.New("rank: the ranker is unavailable")
)

// errorMessage is Result.Rank.Error for a search the ranker could not rank: constant, never a
// provider's own text.
const errorMessage = "the ranker could not rank this search: the results are in recall order"

// The default and the most hits a search answers, when its query does not say (Limit 0) and when it
// asks for more.
const (
	DefaultLimit = 20
	MaxLimit     = 200
)

// Option configures a Searcher.
type Option func(*Searcher)

// WithDefaultLimit is the hits a query with Limit 0 answers (DefaultLimit otherwise).
func WithDefaultLimit(n int) Option {
	return func(s *Searcher) {
		if n > 0 {
			s.defaultLimit = n
		}
	}
}

// WithMaxLimit caps the hits any query answers (MaxLimit otherwise).
func WithMaxLimit(n int) Option {
	return func(s *Searcher) {
		if n > 0 {
			s.maxLimit = n
		}
	}
}

// Required makes the Searcher rank or fail: where it would fall back to recall order (no ranker in
// use, a ranker or TextSource failure, an answer of the wrong length) it returns ErrUnavailable and
// no hits. A facet listing and a query with no words still pass through, since nothing is ranked
// there.
func Required() Option { return func(s *Searcher) { s.required = true } }

// Searcher is search's second stage, a search.Searcher: it asks its inner Searcher for the top
// candidates, has the ranker in use score their texts against the query, and answers in the
// ranker's order. Its zero value is not usable: NewSearcher makes one.
type Searcher struct {
	inner        search.Searcher
	texts        TextSource
	source       Source
	defaultLimit int
	maxLimit     int
	required     bool
}

// NewSearcher re-ranks inner's hits with the ranker source holds, reading their texts from texts.
func NewSearcher(inner search.Searcher, texts TextSource, source Source, opts ...Option) *Searcher {
	s := &Searcher{inner: inner, texts: texts, source: source, defaultLimit: DefaultLimit, maxLimit: MaxLimit}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Search answers q. It reads the ranker once, at its start, and finishes with it. The inner
// Searcher is asked for the larger of the window and the query's limit; the window's top hits are
// ranked, any beyond it follow in recall order, and the answer is cut to the query's limit,
// ranked or not.
func (s *Searcher) Search(ctx context.Context, q search.Query) (search.Result, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = s.defaultLimit
	}
	limit = min(limit, s.maxLimit)
	var r Ranker
	var window int
	if s.source != nil {
		r, window = s.source.Current()
	}
	ask := q
	ask.Limit = limit
	if r != nil {
		ask.Limit = max(window, limit)
	}
	res, err := s.inner.Search(ctx, ask)
	if err != nil {
		return res, err
	}
	if res.ModeUsed == search.ModeFacet || strings.TrimSpace(q.Text) == "" {
		return cut(res, limit, search.Ranking{State: search.RankOff}), nil
	}
	if r == nil {
		if s.required {
			return search.Result{}, fmt.Errorf("%w: no ranker in use", ErrUnavailable)
		}
		return cut(res, limit, search.Ranking{State: search.RankOff}), nil
	}
	model := r.Model().Name
	ranked, err := s.rank(ctx, r, q.Text, res.Hits[:min(window, len(res.Hits))])
	switch {
	case err != nil && ctx.Err() != nil:
		return search.Result{}, ctx.Err()
	case err != nil && s.required:
		return search.Result{}, fmt.Errorf("%w: %w", ErrUnavailable, err)
	case err != nil:
		return cut(res, limit, search.Ranking{State: search.RankError, Model: model, Error: errorMessage}), nil
	}
	res.Hits = append(ranked, res.Hits[len(ranked):]...)
	return cut(res, limit, search.Ranking{State: search.RankReady, Model: model}), nil
}

// rank scores hits' texts and returns copies of the hits in the ranker's order, each with its
// score; hits with equal scores keep their recall order. A text shared by several hits is sent
// once, and each of them gets its score.
func (s *Searcher) rank(ctx context.Context, r Ranker, query string, hits []search.Hit) ([]search.Hit, error) {
	if len(hits) == 0 {
		return nil, nil
	}
	texts, err := s.texts(ctx, hits)
	if err != nil {
		return nil, err
	}
	if len(texts) != len(hits) {
		return nil, fmt.Errorf("%w: the text source answered %d texts for %d hits", ErrBadAnswer, len(texts), len(hits))
	}
	at := map[string]int{}
	var distinct []string
	for _, t := range texts {
		if _, ok := at[t]; !ok {
			at[t] = len(distinct)
			distinct = append(distinct, t)
		}
	}
	scores, err := r.Rank(ctx, query, distinct)
	if err != nil {
		return nil, err
	}
	if len(scores) != len(distinct) {
		return nil, fmt.Errorf("%w: %d scores for %d texts", ErrBadAnswer, len(scores), len(distinct))
	}
	out := make([]search.Hit, len(hits))
	for i, h := range hits {
		score := scores[at[texts[i]]]
		h.RankScore = &score
		h.Via = append(append([]string(nil), h.Via...), "rank")
		out[i] = h
	}
	sort.SliceStable(out, func(i, j int) bool { return *out[i].RankScore > *out[j].RankScore })
	return out, nil
}

// cut is res with at most limit hits and the stage's state.
func cut(res search.Result, limit int, state search.Ranking) search.Result {
	if len(res.Hits) > limit {
		res.Hits = res.Hits[:limit]
	}
	res.Rank = state
	return res
}
