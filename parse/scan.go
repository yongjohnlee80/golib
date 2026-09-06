package parse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"

	"github.com/yongjohnlee80/golib/streamcache"
)

// ErrUnclaimed reports a byte no form in the list recognises. Total coverage is
// the caller's to arrange — end the list with a [ByteForm] — because the core
// cannot invent a kind for a byte nothing claims, and stepping over it would
// drop input silently, which is worse than refusing.
var ErrUnclaimed = errors.New("parse: no form claims this byte")

// ErrScanClosed reports use of a Scan after Close.
var ErrScanClosed = errors.New("parse: scan is closed")

// BoundError reports a construct that outran the scan's delimiter bound. It
// names the construct, how far it ran, and the limit it broke, because a caller
// who set the limit needs all three to decide whether the input or the limit was
// wrong.
type BoundError struct {
	Kind   Kind   // the kind of the form that was still reading
	Open   string // the opening bytes, verbatim
	Offset int64  // where the construct began
	Length int    // bytes examined before giving up
	Limit  int    // the bound that was exceeded
}

func (e *BoundError) Error() string {
	return fmt.Sprintf("parse: %s opened by %q at offset %d ran past the delimiter limit "+
		"(%d bytes examined, limit %d)", e.Kind, e.Open, e.Offset, e.Length, e.Limit)
}

// Option configures a Lexer. Configuration is immutable after New.
type Option func(*lexerConfig)

type lexerConfig struct {
	forms    []Form
	maxDelim int
}

// WithForms sets the form list. ORDER IS PRECEDENCE: the first form that matches
// wins, so a caller declares `/*` before `/` and `--` before `-`. The core does
// not sort by length or guess, because a dialect's precedence is a dialect's
// knowledge. End the list so every byte is claimed — see [ByteForm].
func WithForms(f ...Form) Option {
	return func(c *lexerConfig) { c.forms = append([]Form(nil), f...) }
}

// WithMaxDelimiter bounds how far the scan will read for ONE construct before
// reporting a [BoundError]. Zero means unbounded, which is what full fidelity to
// an unbounded dialect delimiter requires, and it is the reason constant memory
// and unrestricted delimiters cannot both be unconditional.
func WithMaxDelimiter(n int) Option {
	return func(c *lexerConfig) {
		if n < 0 {
			panic(fmt.Sprintf("parse: WithMaxDelimiter(%d): a bound must not be negative", n))
		}
		c.maxDelim = n
	}
}

// Lexer is an immutable, reusable configuration. It holds no scan state, so one
// Lexer may drive any number of concurrent scans.
type Lexer struct{ cfg lexerConfig }

// New returns a Lexer configured by opts.
func New(opts ...Option) *Lexer {
	var cfg lexerConfig
	for _, o := range opts {
		o(&cfg)
	}
	return &Lexer{cfg: cfg}
}

// Ownership says whether a Scan owns the reader it was given. It is an ARGUMENT
// rather than a difference between two constructor names, so the decision is
// visible at the call site instead of hidden in a verb.
type Ownership bool

const (
	// BorrowReader leaves the reader open when the Scan closes.
	BorrowReader Ownership = false
	// OwnReader closes the reader when the Scan closes, if it is a Closer.
	OwnReader Ownership = true
)

// Scan is ONE pass over one input, and a resource: it must be closed. It is not
// safe for concurrent use.
//
// Close is eager rather than deferred to a lazily-evaluated sequence, because a
// sequence that owned an io.ReadCloser would close nothing if it were never
// ranged over, and would have nowhere to report a close failure after a caller
// stopped early.
type Scan struct {
	ctx      context.Context
	forms    []Form
	maxDelim int

	cache *streamcache.Cache
	src   *Source
	rc    io.Closer // non-nil only under OwnReader

	pos int64 // offset of the next token

	// win holds the bytes at [winFrom, winFrom+len(win)) — the window handed to
	// forms. It is reused across tokens and grown in place, so a construct is
	// copied out of the cache once rather than once per retry.
	win     []byte
	winFrom int64
	openBuf []byte // the opener, copied so a widening cannot alias it

	end     int64 // total length once known, else -1
	notedTo int64 // offset through which newlines have been recorded

	eofDone  bool
	err      error
	closed   bool
	closeErr error
}

