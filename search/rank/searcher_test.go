package rank

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/search"
)

// inner is a fake inner Searcher: n hits in recall order (hit i is "d<i>.md", relevance falling), or
// a facet listing; it records the limit it was asked for.
type inner struct {
	n      int
	facet  bool
	mu     sync.Mutex
	asked  []int
	before func() // runs before it answers
}

func (f *inner) Search(_ context.Context, q search.Query) (search.Result, error) {
	f.mu.Lock()
	f.asked = append(f.asked, q.Limit)
	f.mu.Unlock()
	if f.before != nil {
		f.before()
	}
	res := search.Result{ModeUsed: search.ModeHybrid, Semantic: search.StateReady}
	if f.facet {
		res.ModeUsed = search.ModeFacet
	}
	for i := range min(f.n, q.Limit) {
		res.Hits = append(res.Hits, search.Hit{Path: fmt.Sprintf("d%d.md", i), Relevance: 1 - float64(i)/1000,
			Via: []string{"lexical", "semantic"}})
	}
	return res, nil
}

// byPath is a TextSource: a hit's text is its path.
func byPath(_ context.Context, hits []search.Hit) ([]string, error) {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.Path
	}
	return out, nil
}

// fake is a ranker scoring a text by score; it records what it was sent.
type fake struct {
	name  string
	score func(text string) float64
	err   error
	short bool // answer one score too few
	mu    sync.Mutex
	sent  [][]string
}

func (f *fake) Model() Model { return Model{Provider: "fake", Name: f.name, MaxBatch: 100} }

func (f *fake) Rank(_ context.Context, _ string, texts []string) ([]float64, error) {
	f.mu.Lock()
	f.sent = append(f.sent, append([]string(nil), texts...))
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([]float64, len(texts))
	for i, t := range texts {
		out[i] = f.score(t)
	}
	if f.short {
		out = out[1:]
	}
	return out, nil
}

// reverse scores later recall hits higher: d39 first, as a cross-encoder may.
func reverse(t string) float64 {
	var n int
	_, _ = fmt.Sscanf(t, "d%d.md", &n)
	return float64(n)
}

func paths(hits []search.Hit) []string {
	var out []string
	for _, h := range hits {
		out = append(out, h.Path)
	}
	return out
}

func holding(r Ranker, window int) *Holder {
	var h Holder
	h.Set(r, window)
	return &h
}

// TestTheLimitsHoldRankedOrNot: the inner Searcher is asked for the larger of the window and the
// query's limit, and the answer is cut to the limit (0: the default), on success and on every
// fallback alike.
func TestTheLimitsHoldRankedOrNot(t *testing.T) {
	failures := map[string]func(*fake) TextSource{
		"ranked":       func(*fake) TextSource { return byPath },
		"ranker fails": func(f *fake) TextSource { f.err = ErrUnreachable; return byPath },
		"texts fail": func(*fake) TextSource {
			return func(context.Context, []search.Hit) ([]string, error) { return nil, ErrStale }
		},
		"short answer": func(f *fake) TextSource { f.short = true; return byPath },
		"texts too few": func(*fake) TextSource {
			return func(context.Context, []search.Hit) ([]string, error) { return []string{"x"}, nil }
		},
	}
	for name, setup := range failures {
		for _, c := range []struct{ limit, want, asked int }{{0, 20, 40}, {10, 10, 40}, {100, 100, 100}} {
			f := &fake{name: "m", score: reverse}
			texts := setup(f)
			in := &inner{n: 500}
			res, err := NewSearcher(in, texts, holding(f, 40)).Search(context.Background(), search.Query{Text: "q", Limit: c.limit})
			if err != nil || len(res.Hits) != c.want || in.asked[0] != c.asked {
				t.Fatalf("%s, limit %d: %d hits, asked %v, %v; want %d hits, asked %d", name, c.limit, len(res.Hits), in.asked, err, c.want, c.asked)
			}
			wantState := search.RankError
			if name == "ranked" {
				wantState = search.RankReady
			}
			if res.Rank.State != wantState || res.Rank.Model != "m" {
				t.Fatalf("%s, limit %d: rank %+v", name, c.limit, res.Rank)
			}
			if wantState == search.RankError {
				if res.Rank.Error == "" || strings.Contains(res.Rank.Error, "unreachable") || res.Hits[0].Path != "d0.md" || res.Hits[0].RankScore != nil {
					t.Fatalf("%s: a fallback is not in recall order with a constant error: %+v, %+v", name, res.Rank, res.Hits[0])
				}
			}
		}
	}
}

