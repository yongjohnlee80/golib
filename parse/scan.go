package parse

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"math"
	"unicode/utf8"

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
	Length int    // bytes examined before giving up — the whole construct's width
	Limit  int    // the bound that was exceeded
}

func (e *BoundError) Error() string {
	return fmt.Sprintf("parse: %s opened by %q at offset %d ran past the delimiter limit "+
		"(%d bytes examined, limit %d)", e.Kind, e.Open, e.Offset, e.Length, e.Limit)
}

// contractAt reports a form that broke the interface, naming its POSITION in the
// list as well as its type: a list may hold several instances of one type, and
// %T alone cannot say which of them misbehaved.
func contractAt(i int, f Form, format string, args ...any) error {
	return fmt.Errorf("%w: form[%d] (%T): %s", ErrFormContract, i, f, fmt.Sprintf(format, args...))
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
// reporting a [BoundError]. The bound is INCLUSIVE: a construct exactly that
// wide is legal, and only one still undecided after being shown exactly that
// many bytes is refused.
//
// Zero means unbounded, which is what full fidelity to an unbounded dialect
// delimiter requires, and it is the reason constant memory and unrestricted
// delimiters cannot both be unconditional.
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
//
// # Two window providers, one state machine
//
// A Form is handed a CONTIGUOUS window. Over a []byte the caller's slice already
// is one, so it is resliced and nothing is copied; over a stream the bytes are
// segmented, so they are copied once into a reused buffer that grows in place.
// The walk itself does not know which provider it is running on.
type Scan struct {
	ctx      context.Context
	forms    []Form
	maxDelim int

	cache *streamcache.Cache
	src   *Source
	rc    io.Closer // non-nil only under OwnReader

	// fixed is the whole input when it was supplied as a slice. Non-nil selects
	// the no-copy window provider.
	fixed []byte

	pos int64 // offset of the next token

	// win holds the bytes at [winFrom, winFrom+len(win)) — the window handed to
	// forms. Under the streaming provider it is a reused buffer appended to in
	// place, so a construct is copied out of the cache once rather than once per
	// retry; under the fixed provider it is a reslice of the input.
	win     []byte
	winFrom int64
	openBuf []byte // the opener, copied when a widening could otherwise alias it
	idxBuf  []byte // scratch for the bounded lookahead a location may need

	end     int64 // total length once known, else -1
	notedTo int64 // offset through which newlines have been INDEXED

	eofDone bool
	// failed records THAT the scan failed, as a scalar, so Close can drop the
	// error value itself without losing the fact.
	failed   bool
	err      error
	closed   bool
	closeErr error
}

// Scan begins a pass over r. Nothing is read until the tokens are ranged over.
func (l *Lexer) Scan(ctx context.Context, r io.Reader, own Ownership) *Scan {
	s := l.newScan(ctx, streamcache.New(r), nil)
	if own == OwnReader {
		if rc, ok := r.(io.Closer); ok {
			s.rc = rc
		}
	}
	return s
}

// ScanBytes begins a pass over b WITHOUT COPYING IT — not into the cache, and
// not into the windows handed to forms, which are reslices of b. The caller must
// not mutate b for the life of the Scan.
func (l *Lexer) ScanBytes(ctx context.Context, b []byte) *Scan {
	return l.newScan(ctx, streamcache.NewBytes(b), b)
}

func (l *Lexer) newScan(ctx context.Context, c *streamcache.Cache, fixed []byte) *Scan {
	if ctx == nil {
		ctx = context.Background()
	}
	s := &Scan{
		ctx:      ctx,
		forms:    l.cfg.forms,
		maxDelim: l.cfg.maxDelim,
		cache:    c,
		fixed:    fixed,
		end:      -1,
	}
	if fixed != nil {
		s.end = int64(len(fixed))
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
// caller closes the View.
//
// EXACT LIFETIME: a token is guaranteed acquirable while the iteration is still
// inside the yield that produced it. Once the next token is requested the scan
// has released behind itself, and only a View already held is guaranteed — a
// View, once held, keeps its own bytes alive however far the scan runs on.
func (s *Scan) Acquire(t Token) (*streamcache.View, error) {
	if s.closed {
		return nil, ErrScanClosed
	}
	return s.cache.Acquire(t.Start, t.End)
}

// LocationAt resolves an offset to a line and column, for a diagnostic.
//
// A SUCCESSFUL LOCATION IS NEVER PROVISIONAL. Resolving needs the rune that ends
// at or straddles the offset to be decodable, so this indexes a lookahead first
// — at most utf8.UTFMax-1 bytes past the offset, or to the end of input. Without
// it the same offset could answer with a column while a multi-byte rune was still
// arriving, then refuse once it completed.
//
// AND THE LOOKAHEAD IS ALL IT WILL READ. Over a stream, an offset ahead of what
// has been indexed is refused BEFORE any I/O: a diagnostic is for an offset the
// scan has already reached, and driving the reader forward to answer one would
// turn a question about the past into an unbounded read. Over a slice the whole
// input is already in memory, so any in-range offset is answered by indexing
// forward, which costs no I/O at all.
//
// An offset inside a multibyte rune is not a position and is refused — which
// byte-oriented forms can produce, so the refusal has to be stable rather than
// dependent on how much has been read.
func (s *Scan) LocationAt(off int64) (Location, error) {
	if s.closed {
		return Location{}, ErrScanClosed
	}
	if off >= 0 {
		if s.fixed == nil && off > s.notedTo {
			return Location{}, fmt.Errorf("%w: offset %d is ahead of the indexed head %d — a "+
				"streamed location is not read forward for", ErrLocationRange, off, s.notedTo)
		}
		if err := s.indexThrough(off + int64(utf8.UTFMax) - 1); err != nil {
			return Location{}, err
		}
	}
	return s.src.LocationAt(off)
}

// Close releases the scan's retention and, under OwnReader, closes the reader and
// reports its error. It is idempotent, and it does its work even for a Scan that
// was never ranged over.
//
// It also DROPS what the Scan itself holds — the window buffer, which may be as
// large as the widest construct seen, and the cache and source graph. A View the
// caller still holds keeps the cache alive on its own, so letting go here cannot
// invalidate one.
func (s *Scan) Close() error {
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	s.cache.Release(math.MaxInt64) // nothing is needed any more
	if s.rc != nil {
		s.closeErr = s.rc.Close()
	}
	// EVERY reference the Scan owns, not just the obvious buffers: a form list
	// can close over as much as its author likes, a context can carry a whole
	// value graph, and a terminal error can wrap anything. What is kept is scalar
	// terminal state plus closeErr, which the caller may still ask for.
	s.win, s.openBuf, s.idxBuf, s.fixed = nil, nil, nil, nil
	s.cache, s.src, s.rc = nil, nil, nil
	s.forms, s.ctx, s.err = nil, nil, nil
	return s.closeErr
}

// step produces the next token. ok is false once the stream is finished.
func (s *Scan) step() (Token, bool, error) {
	// CLOSED IS CHECKED FIRST, because Close drops the error value along with
	// every other reference: a Scan that failed and was then closed reports
	// ErrScanClosed, which is the truthful answer about a resource that is gone.
	// Before Close, a failed scan keeps reporting what went wrong.
	if s.closed {
		return Token{}, false, ErrScanClosed
	}
	if s.err != nil {
		return Token{}, false, s.err
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
	// release the token being handed back in this very call.
	s.cache.Release(s.src.reclaim(tok.Start))
	return tok, true, nil
}

func (s *Scan) fail(err error) (Token, bool, error) {
	s.failed, s.err = true, err
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
					return Token{}, contractAt(i, f, "Starts returned (%d, Matched) for a %d-byte "+
						"window: want 0 < n <= %d — n <= 0 returns the scan to this offset "+
						"forever, n > len(src) claims bytes it was not shown", n, len(win), len(win))
				}
				opened, openN = i, n
			case NoMatch, Incomplete:
				if n != 0 {
					return Token{}, contractAt(i, f, "Starts returned (%d, %v): n must be 0 for %v", n, m, m)
				}
			default:
				return Token{}, contractAt(i, f, "Starts returned %v, which is not a Match value", m)
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
			return s.closeConstruct(opened, s.forms[opened], start, openN)
		}
		if stalledAt >= 0 {
			// Widen and restart the walk AT THE SAME OFFSET, from the top of the
			// list. If the stream ends instead, the next pass sees EndOfInput and
			// the Incomplete degrades, so this cannot spin.
			f := s.forms[stalledAt]
			if err := s.widen(&want, 0, f.Kind(), string(win), start); err != nil {
				return Token{}, err
			}
			continue
		}
		return Token{}, fmt.Errorf("%w: offset %d (0x%02x)", ErrUnclaimed, start, win[0])
	}
}

// closeConstruct resolves where the construct opened by f ends, enforcing the
// whole End matrix rather than the part that is convenient.
func (s *Scan) closeConstruct(idx int, f Form, start int64, openN int) (Token, error) {
	restWant := 0
	for {
		win, atEnd, err := s.fill(openN + restWant)
		if err != nil {
			return Token{}, err
		}
		if len(win) < openN {
			return Token{}, contractAt(idx, f, "the window shrank below the %d-byte opener", openN)
		}
		boundary := MoreInput
		if atEnd {
			boundary = EndOfInput
		}
		opener := s.opener(win, openN)
		rest := win[openN:]

		n, err := f.End(rest, opener, boundary)
		switch {
		case err == nil:
			if n < 0 || n > len(rest) {
				return Token{}, contractAt(idx, f, "End returned %d for a %d-byte remainder: want "+
					"0 <= n <= %d", n, len(rest), len(rest))
			}
			return Token{Kind: f.Kind(), Start: start, End: start + int64(openN) + int64(n)}, nil

		case errors.Is(err, ErrNeedMore):
			if n != 0 {
				return Token{}, contractAt(idx, f, "End returned (%d, ErrNeedMore): n must be 0 when "+
					"no decision was reached — a count there claims bytes the form just said it "+
					"could not judge", n)
			}
			if boundary == EndOfInput {
				// It asks for input that cannot exist. Honouring it loops forever
				// and ignoring it picks one of two answers only the form knows.
				return Token{}, contractAt(idx, f, "End returned ErrNeedMore at EndOfInput, asking "+
					"for input that cannot arrive")
			}
			if err := s.widen(&restWant, openN, f.Kind(), string(opener), start); err != nil {
				return Token{}, err
			}
			continue

		default:
			// A TERMINAL REPORT, and the matrix is narrow about when one is legal.
			if n != 0 {
				return Token{}, contractAt(idx, f, "End returned (%d, %v): n must be 0 alongside an "+
					"error", n, err)
			}
			if boundary == MoreInput {
				return Token{}, contractAt(idx, f, "End returned %v while more input may arrive: the "+
					"only refusal available then is ErrNeedMore, because reporting the construct "+
					"terminal here decides against bytes that have not been read yet", err)
			}
			var unterm *UnterminatedError
			if !errors.As(err, &unterm) {
				return Token{}, contractAt(idx, f, "End returned %v at EndOfInput: a construct that "+
					"cannot close at end of input reports *parse.UnterminatedError, so a caller can "+
					"name what was left open", err)
			}
			// The form saw a window, not the stream, so the offset it started at
			// is the core's to attach; %w keeps the form's own type reachable.
			return Token{}, fmt.Errorf("parse: at offset %d: %w", start, err)
		}
	}
}

// opener returns the construct's opening bytes. Under the fixed provider the
// backing array never moves, so the opener is an alias; under the streaming
// provider the window is appended to in place and a widening may reallocate it,
// so the opener must be copied out.
func (s *Scan) opener(win []byte, openN int) []byte {
	if s.fixed != nil {
		return win[:openN:openN]
	}
	s.openBuf = append(s.openBuf[:0], win[:openN]...)
	return s.openBuf
}

// widen grows a window request geometrically and refuses once the construct has
// been shown its whole allowance.
//
// base is what the request excludes — the opener, while End is resolving — so
// the bound applies to the WHOLE construct rather than to the remainder alone.
// The limit is INCLUSIVE: growth is clamped to it, so a construct exactly that
// wide is examined, and only one still undecided at exactly that width fails.
func (s *Scan) widen(want *int, base int, kind Kind, open string, start int64) error {
	next := *want * 2
	if next <= *want {
		next = *want + 1
	}
	if s.maxDelim > 0 && base+next > s.maxDelim {
		next = s.maxDelim - base
	}
	if next <= *want {
		if len(open) > 64 {
			open = open[:64]
		}
		return &BoundError{
			Kind: kind, Open: open, Offset: start,
			Length: base + *want, Limit: s.maxDelim,
		}
	}
	*want = next
	return nil
}

// fill grows the window to at least want bytes from s.pos and reports whether it
// now shows the whole remainder of the stream.
func (s *Scan) fill(want int) ([]byte, bool, error) {
	if s.fixed != nil {
		return s.fillFixed(want)
	}
	if s.winFrom != s.pos {
		s.win = s.win[:0]
		s.winFrom = s.pos
	}
	if len(s.win) < want {
		avail, err := s.cache.Ensure(s.pos, want)
		if err != nil {
			return nil, false, err
		}
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
			s.indexNewlines(from, s.win[from-s.winFrom:])
		}
	}
	atEnd := s.end >= 0 && s.pos+int64(len(s.win)) >= s.end
	return s.win, atEnd, nil
}