// Scan begins a pass over r. Nothing is read until the tokens are ranged over.
func (l *Lexer) Scan(ctx context.Context, r io.Reader, own Ownership) *Scan {
	c := streamcache.New(r)
	s := l.newScan(ctx, c)
	if own == OwnReader {
		if rc, ok := r.(io.Closer); ok {
			s.rc = rc
		}
	}
	return s
}

// ScanBytes begins a pass over b WITHOUT COPYING IT. The cache is one immutable
// segment over the caller's slice, so it is never written to and never dropped.
// The caller must not mutate b for the life of the Scan.
func (l *Lexer) ScanBytes(ctx context.Context, b []byte) *Scan {
	return l.newScan(ctx, streamcache.NewBytes(b))
}

func (l *Lexer) newScan(ctx context.Context, c *streamcache.Cache) *Scan {
	if ctx == nil {
		ctx = context.Background()
	}
	s := &Scan{
		ctx:      ctx,
		forms:    l.cfg.forms,
		maxDelim: l.cfg.maxDelim,
		cache:    c,
		end:      -1,
	}
	// The Source's byte access is lifetime-bearing: it holds a View for the
	// duration of the callback and closes it before returning, so no borrowed
	// slice outlives a lookup.
	s.src = newSource(func(from, to int64, fn func(io.Reader) error) error {
		if from >= to {
			return fn(bytes.NewReader(nil))
		}
		v, err := c.Acquire(from, to)
		if err != nil {
			return err
		}
		defer v.Close()
		return fn(v.Reader())
	})
	return s
}

// Tokens yields the token stream. It stops at the first error, which it yields,
// and after the EOF token otherwise. Ranging stops early without corrupting the
// Scan, but the Scan must still be closed.
func (s *Scan) Tokens() iter.Seq2[Token, error] {
	return func(yield func(Token, error) bool) {
		for {
			tok, ok, err := s.step()
			if err != nil {
				yield(Token{}, err)
				return
			}
			if !ok {
				return
			}
			if !yield(tok, nil) {
				return
			}
		}
	}
}

// Acquire returns the bytes of t, with the lifetime that keeps them valid. The
// caller closes the View. A scan releases retention behind itself as it advances,
// so acquire a token's bytes while it is still the recent past — a View, once
// held, keeps its own bytes alive however far the scan runs on.
func (s *Scan) Acquire(t Token) (*streamcache.View, error) {
	if s.closed {
		return nil, ErrScanClosed
	}
	return s.cache.Acquire(t.Start, t.End)
}

// LocationAt resolves an offset to a line and column, for a diagnostic. It is
// answered from the live window: see [Source].
func (s *Scan) LocationAt(off int64) (Location, error) {
	if s.closed {
		return Location{}, ErrScanClosed
	}
	return s.src.LocationAt(off)
}

// Close releases the scan's retention and, under OwnReader, closes the reader and
// reports its error. It is idempotent, and it does its work even for a Scan that
// was never ranged over.
func (s *Scan) Close() error {
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	s.cache.Release(math.MaxInt64) // nothing is needed any more
	if s.rc != nil {
		s.closeErr = s.rc.Close()
	}
	return s.closeErr
}

