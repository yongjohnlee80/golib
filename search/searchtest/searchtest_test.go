package searchtest

import (
	"context"
	"fmt"
	"math/rand/v2"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/search"
)

func TestMemStorePasses(t *testing.T) {
	Run(t, func(t testing.TB, docs []Doc) Fixture[int64, *MemView] { return NewMemStore("m", docs).Fixture() })
}

// TestAnEngineOverMemStore: the reference store under the engine, end to end.
func TestAnEngineOverMemStore(t *testing.T) {
	s := NewMemStore("m", Corpus())
	embed := func(ctx context.Context, q string) (string, []float32, error) { return "m", Embed(q), nil }
	res, err := search.NewEngine[int64, *MemView](s, search.WithQueryEmbedder(embed)).Search(context.Background(),
		search.Query{Text: "quindle", Filter: search.Filter{Tags: []string{"keep"}}})
	if err != nil || len(res.Hits) == 0 || res.Hits[0].Path != "keep/k1.md" || res.ModeUsed != search.ModeHybrid || res.Semantic != search.StatePartial {
		t.Fatalf("%+v, %v", res, err)
	}
}

// recorder is a testing.TB that records failures instead of failing, so a broken store can be shown
// to fail the suite. Fatalf ends the goroutine Run is called in.
type recorder struct {
	testing.TB
	mu   sync.Mutex
	errs []string
}

func (r *recorder) Helper() {}
func (r *recorder) Errorf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errs = append(r.errs, fmt.Sprintf(format, args...))
}
func (r *recorder) Fatalf(format string, args ...any) { r.Errorf(format, args...); runtime.Goexit() }

func (r *recorder) failures() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.errs, "\n")
}

func runRecorded[V search.View[int64]](t *testing.T, open func(t testing.TB, docs []Doc) Fixture[int64, V]) string {
	r := &recorder{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(r, open)
	}()
	<-done
	return r.failures()
}

// broken wraps a MemStore with a defect.
type broken struct {
	*MemStore
	defect string
}

type brokenView struct {
	*MemView
	s *broken
}

func (b *broken) View(ctx context.Context, fn func(v *brokenView) error) error {
	return b.MemStore.View(ctx, func(v *MemView) error { return fn(&brokenView{v, b}) })
}

func (v *brokenView) Lexical(ctx context.Context, terms []search.Term, f search.Filter, n int) ([]search.Candidate[int64], error) {
	switch v.s.defect {
	case "filter after the limit":
		all, err := v.MemView.Lexical(ctx, terms, search.Filter{}, n)
		var out []search.Candidate[int64]
		for _, c := range all {
			for _, d := range v.st.docs {
				if d.id == c.Doc && admits(d.doc, f) {
					out = append(out, c)
				}
			}
		}
		return out, err
	case "a second snapshot in one view":
		return (&MemView{st: v.s.state.Load(), model: v.model}).Lexical(ctx, terms, f, n)
	case "presentation from outside the view":
		out, err := v.MemView.Lexical(ctx, terms, f, n)
		latest := v.s.state.Load()
		for i, c := range out {
			for _, d := range latest.docs {
				if d.id == c.Doc && c.Ord < len(d.doc.Chunks) {
					out[i].Snippet = d.doc.Chunks[c.Ord]
				}
			}
		}
		return out, err
	case "ties in no set order":
		out, err := v.MemView.Lexical(ctx, terms, f, n)
		rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out, err
	}
	return v.MemView.Lexical(ctx, terms, f, n)
}

func (v *brokenView) Semantic(ctx context.Context, model string, vec []float32, f search.Filter, n int) ([]search.Candidate[int64], error) {
	if v.s.defect == "semantic answers a document not ready" {
		ready := &MemView{st: &memState{}, model: v.model}
		for _, d := range v.st.docs {
			c := *d
			c.doc.Unready = false
			ready.st.docs = append(ready.st.docs, &c)
		}
		return ready.Semantic(ctx, model, vec, f, n)
	}
	return v.MemView.Semantic(ctx, model, vec, f, n)
}

func (v *brokenView) Signals(ctx context.Context, docs []int64) (map[int64]search.Signals, error) {
	out, err := v.MemView.Signals(ctx, docs)
	if v.s.defect == "a link to itself counts" {
		for id, s := range out {
			s.InLinks++
			out[id] = s
		}
	}
	return out, err
}

func (v *brokenView) List(ctx context.Context, f search.Filter, n int) ([]search.Candidate[int64], error) {
	out, err := v.MemView.List(ctx, f, n)
	if v.s.defect == "listed in reverse" {
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
			out[i], out[j] = out[j], out[i]
		}
	}
	return out, err
}

// TestTheSuiteCatchesBrokenStores: each defect the suite names fails it, with the check that names
// it; the correct store fails nothing.
func TestTheSuiteCatchesBrokenStores(t *testing.T) {
	for defect, want := range map[string]string{
		"":                                      "",
		"filter after the limit":                "filters apply before the limit",
		"a second snapshot in one view":         "searchtest: snapshot:",
		"presentation from outside the view":    "searchtest: snapshot:",
		"ties in no set order":                  "searchtest: ties:",
		"semantic answers a document not ready": "searchtest: semantic: a document not ready answered",
		"a link to itself counts":               "searchtest: signals:",
		"listed in reverse":                     "searchtest: list:",
	} {
		got := runRecorded(t, func(t testing.TB, docs []Doc) Fixture[int64, *brokenView] {
			b := &broken{NewMemStore("m", docs), defect}
			return Fixture[int64, *brokenView]{Store: b, Model: "m", Change: b.Change}
		})
		switch {
		case want == "" && got != "":
			t.Errorf("the correct store failed:\n%s", got)
		case want != "" && !strings.Contains(got, want):
			t.Errorf("%s: the suite did not catch it (want %q):\n%s", defect, want, got)
		}
	}
}

// TestALexicalOnlyStoreSkipsTheCapabilities: a view type with Lexical alone runs only the checks
// every store owes.
func TestALexicalOnlyStoreSkipsTheCapabilities(t *testing.T) {
	got := runRecorded(t, func(t testing.TB, docs []Doc) Fixture[int64, lexicalOnly] {
		s := NewMemStore("m", docs)
		return Fixture[int64, lexicalOnly]{Store: lexicalStore{s}, Change: s.Change}
	})
	if got != "" {
		t.Errorf("a lexical-only store:\n%s", got)
	}
}

type lexicalOnly struct{ v *MemView }

func (l lexicalOnly) Lexical(ctx context.Context, terms []search.Term, f search.Filter, n int) ([]search.Candidate[int64], error) {
	return l.v.Lexical(ctx, terms, f, n)
}

type lexicalStore struct{ s *MemStore }

func (l lexicalStore) View(ctx context.Context, fn func(lexicalOnly) error) error {
	return l.s.View(ctx, func(v *MemView) error { return fn(lexicalOnly{v}) })
}