// TestRankedHitsAreTheRankersOrder: the window is in the ranker's order, each hit carries its score
// with Relevance untouched and "rank" appended to Via; hits past the window follow in recall order,
// unscored; ties keep recall order.
func TestRankedHitsAreTheRankersOrder(t *testing.T) {
	f := &fake{name: "m", score: reverse}
	res, err := NewSearcher(&inner{n: 500}, byPath, holding(f, 10)).Search(context.Background(), search.Query{Text: "q", Limit: 12})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"d9.md", "d8.md", "d7.md", "d6.md", "d5.md", "d4.md", "d3.md", "d2.md", "d1.md", "d0.md", "d10.md", "d11.md"}
	if got := paths(res.Hits); !slices.Equal(got, want) {
		t.Fatalf("order %v", got)
	}
	top := res.Hits[0]
	if top.RankScore == nil || *top.RankScore != 9 || top.Relevance != 1-9.0/1000 || !slices.Equal(top.Via, []string{"lexical", "semantic", "rank"}) {
		t.Fatalf("the top hit %+v", top)
	}
	if res.Hits[10].RankScore != nil || slices.Contains(res.Hits[10].Via, "rank") {
		t.Fatalf("a hit past the window was scored: %+v", res.Hits[10])
	}
	tie := &fake{name: "m", score: func(string) float64 { return 1 }}
	res, _ = NewSearcher(&inner{n: 5}, byPath, holding(tie, 10)).Search(context.Background(), search.Query{Text: "q", Limit: 5})
	if got := paths(res.Hits); !slices.Equal(got, []string{"d0.md", "d1.md", "d2.md", "d3.md", "d4.md"}) {
		t.Fatalf("ties are not in recall order: %v", got)
	}
}

// TestDistinctTextsAreScoredOnce: hits sharing a text send it once, and every one of them gets its
// score; no hit is dropped.
func TestDistinctTextsAreScoredOnce(t *testing.T) {
	f := &fake{name: "m", score: func(t string) float64 { return float64(len(t)) }}
	same := func(_ context.Context, hits []search.Hit) ([]string, error) {
		return []string{"one record", "short", "one record"}, nil
	}
	res, err := NewSearcher(&inner{n: 3}, same, holding(f, 10)).Search(context.Background(), search.Query{Text: "q", Limit: 3})
	if err != nil || len(res.Hits) != 3 {
		t.Fatalf("%v, %v", paths(res.Hits), err)
	}
	if len(f.sent) != 1 || !slices.Equal(f.sent[0], []string{"one record", "short"}) {
		t.Fatalf("sent %v", f.sent)
	}
	if got := paths(res.Hits); !slices.Equal(got, []string{"d0.md", "d2.md", "d1.md"}) || *res.Hits[1].RankScore != 10 {
		t.Fatalf("the hits sharing a text: %v", got)
	}
}