// step produces the next token. ok is false once the stream is finished.
func (s *Scan) step() (Token, bool, error) {
	if s.err != nil {
		return Token{}, false, s.err
	}
	if s.closed {
		return Token{}, false, ErrScanClosed
	}
	if s.eofDone {
		return Token{}, false, nil
	}
	if err := s.ctx.Err(); err != nil {
		return s.fail(err)
	}

	win, atEnd, err := s.fill(1)
	if err != nil {
		return s.fail(err)
	}
	if len(win) == 0 && atEnd {
		// EOF IS A TOKEN, with a real position, so a caller ranging the stream
		// sees where the input stopped rather than inferring it from exhaustion.
		s.eofDone = true
		return Token{Kind: EOF, Start: s.pos, End: s.pos}, true, nil
	}

	tok, err := s.lexOne()
	if err != nil {
		return s.fail(err)
	}
	s.pos = tok.End
	// Release behind the scan, to the LINE boundary the Source can still resolve
	// a column from — cache and index advance to the same offset, which is what
	// keeps a retained token's location exact.
	//
	// Relative to the token's START, not to the new position: a token that ends
	// on a newline begins a line at exactly s.pos, and reclaiming to there would
	// release the token being handed back in this very call, so the caller could
	// not Acquire what it had just been given.
	s.cache.Release(s.src.reclaim(tok.Start))
	return tok, true, nil
}

func (s *Scan) fail(err error) (Token, bool, error) {
	s.err = err
	return Token{}, false, err
}

// lexOne walks the form list at s.pos and returns the one token that starts
// there.
func (s *Scan) lexOne() (Token, error) {
	start := s.pos
	want := 1
	for {
		win, atEnd, err := s.fill(want)
		if err != nil {
			return Token{}, err
		}
		boundary := MoreInput
		if atEnd {
			boundary = EndOfInput
		}

		opened := -1
		openN := 0
		stalledAt := -1
		for i, f := range s.forms {
			n, m := f.Starts(win)
			switch m {
			case Matched:
				if n <= 0 || n > len(win) {
					return Token{}, contractf(f, "Starts returned (%d, Matched) for a %d-byte "+
						"window: want 0 < n <= %d — n <= 0 returns the scan to this offset "+
						"forever, n > len(src) claims bytes it was not shown", n, len(win), len(win))
				}
				opened, openN = i, n
			case NoMatch, Incomplete:
				if n != 0 {
					return Token{}, contractf(f, "Starts returned (%d, %v): n must be 0 for %v", n, m, m)
				}
			default:
				return Token{}, contractf(f, "Starts returned %v, which is not a Match value", m)
			}
			if m == Matched {
				break
			}
			if m == Incomplete {
				if boundary == EndOfInput {
					// A CLAIM CONDITIONAL ON MORE INPUT, and the condition is
					// false: the walk resumes and a shorter form may win, which is
					// how a source ending in "/" lexes as an operator rather than
					// an unterminated block comment. Without this the scan widens
					// a window that can never grow — it does not merely mis-lex,
					// it fails to terminate.
					continue
				}
				// It blocks the forms behind it: one of them matching now could be
				// overruled by this one once more bytes arrive, and a token
				// emitted cannot be taken back.
				stalledAt = i
				break
			}
		}

		if opened >= 0 {
			return s.closeConstruct(s.forms[opened], start, openN)
		}
		if stalledAt >= 0 {
			// Widen and restart the walk AT THE SAME OFFSET, from the top of the
			// list. If the stream ends instead, the next pass sees EndOfInput and
			// the Incomplete degrades, so this cannot spin.
			if err := s.widen(&want, s.forms[stalledAt].Kind(), win, start, len(win)); err != nil {
				return Token{}, err
			}
			continue
		}
		return Token{}, fmt.Errorf("%w: offset %d (0x%02x)", ErrUnclaimed, start, win[0])
	}
}

