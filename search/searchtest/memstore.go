package searchtest

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"

	"github.com/yongjohnlee80/golib/search"
	"github.com/yongjohnlee80/golib/search/chunk"
	"github.com/yongjohnlee80/golib/search/vector"
)

// MemStore is a search.Store held in memory: a reference implementation of the port, and a store
// for tests of code that searches. Each View reads one immutable state, so a Change during a View
// is not seen in it. Its views are Semantic, Signaler and Lister; documents are keyed by an int64
// that a document keeps across changes.
//
// Lexical search matches whole words, case-insensitively (a prefix term, the start of a word), and
// ranks by the share of a chunk's words that match. Semantic search ranks by dot product with the
// stored vectors, which are Embed of each chunk's body.
type MemStore struct {
	model string
	mu    sync.Mutex // serializes changes
	state atomic.Pointer[memState]
}

type memState struct {
	docs []*memDoc // by path
}

type memDoc struct {
	id      int64
	doc     Doc
	vectors [][]float32
}

// NewMemStore holds docs, embedded with model (Embed of each chunk's body).
func NewMemStore(model string, docs []Doc) *MemStore {
	s := &MemStore{model: model}
	st := &memState{}
	for i, d := range docs {
		st.docs = append(st.docs, newMemDoc(int64(i+1), d))
	}
	slices.SortFunc(st.docs, func(a, b *memDoc) int { return strings.Compare(a.doc.Path, b.doc.Path) })
	s.state.Store(st)
	return s
}

func newMemDoc(id int64, d Doc) *memDoc {
	m := &memDoc{id: id, doc: d}
	for _, c := range d.Chunks {
		m.vectors = append(m.vectors, Embed(c))
	}
	return m
}

// Fixture is the store as Run's fixture.
func (s *MemStore) Fixture() Fixture[int64, *MemView] {
	return Fixture[int64, *MemView]{Store: s, Model: s.model, Change: s.Change}
}

// Change replaces path's chunks (adding the document when it is new). Views already open keep the
// state they read.
func (s *MemStore) Change(path string, chunks []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old := s.state.Load()
	next := &memState{docs: slices.Clone(old.docs)}
	i, found := slices.BinarySearchFunc(next.docs, path, func(d *memDoc, p string) int { return strings.Compare(d.doc.Path, p) })
	if found {
		d := next.docs[i].doc
		d.Chunks = slices.Clone(chunks)
		next.docs[i] = newMemDoc(next.docs[i].id, d)
	} else {
		var id int64
		for _, d := range old.docs {
			id = max(id, d.id)
		}
		next.docs = slices.Insert(next.docs, i, newMemDoc(id+1, Doc{Path: path, Chunks: slices.Clone(chunks)}))
	}
	s.state.Store(next)
}

// View runs fn over the current state.
func (s *MemStore) View(ctx context.Context, fn func(v *MemView) error) error {
	return fn(&MemView{st: s.state.Load(), model: s.model})
}

// MemView is one MemStore state.
type MemView struct {
	st    *memState
	model string
}

// admits reports whether f admits d.
func admits(d Doc, f search.Filter) bool {
	for _, t := range f.Tags {
		if !slices.Contains(d.Tags, t) {
			return false
		}
	}
	if len(f.Paths) > 0 && !slices.ContainsFunc(f.Paths, func(p string) bool { return p == "" || p == "." }) {
		if !slices.ContainsFunc(f.Paths, func(p string) bool {
			p = strings.Trim(p, "/")
			return d.Path == p || strings.HasPrefix(d.Path, p+"/")
		}) {
			return false
		}
	}
	for field, values := range f.Facets {
		if !slices.ContainsFunc(values, func(v string) bool { return slices.Contains(d.Facets[field], v) }) {
			return false
		}
	}
	return true
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}

type scored struct {
	c     search.Candidate[int64]
	score float64
}

// best orders scored candidates by score, then path, then ordinal, and keeps n.
func best(all []scored, n int) []search.Candidate[int64] {
	slices.SortFunc(all, func(a, b scored) int {
		switch {
		case a.score != b.score:
			if a.score > b.score {
				return -1
			}
			return 1
		case a.c.Path != b.c.Path:
			return strings.Compare(a.c.Path, b.c.Path)
		}
		return a.c.Ord - b.c.Ord
	})
	out := make([]search.Candidate[int64], 0, min(n, len(all)))
	for _, s := range all[:min(max(n, 0), len(all))] {
		out = append(out, s.c)
	}
	return out
}