// fillFixed is the no-copy provider: the caller's slice already is a contiguous
// window, so the request is a reslice and nothing is read or copied.
func (s *Scan) fillFixed(want int) ([]byte, bool, error) {
	total := int64(len(s.fixed))
	from := s.pos
	if from > total {
		from = total
	}
	to := from + int64(want)
	if to > total || to < from {
		to = total
	}
	s.winFrom = from
	s.win = s.fixed[from:to:to]
	s.indexNewlines(from, s.win)
	return s.win, to >= total, nil
}

// indexNewlines records the line starts in buf, which begins at offset abs. The
// high-water mark is what keeps a re-filled or resliced window from recording a
// line twice.
func (s *Scan) indexNewlines(abs int64, buf []byte) {
	i := int64(0)
	if abs < s.notedTo {
		i = s.notedTo - abs
	}
	for ; i < int64(len(buf)); i++ {
		if buf[i] == '\n' {
			s.src.noteNewlineAt(abs + i)
		}
	}
	if end := abs + int64(len(buf)); end > s.notedTo {
		s.notedTo = end
	}
	// THE SOURCE'S HEAD MEANS INDEXED, not read. Advancing it to the cache's head
	// would let a location be answered from an incomplete line index — a wrong
	// line, or a column that changes once the rest of a rune arrives.
	s.src.advanceHead(s.notedTo)
}