// closeConstruct resolves where the construct opened by f ends.
func (s *Scan) closeConstruct(f Form, start int64, openN int) (Token, error) {
	s.openBuf = append(s.openBuf[:0], s.win[:openN]...)
	restWant := 0
	for {
		win, atEnd, err := s.fill(openN + restWant)
		if err != nil {
			return Token{}, err
		}
		boundary := MoreInput
		if atEnd {
			boundary = EndOfInput
		}
		if len(win) < openN {
			return Token{}, contractf(f, "the window shrank below the %d-byte opener", openN)
		}
		rest := win[openN:]

		n, err := f.End(rest, s.openBuf, boundary)
		switch {
		case err == nil:
			if n < 0 || n > len(rest) {
				return Token{}, contractf(f, "End returned %d for a %d-byte remainder: want "+
					"0 <= n <= %d", n, len(rest), len(rest))
			}
			return Token{Kind: f.Kind(), Start: start, End: start + int64(openN) + int64(n)}, nil

		case errors.Is(err, ErrNeedMore):
			if n != 0 {
				return Token{}, contractf(f, "End returned (%d, ErrNeedMore): n must be 0 when "+
					"no decision was reached", n)
			}
			if boundary == EndOfInput {
				// It asks for input that cannot exist. Honouring it loops forever
				// and ignoring it picks one of two answers only the form knows.
				return Token{}, contractf(f, "End returned ErrNeedMore at EndOfInput, asking "+
					"for input that cannot arrive")
			}
			if gerr := s.widen(&restWant, f.Kind(), win, start, openN+restWant); gerr != nil {
				return Token{}, gerr
			}
			continue

		default:
			// A terminal report — an *UnterminatedError, or whatever else the form
			// judged fatal. The form saw a window, not the stream, so the offset
			// it started at is the core's to attach; %w keeps the form's own type
			// reachable with errors.As.
			return Token{}, fmt.Errorf("parse: at offset %d: %w", start, err)
		}
	}
}

// widen grows a window request geometrically, and refuses once the construct
// would run past the delimiter bound.
func (s *Scan) widen(want *int, kind Kind, win []byte, start int64, examined int) error {
	next := *want * 2
	if next <= *want {
		next = *want + 1
	}
	if s.maxDelim > 0 && next > s.maxDelim {
		open := string(win)
		if len(open) > 32 {
			open = open[:32]
		}
		return &BoundError{
			Kind: kind, Open: open, Offset: start,
			Length: examined, Limit: s.maxDelim,
		}
	}
	*want = next
	return nil
}

// fill grows the window to at least want bytes from s.pos and reports whether it
// now shows the whole remainder of the stream.
//
// The window is appended to in place, so the bytes of a long construct are copied
// out of the cache once rather than once per retry.
func (s *Scan) fill(want int) ([]byte, bool, error) {
	if s.winFrom != s.pos {
		s.win = s.win[:0]
		s.winFrom = s.pos
	}
	if len(s.win) < want {
		avail, err := s.cache.Ensure(s.pos, want)
		if err != nil {
			return nil, false, err
		}
		s.src.advanceHead(s.cache.Head())
		if avail < want {
			// Ensure reads until want bytes are there or the stream ends, so a
			// short answer is the end of the stream.
			s.end = s.pos + int64(avail)
		}
		if avail > len(s.win) {
			from := s.pos + int64(len(s.win))
			to := s.pos + int64(avail)
			v, err := s.cache.Acquire(from, to)
			if err != nil {
				return nil, false, err
			}
			s.win, err = v.AppendTo(s.win)
			v.Close()
			if err != nil {
				return nil, false, err
			}
			s.noteNewlines(from, to)
		}
	}
	atEnd := s.end >= 0 && s.pos+int64(len(s.win)) >= s.end
	return s.win, atEnd, nil
}

// noteNewlines records the line starts in the newly read range, once each. The
// high-water mark is what keeps a re-filled window from recording a line twice.
func (s *Scan) noteNewlines(from, to int64) {
	if from < s.notedTo {
		from = s.notedTo
	}
	for off := from; off < to; off++ {
		if s.win[off-s.winFrom] == '\n' {
			s.src.noteNewlineAt(off)
		}
	}
	if to > s.notedTo {
		s.notedTo = to
	}
}