func candidate(d *memDoc, ord int, snippet string) search.Candidate[int64] {
	return search.Candidate[int64]{Doc: d.id, Ord: ord, Path: d.doc.Path, Breadcrumb: d.doc.Path, Snippet: snippet}
}

// Lexical implements search.View.
func (v *MemView) Lexical(ctx context.Context, terms []search.Term, f search.Filter, n int) ([]search.Candidate[int64], error) {
	var all []scored
	for _, d := range v.st.docs {
		if !admits(d.doc, f) {
			continue
		}
		for ord, body := range d.doc.Chunks {
			ws := words(body)
			hits, ok := 0, len(terms) > 0
			for _, t := range terms {
				want := strings.ToLower(t.Text)
				n := 0
				for _, w := range ws {
					if w == want || (t.Prefix && strings.HasPrefix(w, want)) {
						n++
					}
				}
				ok = ok && n > 0
				hits += n
			}
			if ok {
				all = append(all, scored{candidate(d, ord, mark(body, terms)), float64(hits) / float64(len(ws))})
			}
		}
	}
	return best(all, n), nil
}

// mark puts the highlight marks around the words of body the terms match.
func mark(body string, terms []search.Term) string {
	out := strings.Fields(body)
	for i, w := range out {
		lw := strings.ToLower(w)
		for _, t := range terms {
			want := strings.ToLower(t.Text)
			if lw == want || (t.Prefix && strings.HasPrefix(lw, want)) {
				out[i] = search.HighlightStart + w + search.HighlightEnd
				break
			}
		}
	}
	return strings.Join(out, " ")
}

// Semantic implements search.Semantic.
func (v *MemView) Semantic(ctx context.Context, model string, vec []float32, f search.Filter, n int) ([]search.Candidate[int64], error) {
	if model != v.model {
		return nil, search.ErrModelChanged
	}
	var all []scored
	for _, d := range v.st.docs {
		if d.doc.Unready || !admits(d.doc, f) {
			continue
		}
		for ord, body := range d.doc.Chunks {
			all = append(all, scored{candidate(d, ord, chunk.Snippet(body)), vector.Dot(vec, d.vectors[ord])})
		}
	}
	return best(all, n), nil
}

// SemanticState implements search.Semantic.
func (v *MemView) SemanticState(ctx context.Context) (search.State, error) {
	for _, d := range v.st.docs {
		if d.doc.Unready {
			return search.StatePartial, nil
		}
	}
	return search.StateReady, nil
}

// Signals implements search.Signaler.
func (v *MemView) Signals(ctx context.Context, docs []int64) (map[int64]search.Signals, error) {
	byPath := map[string]int64{}
	for _, d := range v.st.docs {
		byPath[d.doc.Path] = d.id
	}
	inLinks := map[int64]map[int64]bool{}
	for _, d := range v.st.docs {
		for _, l := range d.doc.Links {
			if dst, ok := byPath[l]; ok && dst != d.id {
				if inLinks[dst] == nil {
					inLinks[dst] = map[int64]bool{}
				}
				inLinks[dst][d.id] = true
			}
		}
	}
	out := map[int64]search.Signals{}
	for _, d := range v.st.docs {
		if slices.Contains(docs, d.id) {
			out[d.id] = search.Signals{InLinks: len(inLinks[d.id]), Tags: slices.Clone(d.doc.Tags)}
		}
	}
	return out, nil
}

// List implements search.Lister.
func (v *MemView) List(ctx context.Context, f search.Filter, n int) ([]search.Candidate[int64], error) {
	var out []search.Candidate[int64]
	for _, d := range v.st.docs {
		if len(out) == n {
			break
		}
		if admits(d.doc, f) && len(d.doc.Chunks) > 0 {
			out = append(out, candidate(d, 0, chunk.Snippet(d.doc.Chunks[0])))
		}
	}
	return out, nil
}

var (
	_ search.Store[int64, *MemView] = (*MemStore)(nil)
	_ search.Semantic[int64]        = (*MemView)(nil)
	_ search.Signaler[int64]        = (*MemView)(nil)
	_ search.Lister[int64]          = (*MemView)(nil)
)
