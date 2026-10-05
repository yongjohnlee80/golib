package search

import (
	"context"
	"errors"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/search/query"
)

// Term is one word of a query (query.Term).
type Term = query.Term

// Fields declares the fields a query may filter on (query.Fields).
type Fields = query.Fields

// Mode is how a query searches, and how it did.
type Mode string

const (
	ModeAuto     Mode = "auto"     // hybrid when vectors can answer, else lexical
	ModeLexical  Mode = "lexical"  // by words
	ModeSemantic Mode = "semantic" // by meaning
	ModeHybrid   Mode = "hybrid"   // both, fused (a mode used, never asked for)
	ModeFacet    Mode = "facet"    // filters alone listed the documents (a mode used)
)

// State is what semantic search could do for a query.
type State string

const (
	StateOff       State = "off"       // no semantic search: no embedder, or a store without vectors
	StateReady     State = "ready"     // every document can answer by meaning
	StatePartial   State = "partial"   // some documents are not yet embedded; they answer by words
	StateSwitching State = "switching" // a new model is filling; search is by words meanwhile
	StateError     State = "error"     // the query could not be embedded
)

// Filter narrows a search to documents. Its fields combine with AND.
type Filter struct {
	// Tags: a document has every one. They are lowercased, with a leading '#' removed.
	Tags []string
	// Paths: a document is one of the paths or under one of them (a directory and what is below
	// it). They are root-relative with '/' trimmed; "" or "." anywhere in the list means no path
	// filter.
	Paths []string
	// Facets: for every field, a document has one of its values. Values are exact, as
	// Fields.FacetValue reads them.
	Facets map[string][]string
}

// Query is one search.
type Query struct {
	Text   string
	Mode   Mode // "" is ModeAuto
	Limit  int  // 0 is the engine's default; above the engine's maximum is its maximum
	Filter Filter
	// Fields are the facet fields declared now. A query's name:value words for them become filters;
	// nil declares none. They travel with each query, so a schema change applies from the next one.
	Fields Fields
}

// Candidate is one chunk a retriever found, read in the same View as every other candidate.
type Candidate[D comparable] struct {
	// Doc and Ord identify the chunk: the same (Doc, Ord) from two retrievers is one hit, and Doc is
	// what the per-document cap counts.
	Doc D
	Ord int
	// Path, Breadcrumb and Snippet present the hit. A lexical snippet marks the matched terms with
	// HighlightStart and HighlightEnd; a semantic one is the start of the chunk's text.
	Path, Breadcrumb, Snippet string
	Generation                int64 // the document's version as the store numbers it; 0 when it has none
	ByteStart, ByteEnd        int
}

// HighlightStart and HighlightEnd mark the matched terms in a lexical snippet: control characters
// no document contains, so a client renders them as it likes.
const (
	HighlightStart = "\x02"
	HighlightEnd   = "\x03"
)

// Hit is one chunk of a Result.
type Hit struct {
	Path, Breadcrumb, Snippet string
	Generation                int64
	ByteStart, ByteEnd        int
	Score                     float64 // fused and boosted; comparable only within one Result
	// Relevance is Score on a fixed scale, 0 to 1: 1 is first in every retriever the search ran,
	// before boosts; a boost past it stays at 1.
	Relevance float64
	// Via names the retrievers that found it: "lexical", "semantic" (in the order they found it),
	// or "facet"; "rank" is appended when a ranker scored it (search/rank).
	Via []string
	// RankScore is the ranker's score for the hit, comparable only within one Result; nil when no
	// ranker scored it. Relevance keeps its own meaning, the recall scale.
	RankScore *float64
}

// Result is a search's answer: the hits, and what the search could use.
type Result struct {
	Hits     []Hit
	ModeUsed Mode
	Semantic State
	// SemanticError is a constant message when Semantic is StateError: a provider's own error text
	// is not passed on.
	SemanticError string
	// Rank is what a re-ranking stage did with the hits (search/rank); the zero value for a search
	// without one.
	Rank Ranking
}

// RankState is what a re-ranking stage did for a query.
type RankState string

const (
	RankOff   RankState = "off"   // not ranked: no ranker in use, or nothing to rank (a facet listing, no words)
	RankReady RankState = "ready" // the hits are in the ranker's order
	RankError RankState = "error" // the ranker could not rank them: they are in recall order
)

// Ranking is a re-ranking stage's state for one Result: the model that ranked it, and a constant
// message when it could not.
type Ranking struct {
	State RankState
	Model string
	Error string
}

// Searcher answers queries. Engine is one; any other that satisfies Searcher can take its place.
type Searcher interface {
	Search(ctx context.Context, q Query) (Result, error)
}

// The search's errors.
var (
	// ErrUnknownMode is a mode that is none of ModeAuto, ModeLexical and ModeSemantic.
	ErrUnknownMode = errs.Sentinel(errs.ErrInvalidArgument, "search: unknown search mode")
	// ErrNoProvider is ModeSemantic asked of an engine that cannot search by meaning: it has no query
	// embedder, or its store has no vectors.
	ErrNoProvider = errs.Sentinel(errs.ErrUnsupported, "search: semantic search: no embedding provider in use")
	// ErrNoLister is a query of filters alone asked of a store that cannot list documents by them.
	ErrNoLister = errs.Sentinel(errs.ErrUnsupported, "search: the store cannot list documents by filters alone")
	// ErrSwitching is a query embedder (or a ModeSemantic search) answering while a new model fills.
	ErrSwitching = errors.New("search: a new model is filling; search by words until it is ready")
	// ErrEmbedFailed is ModeSemantic when the query could not be embedded; its message is the
	// constant SemanticError of a degraded ModeAuto search.
	ErrEmbedFailed = errors.New("search: the query could not be embedded")
	// ErrModelChanged is a Semantic view whose active model is not the one the query was embedded
	// with.
	ErrModelChanged = errors.New("search: the active model changed")
)