// TestTheRankerReadsTheCallersTexts: whatever the TextSource answers, in any format, is exactly
// what the ranker receives.
func TestTheRankerReadsTheCallersTexts(t *testing.T) {
	f := &fake{name: "m", score: func(string) float64 { return 0 }}
	rendered := func(_ context.Context, hits []search.Hit) ([]string, error) {
		var out []string
		for _, h := range hits {
			out = append(out, `{"title":"`+strings.TrimSuffix(h.Path, ".md")+`"}`)
		}
		return out, nil
	}
	if _, err := NewSearcher(&inner{n: 2}, rendered, holding(f, 10)).Search(context.Background(), search.Query{Text: "q"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.sent[0], []string{`{"title":"d0"}`, `{"title":"d1"}`}) {
		t.Fatalf("the ranker read %v", f.sent)
	}
}

// TestPassThrough: a facet listing, a query of no words and no ranker in use are answered in recall
// order, cut, with the stage off.
func TestPassThrough(t *testing.T) {
	f := &fake{name: "m", score: reverse}
	for name, c := range map[string]struct {
		in *inner
		q  search.Query
		h  *Holder
	}{
		"facet":     {&inner{n: 50, facet: true}, search.Query{Text: "type:adr", Limit: 5}, holding(f, 10)},
		"no words":  {&inner{n: 50}, search.Query{Text: "  ", Limit: 5}, holding(f, 10)},
		"no ranker": {&inner{n: 50}, search.Query{Text: "q", Limit: 5}, &Holder{}},
	} {
		res, err := NewSearcher(c.in, byPath, c.h).Search(context.Background(), c.q)
		if err != nil || res.Rank.State != search.RankOff || !slices.Equal(paths(res.Hits), []string{"d0.md", "d1.md", "d2.md", "d3.md", "d4.md"}) {
			t.Errorf("%s: %+v, %v, %v", name, res.Rank, paths(res.Hits), err)
		}
	}
	if len(f.sent) != 0 {
		t.Fatalf("a pass-through ranked: %v", f.sent)
	}
	if res, err := NewSearcher(&inner{n: 3}, byPath, nil).Search(context.Background(), search.Query{Text: "q"}); err != nil || res.Rank.State != search.RankOff {
		t.Fatalf("a nil Source: %+v, %v", res.Rank, err)
	}
}

// TestRequiredRanksOrFails: every fallback becomes ErrUnavailable with no hits, wrapping its cause;
// a facet listing and a word-less query still pass through.
func TestRequiredRanksOrFails(t *testing.T) {
	stale := func(context.Context, []search.Hit) ([]string, error) { return nil, ErrStale }
	for name, c := range map[string]struct {
		h     *Holder
		texts TextSource
		cause error
	}{
		"ranker fails": {holding(&fake{name: "m", err: ErrRateLimited}, 10), byPath, ErrRateLimited},
		"texts fail":   {holding(&fake{name: "m", score: reverse}, 10), stale, ErrStale},
		"short answer": {holding(&fake{name: "m", score: reverse, short: true}, 10), byPath, ErrBadAnswer},
		"no ranker":    {&Holder{}, byPath, nil},
	} {
		res, err := NewSearcher(&inner{n: 20}, c.texts, c.h, Required()).Search(context.Background(), search.Query{Text: "q"})
		if !errors.Is(err, ErrUnavailable) || len(res.Hits) != 0 || (c.cause != nil && !errors.Is(err, c.cause)) {
			t.Errorf("%s: %v, %d hits", name, err, len(res.Hits))
		}
	}
	for name, c := range map[string]struct {
		in *inner
		q  search.Query
	}{"facet": {&inner{n: 5, facet: true}, search.Query{Text: "type:adr"}}, "no words": {&inner{n: 5}, search.Query{}}} {
		if res, err := NewSearcher(c.in, byPath, &Holder{}, Required()).Search(context.Background(), c.q); err != nil || len(res.Hits) != 5 {
			t.Errorf("Required %s: %v, %d hits", name, err, len(res.Hits))
		}
	}
}

// TestASearchFinishesWithTheRankerItRead: a Holder set while a search is under way changes nothing
// for it; the next search reads the new ranker.
func TestASearchFinishesWithTheRankerItRead(t *testing.T) {
	a := &fake{name: "a", score: reverse}
	b := &fake{name: "b", score: reverse}
	h := holding(a, 10)
	in := &inner{n: 5, before: func() { h.Set(b, 10) }}
	res, err := NewSearcher(in, byPath, h).Search(context.Background(), search.Query{Text: "q"})
	if err != nil || res.Rank.Model != "a" || len(a.sent) != 1 || len(b.sent) != 0 {
		t.Fatalf("mid-search swap: %+v, a %d, b %d, %v", res.Rank, len(a.sent), len(b.sent), err)
	}
	in.before = nil
	if res, _ := NewSearcher(in, byPath, h).Search(context.Background(), search.Query{Text: "q"}); res.Rank.Model != "b" {
		t.Fatalf("the next search: %+v", res.Rank)
	}
}

// TestACancelledSearchIsNotAFallback: a search whose context ends while ranking returns the
// context's error, not a recall-order answer.
func TestACancelledSearchIsNotAFallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &fake{name: "m", err: context.Canceled}
	in := &inner{n: 5, before: cancel}
	if _, err := NewSearcher(in, byPath, holding(f, 10)).Search(ctx, search.Query{Text: "q"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled: %v", err)
	}
}
