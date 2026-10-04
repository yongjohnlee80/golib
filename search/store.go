package search

import "context"

// Store is a consumer's index, as the Engine reads it: one consistent snapshot per View. The
// consumer owns the tables, the SQL and the write path; the Engine reads only through the View it
// is given, so every hit's text, path and generation come from one snapshot.
//
// D is the store's document key, compared only for equality. V is the store's view type, and its
// static method set decides what the Engine asks of it (see NewEngine).
type Store[D comparable, V View[D]] interface {
	View(ctx context.Context, fn func(v V) error) error
}

// View is what every store answers: lexical retrieval.
type View[D comparable] interface {
	// Lexical is at most n candidates matching every term, best first, with f applied before the
	// rank and the limit. Equal ranks are ordered by path, then the backend's own stable order.
	Lexical(ctx context.Context, terms []Term, f Filter, n int) ([]Candidate[D], error)
}

// Semantic is a View that searches by meaning. A store without vectors does not implement it.
type Semantic[D comparable] interface {
	// Semantic is at most n candidates nearest vec, best first by dot product (vec is normalized),
	// then path, then ordinal, with f applied before the limit. It answers only from alive chunks of
	// documents that are ready under the active model, in this View's snapshot, and returns
	// ErrModelChanged when the active model is not model. A store that scans an in-memory index
	// uses it only when it is the snapshot's (vector.Index.Usable) and scans its stored codes
	// otherwise.
	Semantic(ctx context.Context, model string, vec []float32, f Filter, n int) ([]Candidate[D], error)
	// SemanticState is StateReady, StatePartial or StateSwitching, read in this View.
	SemanticState(ctx context.Context) (State, error)
}

// Signals are a document's boosts: the documents linking to it (itself not counted), and its tags,
// lowercased.
type Signals struct {
	InLinks int
	Tags    []string
}

// Signaler is a View that knows its documents' links and tags. Without it, nothing is boosted.
type Signaler[D comparable] interface {
	Signals(ctx context.Context, docs []D) (map[D]Signals, error)
}

// Lister is a View that lists documents by filters alone: those f admits, in path order, each as
// its first chunk (ordinal 0) with the start of its text as the snippet, at most n.
type Lister[D comparable] interface {
	List(ctx context.Context, f Filter, n int) ([]Candidate[D], error)
}