// indexThrough reads and indexes up to target, so a location resolved just after
// it cannot be provisional. It is bounded: callers ask for a few bytes past an
// offset that is already live, never for the rest of the stream.
func (s *Scan) indexThrough(target int64) error {
	if s.end >= 0 && target > s.end {
		target = s.end
	}
	if target <= s.notedTo {
		return nil
	}
	if s.fixed != nil {
		if target > int64(len(s.fixed)) {
			target = int64(len(s.fixed))
		}
		if target <= s.notedTo {
			return nil
		}
		s.indexNewlines(s.notedTo, s.fixed[s.notedTo:target:target])
		return nil
	}

	avail, err := s.cache.Ensure(s.notedTo, int(target-s.notedTo))
	if err != nil {
		return err
	}
	if int64(avail) < target-s.notedTo {
		s.end = s.notedTo + int64(avail)
		target = s.end
	}
	if target <= s.notedTo {
		s.src.advanceHead(s.notedTo)
		return nil
	}
	v, err := s.cache.Acquire(s.notedTo, target)
	if err != nil {
		return err
	}
	buf, err := v.AppendTo(s.idxBuf[:0])
	v.Close()
	if err != nil {
		return err
	}
	s.idxBuf = buf
	s.indexNewlines(s.notedTo, buf)
	return nil
}
