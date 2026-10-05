package rank

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

// Ranker scores texts against a query.
type Ranker interface {
	// Rank scores each text's relevance to query: exactly len(texts) scores, Scores[i] is
	// texts[i]'s, higher is better, comparable only within one call. It returns every score or an
	// error, never a partial answer.
	Rank(ctx context.Context, query string, texts []string) ([]float64, error)
	Model() Model
}

// Model names a ranker's model, and the largest batch its server takes in one request.
type Model struct {
	Provider string // "tei" | "rerank-api"
	Name     string
	MaxBatch int
}

// Call is one request to a ranker, as its meter hears of it: how many texts, the tokens the server
// counted (0 where it reports none), how it went, and how long it took.
type Call struct {
	Texts, Tokens int
	Err           error
	Duration      time.Duration
}

// A Metered ranker reports every call to the meter set on it.
type Metered interface {
	SetMeter(fn func(Call))
}

type meter struct{ fn func(Call) }

func (m *meter) SetMeter(fn func(Call)) { m.fn = fn }

func (m *meter) report(texts, tokens int, err error, start time.Time) {
	if m.fn != nil {
		m.fn(Call{Texts: texts, Tokens: tokens, Err: err, Duration: time.Since(start)})
	}
}

// The rankers' errors. A provider's own error text is never passed on: these say what happened.
var (
	ErrUnreachable  = errors.New("rank: the ranker did not answer")
	ErrUnauthorized = errors.New("rank: the ranker refused the key")
	ErrNoModel      = errors.New("rank: the ranker has no such model")
	ErrNotARanker   = errors.New("rank: the server's model is not a re-ranker")
	ErrRateLimited  = errors.New("rank: the ranker's usage limit is reached")
	ErrBadAnswer    = errors.New("rank: the ranker's answer is not one score for each text")
	ErrRefused      = errors.New("rank: the ranker refused the request")
)

// Source is where a Searcher reads its ranker and window at the start of each search: nil for no
// ranker in use.
type Source interface {
	Current() (Ranker, int)
}

// The window's bounds and default: how many of the inner Searcher's top hits are ranked.
const (
	MinWindow     = 10
	MaxWindow     = 100
	DefaultWindow = 40
)

// Holder is a Source a host sets: an atomic pointer to the ranker in use and its window. A search
// reads it once, at its start, so setting it never waits for one and never changes one under way.
// The zero value holds no ranker.
type Holder struct{ p atomic.Pointer[held] }

type held struct {
	r      Ranker
	window int
}

// Set makes r the ranker in use, ranking the top window hits (clamped to MinWindow..MaxWindow; 0
// is DefaultWindow). A nil r holds none.
func (h *Holder) Set(r Ranker, window int) {
	if r == nil {
		h.p.Store(nil)
		return
	}
	h.p.Store(&held{r: r, window: Window(window)})
}

// Current is the ranker in use and its window; nil when none.
func (h *Holder) Current() (Ranker, int) {
	if v := h.p.Load(); v != nil {
		return v.r, v.window
	}
	return nil, 0
}

// Window is n clamped to the window's bounds; 0 is DefaultWindow.
func Window(n int) int {
	if n == 0 {
		return DefaultWindow
	}
	return min(max(n, MinWindow), MaxWindow)
}
